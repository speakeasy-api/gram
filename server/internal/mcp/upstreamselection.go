// Per-server upstream OAuth client selection for issuer-gated serving.
//
// A user session credential records, when it is issued, which upstream OAuth
// client it uses for each MCP server it can reach, or that a server needs
// none. Proxied (remote and tunneled) servers resolve the caller's upstream
// credential through that choice alone: the token comes from the chosen client and routes by that
// client's own remote session issuer, never by the issuer derived for the
// server's user session issuer. A missing or unusable choice fails closed
// with a reconnect challenge; it is never inferred on a request and never
// replaced by another client.
//
// Credentials without a session row (principal credentials, agent keys,
// assistant tokens) and hosted toolsets keep the issuer-derived path, except
// that once the organization enforces upstream auth a principal credential is
// served only through the execution-selection profile version it binds.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// issuerGateReasonUpstreamSelectionUnavailable: the bearer token was accepted
// but the credential has no usable upstream client choice for the server:
// it was issued before the server was configured, or its chosen client was
// detached or deleted since. Reconnecting issues a credential that records a
// fresh choice.
const issuerGateReasonUpstreamSelectionUnavailable = "upstream_selection_unavailable"

var (
	// errUpstreamSelectionMissing is a credential with no choice for the
	// server.
	errUpstreamSelectionMissing = errors.New("credential has no upstream client choice for this server")

	// errUpstreamSelectionUnusable is a choice whose client is no longer
	// associated with the server, no longer bound to the credential's issuer
	// or no longer exists, or a no-auth choice for a server that has since
	// been given a client.
	errUpstreamSelectionUnusable = errors.New("credential's upstream client choice for this server is no longer usable")

	// errSessionAuthContextMismatch is a session credential whose auth context
	// is missing or names another organization than the endpoint's.
	errSessionAuthContextMismatch = errors.New("session credential's auth context does not match the endpoint's organization")
)

// upstreamChoice is one credential's usable choice for one server.
type upstreamChoice struct {
	// clientID is the chosen remote session client; invalid for no-auth.
	clientID uuid.NullUUID

	// remoteSessionIssuerID is the chosen client's own issuer, which the
	// credential routes by; invalid for no-auth.
	remoteSessionIssuerID uuid.NullUUID
}

// noAuth reports a choice that the server needs no upstream authentication.
func (c upstreamChoice) noAuth() bool {
	return !c.clientID.Valid
}

// upstreamSelector reads one authenticated credential's choices.
type upstreamSelector struct {
	service             *Service
	organizationID      string
	userSessionIssuerID uuid.UUID
	// credentialKind names what credentialID identifies; empty is a user
	// session credential.
	credentialKind string
	credentialID   uuid.UUID
}

// credentialKindOrDefault is the kind of credential the selector reads the
// choices of.
func (u *upstreamSelector) credentialKindOrDefault() string {
	if u.credentialKind == "" {
		return upstreamclients.CredentialKindUserSession
	}
	return u.credentialKind
}

// profileSelectorFor returns the selector for a principal credential that
// binds an execution-selection profile version in the endpoint's organization
// and project, or nil for any other credential. Its choices are read only by
// the typed auth client resolver: the upstream client choice is keyed by
// sign-in, which a turn credential has none of.
func (s *Service) profileSelectorFor(ctx context.Context, endpoint *ResolvedMcpEndpoint) *upstreamSelector {
	authenticated, ok := principalcredential.FromContext(ctx)
	if !ok {
		return nil
	}
	return s.profileSelector(authenticated.Credential, endpoint)
}

func (s *Service) profileSelector(credential principalcredential.Credential, endpoint *ResolvedMcpEndpoint) *upstreamSelector {
	if !credential.SelectionProfileVersionID.Valid ||
		credential.OrganizationID != endpoint.OrganizationID ||
		credential.ProjectID != endpoint.ProjectID {
		return nil
	}
	return &upstreamSelector{
		service:             s,
		organizationID:      endpoint.OrganizationID,
		userSessionIssuerID: endpoint.UserSessionIssuerID,
		credentialKind:      upstreamclients.CredentialKindExecutionProfileVersion,
		credentialID:        credential.SelectionProfileVersionID.UUID,
	}
}

// agentKeySelectorFor returns the selector for an agent API key that was set
// up with a credential for the endpoint's server, or nil for any other caller
// and for a server the key has no choice for, which keeps the issuer-derived
// path.
func (s *Service) agentKeySelectorFor(ctx context.Context, endpoint *ResolvedMcpEndpoint) (*upstreamSelector, error) {
	mode, byKey := contextvalues.APIKeyAuthorization(ctx)
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	authCtx, hasAuth := contextvalues.GetAuthContext(ctx)
	if !byKey || mode != contextvalues.APIKeyAuthorizationModePrincipal || !hasActor || actor.Type != urn.PrincipalTypeAgent ||
		!hasAuth || authCtx == nil || authCtx.ActiveOrganizationID != endpoint.OrganizationID || !endpoint.McpServerID.Valid {
		return nil, nil
	}
	keyID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil {
		return nil, nil
	}
	has, err := upstreamclientsrepo.New(s.db).HasCredentialChoice(ctx, upstreamclientsrepo.HasCredentialChoiceParams{
		CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID,
		McpServerID: endpoint.McpServerID.UUID, OrganizationID: endpoint.OrganizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("check agent key credential choice: %w", err)
	}
	if !has {
		return nil, nil
	}
	return &upstreamSelector{service: s, organizationID: endpoint.OrganizationID, userSessionIssuerID: endpoint.UserSessionIssuerID, credentialKind: upstreamclients.CredentialKindAPIKey, credentialID: keyID}, nil
}

// upstreamSelectorFor returns the selector for the credential in ctx, or nil
// when the credential is not a session and keeps the issuer-derived path. A
// session credential gets a selector or an error, never the issuer-derived
// path: one whose auth context is missing or names another organization is
// refused. Anonymous sessions carry no auth context; their choices are read
// in the endpoint's organization.
func (s *Service) upstreamSelectorFor(ctx context.Context, endpoint *ResolvedMcpEndpoint) (*upstreamSelector, error) {
	identity, ok := mcpidentity.FromContext(ctx)
	if !ok || identity.SessionJTI() == "" {
		return nil, nil
	}
	switch identity.Kind() {
	case mcpidentity.KindUserSession, mcpidentity.KindAgent, mcpidentity.KindWorkload:
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil || authCtx.ActiveOrganizationID != endpoint.OrganizationID {
			return nil, errSessionAuthContextMismatch
		}
	case mcpidentity.KindAnonymous:
	default:
		return nil, nil
	}
	policy, err := s.loadSessionPolicy(ctx, endpoint, identity.SessionJTI())
	if err != nil {
		return nil, fmt.Errorf("load session credential: %w", err)
	}
	return &upstreamSelector{service: s, organizationID: endpoint.OrganizationID, userSessionIssuerID: endpoint.UserSessionIssuerID, credentialKind: upstreamclients.CredentialKindUserSession, credentialID: policy.credentialID}, nil
}

// forServer returns the credential's usable choice for serverID, or
// errUpstreamSelectionMissing or errUpstreamSelectionUnusable.
func (u *upstreamSelector) forServer(ctx context.Context, serverID uuid.UUID) (upstreamChoice, error) {
	var none upstreamChoice
	row, err := usersessions_repo.New(u.service.db).GetUpstreamClientSelection(ctx, usersessions_repo.GetUpstreamClientSelectionParams{
		UserSessionIssuerID: u.userSessionIssuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        u.credentialID,
		McpServerID:         serverID,
		OrganizationID:      u.organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return none, errUpstreamSelectionMissing
	case err != nil:
		return none, fmt.Errorf("load upstream client choice: %w", err)
	case !row.Usable:
		return none, errUpstreamSelectionUnusable
	case row.NoAuth:
		return none, nil
	}
	return upstreamChoice{clientID: row.RemoteSessionClientID, remoteSessionIssuerID: row.RemoteSessionIssuerID}, nil
}

// refreshUpstreamChoice returns the choice of the credential a refreshed
// session belongs to for a direct endpoint's server, or
// errUpstreamSelectionMissing or errUpstreamSelectionUnusable.
func (s *Service) refreshUpstreamChoice(ctx context.Context, endpoint *ResolvedMcpEndpoint, jti string) (upstreamChoice, error) {
	var none upstreamChoice
	if !endpoint.McpServerID.Valid {
		return none, errUpstreamSelectionMissing
	}
	selector, err := s.refreshSelector(ctx, endpoint, jti)
	if err != nil {
		return none, err
	}
	return selector.forServer(ctx, endpoint.McpServerID.UUID)
}

// refreshSelector reads the choices of the credential a refreshed session
// belongs to.
func (s *Service) refreshSelector(ctx context.Context, endpoint *ResolvedMcpEndpoint, jti string) (*upstreamSelector, error) {
	policy, err := s.loadSessionPolicy(ctx, endpoint, jti)
	if err != nil {
		return nil, fmt.Errorf("load session credential: %w", err)
	}
	return &upstreamSelector{service: s, organizationID: endpoint.OrganizationID, userSessionIssuerID: endpoint.UserSessionIssuerID, credentialKind: upstreamclients.CredentialKindUserSession, credentialID: policy.credentialID}, nil
}

// serverChoices is one credential's recorded choices for a set of servers.
type serverChoices map[uuid.UUID]usersessions_repo.ListUpstreamClientSelectionsRow

// forServer returns the usable choice for serverID, or
// errUpstreamSelectionMissing or errUpstreamSelectionUnusable.
func (c serverChoices) forServer(serverID uuid.UUID) (upstreamChoice, error) {
	none := upstreamChoice{clientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, remoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	row, ok := c[serverID]
	switch {
	case !ok:
		return none, errUpstreamSelectionMissing
	case !row.Usable:
		return none, errUpstreamSelectionUnusable
	case row.NoAuth:
		return none, nil
	}
	return upstreamChoice{clientID: row.RemoteSessionClientID, remoteSessionIssuerID: row.RemoteSessionIssuerID}, nil
}

// forServers loads the credential's choices for serverIDs in one read.
func (u *upstreamSelector) forServers(ctx context.Context, serverIDs []uuid.UUID) (serverChoices, error) {
	rows, err := usersessions_repo.New(u.service.db).ListUpstreamClientSelections(ctx, usersessions_repo.ListUpstreamClientSelectionsParams{
		UserSessionIssuerID: u.userSessionIssuerID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        u.credentialID,
		McpServerIds:        serverIDs,
		OrganizationID:      u.organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("load upstream client choices: %w", err)
	}
	choices := make(serverChoices, len(rows))
	for _, row := range rows {
		choices[row.McpServerID] = row
	}
	return choices, nil
}

// directUpstreamChoice applies the credential's choice to a direct proxied
// endpoint. It reports false when the endpoint keeps the issuer-derived path.
// Otherwise it narrows authentication to the chosen client and returns the
// issuer the credential routes by. A missing or unusable choice writes the
// reconnect challenge.
func (s *Service) directUpstreamChoice(ctx context.Context, w http.ResponseWriter, authentication *issuerGateAuthentication) (upstreamChoice, bool, error) {
	var none upstreamChoice
	endpoint := authentication.endpoint
	if !endpoint.McpServerID.Valid {
		return none, false, nil
	}
	selector, err := s.upstreamSelectorFor(ctx, endpoint)
	if err != nil {
		return none, false, oops.E(oops.CodeUnexpected, err, "load upstream client choice").LogError(ctx, s.logger)
	}
	mode, err := s.authClientMode(ctx, endpoint.OrganizationID)
	if err != nil {
		return none, true, oops.E(oops.CodeUnexpected, err, "load upstream auth enforcement").LogError(ctx, s.logger)
	}
	if selector == nil {
		return s.directPrincipalChoice(ctx, w, authentication, mode)
	}
	if mode == feature.AuthClientResolverEnforced {
		choice, handled, err := s.enforceAuthClient(ctx, w, authentication, selector)
		if handled || err != nil {
			return choice, true, err
		}
	}
	choice, err := selector.forServer(ctx, endpoint.McpServerID.UUID)
	switch mode {
	case feature.AuthClientResolverShadow:
		s.shadowAuthClient(ctx, selector, endpoint.McpServerID.UUID, endpoint.ProjectID, authentication.subject, choice, err)
	case feature.AuthClientResolverOff:
		if err == nil {
			s.newMandatoryShadow(ctx, endpoint.OrganizationID).recordSkipped(ctx, mandatorySurfaceAuthClient, true, mandatoryReasonResolverOff, endpoint.McpServerID.UUID.String(), endpoint.ProjectID.String())
		}
	case feature.AuthClientResolverEnforced:
	}
	switch {
	case errors.Is(err, errUpstreamSelectionMissing), errors.Is(err, errUpstreamSelectionUnusable):
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: "+err.Error(), issuerGateReasonUpstreamSelectionUnavailable)
		return none, true, writeRemoteSessionReconnectChallenge(w, authentication)
	case err != nil:
		return none, true, oops.E(oops.CodeUnexpected, err, "load upstream client choice").LogError(ctx, s.logger)
	}
	authentication.chosenClient = choice.clientID
	authentication.chosenNoAuth = choice.noAuth()
	return choice, true, nil
}

// directPrincipalChoice serves a credential that is not a session. Once the
// organization enforces, a principal credential is served only through the
// execution-selection profile it binds: one that binds none, or whose
// profile holds no usable choice, is refused, since a profile has no upstream
// client choice to fall back to. Other credentials, and every credential
// before enforcement, keep the issuer-derived path.
func (s *Service) directPrincipalChoice(ctx context.Context, w http.ResponseWriter, authentication *issuerGateAuthentication, mode feature.AuthClientResolverMode) (upstreamChoice, bool, error) {
	var none upstreamChoice
	if mode != feature.AuthClientResolverEnforced {
		return none, false, nil
	}
	if selector, err := s.agentKeySelectorFor(ctx, authentication.endpoint); err != nil {
		return none, true, oops.E(oops.CodeUnexpected, err, "load agent key credential choice").LogError(ctx, s.logger)
	} else if selector != nil {
		choice, handled, err := s.enforceAuthClient(ctx, w, authentication, selector)
		if handled || err != nil {
			return choice, true, err
		}
		// A key's choice is served only through the typed resolver, never by
		// the issuer's credentials.
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: agent key choice unusable", issuerGateReasonUpstreamSelectionUnavailable)
		return none, true, writeRemoteSessionReconnectChallenge(w, authentication)
	}
	authenticated, ok := principalcredential.FromContext(ctx)
	if !ok {
		return none, false, nil
	}
	profile := s.profileSelector(authenticated.Credential, authentication.endpoint)
	if profile != nil {
		choice, handled, err := s.enforceAuthClient(ctx, w, authentication, profile)
		if handled || err != nil {
			return choice, true, err
		}
	}
	s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: no execution profile choice", issuerGateReasonUpstreamSelectionUnavailable)
	return none, true, writeRemoteSessionReconnectChallenge(w, authentication)
}

// resolveAccessTokens resolves the authenticated caller's upstream
// credentials: every client the issuer binds, or only the chosen client once
// directUpstreamChoice has narrowed authentication to one, or none for a
// no-auth choice.
func (s *Service) resolveAccessTokens(ctx context.Context, authentication *issuerGateAuthentication, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	tokens, err := s.resolveSelectedAccessTokens(ctx, authentication, skipUnusable)
	if err != nil {
		return nil, err
	}
	// An attached agent's checked connection binds every path below, typed
	// or handed back to the upstream client choice.
	tokens, err = pinAttachedSession(tokens, authentication.attachedSession, skipUnusable)
	if err != nil {
		return nil, fmt.Errorf("resolve attached connection: %w", err)
	}
	return tokens, nil
}

func (s *Service) resolveSelectedAccessTokens(ctx context.Context, authentication *issuerGateAuthentication, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	endpoint := authentication.endpoint
	if authentication.chosenNoAuth {
		return map[uuid.UUID]remotesessions.UpstreamToken{}, nil
	}
	if authentication.chosenClient.Valid && authentication.authClient != nil {
		// A typed auth client was checked against the server, which authorizes
		// its registration without a sign-in link.
		tokens, err := s.upstreamTokenSource().ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
			ProjectID:           endpoint.ProjectID,
			OrganizationID:      endpoint.OrganizationID,
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Subject:             authentication.subject,
			RegistrationID:      authentication.chosenClient.UUID,
			SkipUnusable:        skipUnusable,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve auth client: %w", err)
		}
		return tokens, nil
	}
	if authentication.chosenClient.Valid {
		tokens, err := s.upstreamTokenSource().ResolveClientAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject, authentication.chosenClient.UUID, skipUnusable)
		if err != nil {
			return nil, fmt.Errorf("resolve chosen client: %w", err)
		}
		return tokens, nil
	}
	if skipUnusable {
		tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
		if err != nil {
			return nil, fmt.Errorf("resolve available clients: %w", err)
		}
		return tokens, nil
	}
	tokens, err := s.remoteChallengeMgr.ResolveAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
	if err != nil {
		return nil, fmt.Errorf("resolve bound clients: %w", err)
	}
	return tokens, nil
}

// memberChoices applies one credential's choices to a gateway's proxied
// members and resolves each member's credential from its chosen client, once
// per member per request. Hosted members keep the issuer-derived path. Once
// the organization enforces, each member is served by the typed auth client
// resolver and the mandatory check, as a direct endpoint is.
type memberChoices struct {
	selector *upstreamSelector
	subject  urn.SessionSubject
	resolve  func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error)

	// enforced serves members through the typed auth client resolver for
	// subject.
	enforced bool
	// profile reports that selector reads an execution-selection profile,
	// which has no upstream client choice to fall back to.
	profile bool

	// metaServerID is the gateway the members belong to; invalid for the
	// organization gateway, which has no member list.
	metaServerID uuid.NullUUID

	// onlyChosen serves only the members the credential has a choice for;
	// the rest keep the issuer-derived path. An agent key is set up with
	// choices for some servers at most.
	onlyChosen bool

	// chosen are the members apply found a choice for, when onlyChosen.
	chosen map[uuid.UUID]bool

	mu       sync.Mutex
	byServer map[uuid.UUID]*memberTokens
}

// memberTokens is one member's resolution, shared by every caller asking for
// it in the request. done closes once tokens and err are set.
type memberTokens struct {
	done   chan struct{}
	tokens map[uuid.UUID]remotesessions.UpstreamToken
	err    error
}

// newMemberChoices returns the gateway's member choices for the credential
// authentication admitted, or nil when the gateway keeps the issuer-derived
// path.
func (s *Service) newMemberChoices(ctx context.Context, authentication *issuerGateAuthentication) (*memberChoices, error) {
	endpoint := authentication.endpoint
	selector, err := s.upstreamSelectorFor(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	mode, err := s.authClientMode(ctx, endpoint.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load upstream auth enforcement").LogError(ctx, s.logger)
	}
	enforced := mode == feature.AuthClientResolverEnforced
	profile := false
	if selector == nil {
		authenticated, ok := principalcredential.FromContext(ctx)
		if !enforced || !ok {
			return nil, nil
		}
		// A principal credential is served only through its profile; one that
		// binds none reaches no proxied member.
		selector, profile = s.profileSelector(authenticated.Credential, endpoint), true
	}
	return &memberChoices{
		selector:     selector,
		enforced:     enforced,
		subject:      authentication.subject,
		profile:      profile,
		metaServerID: endpoint.MetaMcpServerID,
		onlyChosen:   false,
		chosen:       nil,
		resolve: func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			return s.resolveChosenMemberTokens(ctx, endpoint, authentication.subject, enforced, member)
		},
		mu:       sync.Mutex{},
		byServer: map[uuid.UUID]*memberTokens{},
	}, nil
}

// resolveChosenMemberTokens resolves one gateway member's credential from its
// chosen client for subject.
func (s *Service) resolveChosenMemberTokens(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, enforced bool, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	// Under enforcement the connection a member's permit was decided
	// for binds its tokens, typed or handed back to the upstream
	// client choice. Without its token the member fails: an empty
	// resolution would dial anonymously or chain another identity.
	pinned := uuid.Nil
	if enforced && member.authClient != nil {
		pinned = member.authClient.attachedSession
	}
	if member.typedAuthClient {
		tokens, err := s.upstreamTokenSource().ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
			ProjectID:           member.projectID,
			OrganizationID:      endpoint.OrganizationID,
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Subject:             subject,
			RegistrationID:      member.chosenClientID.UUID,
			SkipUnusable:        true,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve auth client").LogError(ctx, s.logger)
		}
		return pinAttachedSession(tokens, pinned, false)
	}
	tokens, err := s.upstreamTokenSource().ResolveClientAccessTokens(ctx, member.projectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject, member.chosenClientID.UUID, true)
	switch {
	case errors.Is(err, remotesessions.ErrChosenClientUnbound):
		return nil, fmt.Errorf("resolve chosen client: %w", err)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, s.logger)
	}
	return pinAttachedSession(tokens, pinned, false)
}

// apply rewrites each proxied member's routing identity from the credential's
// choice: the chosen client's issuer, none for no-auth, or the reason the
// member cannot be reached. A missing or unusable choice degrades that member
// alone. Choices are read in batches; a read failure degrades proxied members
// while hosted members keep serving.
func (c *memberChoices) apply(ctx context.Context, logger *slog.Logger, members []metaMember, typed bool) {
	ids := make([]uuid.UUID, 0, len(members))
	projects := make(map[uuid.UUID]uuid.UUID, len(members))
	for _, member := range members {
		if member.backend == metaMemberBackendProxied {
			ids = append(ids, member.serverID)
			projects[member.serverID] = member.projectID
		}
	}
	if len(ids) == 0 {
		return
	}
	if c.selector == nil {
		// A principal credential that binds no execution-selection profile
		// reaches no proxied member once the organization enforces.
		for i := range members {
			if members[i].backend == metaMemberBackendProxied {
				members[i].upstreamUnavailable = &metaMemberError{message: fmt.Sprintf("server %q needs this gateway to be signed in to again to choose its connection", members[i].slug)}
			}
		}
		return
	}
	// Under enforcement every proxied member is served through the typed
	// resolver, and a profile has no upstream client choice to fall back to.
	typed = typed || c.enforced || c.profile
	var resolved map[uuid.UUID]authClientResolution
	var err error
	if typed {
		resolved, err = c.selector.resolveAuthClients(ctx, ids)
		if err == nil && c.enforced && !c.profile && c.recordNewMemberChoices(ctx, logger, members, resolved) {
			resolved, err = c.selector.resolveAuthClients(ctx, ids)
		}
		ids = ids[:0]
		for id, res := range resolved {
			if res.outcome != authClientLegacy {
				continue
			}
			// An enforced choice that names no auth client is never served
			// by the upstream client choice; a profile and an agent key have
			// none.
			if (c.profile || c.onlyChosen || c.enforced) && res.clientID == uuid.Nil {
				resolved[id] = withoutLegacyFallback(res)
				continue
			}
			if c.profile || c.onlyChosen {
				res.outcome = authClientGap
				resolved[id] = res
				continue
			}
			ids = append(ids, id)
		}
	}
	var choices serverChoices
	if err == nil && len(ids) > 0 {
		choices, err = c.selector.forServers(ctx, ids)
	}
	if err != nil {
		logger.ErrorContext(ctx, "load gateway member upstream client choices", attr.SlogError(err))
	}
	memberErrors := make(map[uuid.UUID]error)
	var headerIDs []uuid.UUID
	if typed && err == nil {
		for id, res := range resolved {
			issuerID := c.selector.userSessionIssuerID
			if c.onlyChosen {
				var issuerErr error
				issuerID, issuerErr = c.selector.service.agentKeyMemberIssuer(ctx, id, projects[id])
				if issuerErr != nil {
					memberErrors[id] = issuerErr
					continue
				}
			}
			prepared, prepareErr := c.selector.service.checkAuthClientPermission(ctx, authClientPermitRequest{
				OrganizationID: c.selector.organizationID, ProjectID: projects[id], ServerID: id, ClientID: res.clientID, ClientType: res.clientType, RegistrationID: res.choice.clientID,
				CredentialKind: c.selector.credentialKindOrDefault(), CredentialID: c.selector.credentialID, Subject: c.subject,
				UserSessionIssuerID: issuerID, ConnectionOwner: "",
			}, res, nil)
			resolved[id] = prepared
			memberErrors[id] = prepareErr
			if prepareErr == nil && prepared.sendsClientHeaders() {
				headerIDs = append(headerIDs, prepared.clientID)
			}
		}
	}
	headers, headerErr := c.selector.service.loadAuthClientHeaders(ctx, c.selector.organizationID, headerIDs)
	for i := range members {
		member := &members[i]
		if member.backend != metaMemberBackendProxied {
			continue
		}
		if c.onlyChosen {
			// A failed read degrades every member rather than letting one the
			// key has a choice for fall back to the issuer's credentials.
			if _, ok := resolved[member.serverID]; !ok && err == nil {
				continue
			}
			c.mu.Lock()
			c.chosen[member.serverID] = true
			c.mu.Unlock()
		}
		res := gapResolution(authClientReasonMissingChoice)
		memberErr := err
		if memberErr == nil {
			if typed {
				if choice, ok := resolved[member.serverID]; ok {
					res = choice
				}
				memberErr = memberErrors[member.serverID]
				if memberErr == nil && res.sendsClientHeaders() {
					memberErr = headerErr
					if memberErr == nil {
						clientHeaders := headers[res.clientID]
						memberErr = clientHeaders.err
						if memberErr == nil {
							res = applyAuthClientHeaders(res, clientHeaders.headers)
						}
					}
				}
				annotateAuthClient(ctx, c.selector.service.logger, res)
			} else {
				res.outcome = authClientLegacy
			}
			if memberErr == nil && res.outcome == authClientLegacy {
				res.choice, memberErr = choices.forServer(member.serverID)
			}
		}
		c.selector.service.applyMemberAuthClient(ctx, member, res, memberErr)
		// Under enforcement a resolved OAuth-backed client was checked against
		// the member, which authorizes its registration without a sign-in link.
		member.typedAuthClient = c.enforced && member.upstreamUnavailable == nil && res.outcome == authClientResolved && member.chosenClientID.Valid
	}
}

func (s *Service) applyMemberAuthClient(ctx context.Context, member *metaMember, res authClientResolution, err error) {
	member.authClient = &res
	if errors.Is(err, errUpstreamSelectionMissing) || errors.Is(err, errUpstreamSelectionUnusable) {
		member.upstreamUnavailable = &metaMemberError{message: fmt.Sprintf("server %q needs this gateway to be signed in to again to choose its connection", member.slug)}
		return
	}
	if err != nil {
		member.upstreamUnavailable = &metaMemberError{message: "This server's connection could not be checked; try again later."}
		return
	}
	switch res.outcome {
	case authClientNoAccess:
		member.upstreamUnavailable = s.authClientNoAccessError(ctx, member.serverID, res)
		return
	case authClientGap:
		member.upstreamUnavailable = memberCredentialError{&metaMemberError{message: "This server's authentication needs setup or a new sign-in."}}
		return
	case authClientResolved, authClientNoAuth, authClientLegacy:
	}
	member.chosenClientID = res.choice.clientID
	member.remoteSessionIssuerID = res.choice.remoteSessionIssuerID
	member.upstreamNoAuth = res.outcome == authClientNoAuth || res.headersOnly() || (res.outcome == authClientLegacy && res.choice.noAuth())
}

// serves reports whether member routes through the credential's choices
// rather than the issuer-derived path.
func (c *memberChoices) serves(member metaMember) bool {
	if !c.onlyChosen {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.chosen[member.serverID]
}

// tokens is the token map a chosen member routes from: empty for no-auth,
// otherwise its chosen client's credential. Each member resolves once per
// request; concurrent callers for one member wait for that resolution, within
// their own deadline, while other members resolve independently. A failed
// resolution is not kept, so a later caller retries it.
func (c *memberChoices) tokens(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if member.upstreamUnavailable != nil {
		return nil, member.upstreamUnavailable
	}
	if !member.chosenClientID.Valid {
		return nil, nil
	}
	c.mu.Lock()
	entry, ok := c.byServer[member.serverID]
	if !ok {
		entry = &memberTokens{done: make(chan struct{}), tokens: nil, err: nil}
		c.byServer[member.serverID] = entry
	}
	c.mu.Unlock()
	if ok {
		select {
		case <-entry.done:
			return entry.tokens, entry.err
		case <-ctx.Done():
			return nil, &metaMemberError{message: fmt.Sprintf("server %q could not load its connection credentials; retry shortly", member.slug)}
		}
	}

	tokens, err := c.resolve(ctx, member)
	switch {
	case errors.Is(err, remotesessions.ErrChosenClientUnbound):
		err = &metaMemberError{message: fmt.Sprintf("server %q needs this gateway to be signed in to again to choose its connection", member.slug)}
	case errors.Is(err, remotesessions.ErrNoValidToken):
		err = &metaMemberError{message: fmt.Sprintf("server %q's attached connection is no longer available; it must be attached again", member.slug)}
	case err != nil:
		err = &metaMemberError{message: fmt.Sprintf("server %q could not load its connection credentials; retry shortly", member.slug)}
	}
	if err != nil {
		c.mu.Lock()
		delete(c.byServer, member.serverID)
		c.mu.Unlock()
	}
	entry.tokens, entry.err = tokens, err
	close(entry.done)
	return tokens, err
}

// serverAssociatedClients returns the live clients associated with each of
// serverIDs, nil for none. A server with no client maps to an empty set.
func (s *Service) serverAssociatedClients(ctx context.Context, organizationID string, serverIDs []uuid.UUID) (map[uuid.UUID]map[uuid.UUID]bool, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	rows, err := upstreamclientsrepo.New(s.db).ListServerAssociatedClients(ctx, upstreamclientsrepo.ListServerAssociatedClientsParams{
		McpServerIds:   serverIDs,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list server associated clients: %w", err)
	}
	associated := make(map[uuid.UUID]map[uuid.UUID]bool, len(serverIDs))
	for _, id := range serverIDs {
		associated[id] = map[uuid.UUID]bool{}
	}
	for _, row := range rows {
		associated[row.McpServerID][row.RemoteSessionClientID] = true
	}
	return associated, nil
}

// attachAssociatedClients records each proxied member's associated clients,
// so consent and gateway eligibility match a client to the members it is
// configured for rather than to every member sharing its authorization
// server. Hosted members keep the issuer match.
func (s *Service) attachAssociatedClients(ctx context.Context, organizationID string, members []metaMember) error {
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		if member.backend == metaMemberBackendProxied {
			ids = append(ids, member.serverID)
		}
	}
	associated, err := s.serverAssociatedClients(ctx, organizationID, ids)
	if err != nil || associated == nil {
		return err
	}
	for i := range members {
		if clients, ok := associated[members[i].serverID]; ok {
			members[i].associatedClients = clients
		}
	}
	return nil
}

// memberUsesClient reports whether a client authenticates for a member: one
// of a proxied member's associated clients, otherwise, for a hosted member, a
// client of the member's authorization server in the member's own project
// when a project owns it.
func memberUsesClient(member metaMember, clientID, clientIssuerID uuid.UUID, clientProjectID uuid.NullUUID) bool {
	if member.associatedClients != nil {
		return member.associatedClients[clientID]
	}
	if !member.remoteSessionIssuerID.Valid || member.remoteSessionIssuerID.UUID != clientIssuerID {
		return false
	}
	return !clientProjectID.Valid || clientProjectID.UUID == member.projectID
}

// memberNeedsSignIn reports whether a member authenticates upstream through
// a client: a proxied member with associated clients, or a member with a
// derived authorization server. A member with no associated client that
// still derives an authorization server signs in through a client nobody has
// configured for it, so it needs setup rather than reading as needing none.
func memberNeedsSignIn(member metaMember) bool {
	if len(member.associatedClients) > 0 {
		return true
	}
	return member.remoteSessionIssuerID.Valid
}

// agentKeyMemberChoices serves the agent gateway members an agent key was set
// up with a credential for through that credential, where typed auth client
// resolution is enforced. It is nil for any other caller, before enforcement,
// and for a key with no choices, so the gateway keeps its path.
func (s *Service) agentKeyMemberChoices(ctx context.Context, organizationID string, agentID uuid.UUID) (*memberChoices, error) {
	mode, byKey := contextvalues.APIKeyAuthorization(ctx)
	authCtx, hasAuth := contextvalues.GetAuthContext(ctx)
	if !byKey || mode != contextvalues.APIKeyAuthorizationModePrincipal || !hasAuth || authCtx == nil || authCtx.ActiveOrganizationID != organizationID {
		return nil, nil
	}
	keyID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil {
		return nil, nil //nolint:nilerr // a key id that does not parse is no agent key, which keeps the gateway's path
	}
	enforcement, err := s.authClientMode(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if enforcement != feature.AuthClientResolverEnforced {
		return nil, nil
	}
	subject := urn.NewAgentSubject(agentID)
	return &memberChoices{
		selector:     &upstreamSelector{service: s, organizationID: organizationID, userSessionIssuerID: uuid.Nil, credentialKind: upstreamclients.CredentialKindAPIKey, credentialID: keyID},
		subject:      subject,
		enforced:     true,
		profile:      false,
		metaServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		onlyChosen:   true,
		chosen:       map[uuid.UUID]bool{},
		resolve: func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			issuerID, err := s.agentKeyMemberIssuer(ctx, member.serverID, member.projectID)
			if err != nil {
				return nil, err
			}
			endpoint := &ResolvedMcpEndpoint{
				ProjectID:           member.projectID,
				OrganizationID:      organizationID,
				UserSessionIssuerID: issuerID,
			}
			tokens, err := s.resolveChosenMemberTokens(ctx, endpoint, subject, true, member)
			if err != nil {
				return nil, err
			}
			if len(tokens) == 0 {
				return nil, remotesessions.ErrNoValidToken
			}
			return tokens, nil
		},
		mu:       sync.Mutex{},
		byServer: map[uuid.UUID]*memberTokens{},
	}, nil
}

// agentKeyMemberIssuer supplies the same member sign-in scope to permission
// and token resolution. A missing issuer stays empty: personal attachments
// require it, but service-account credentials do not.
func (s *Service) agentKeyMemberIssuer(ctx context.Context, serverID, projectID uuid.UUID) (uuid.UUID, error) {
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if err != nil {
		return uuid.Nil, fmt.Errorf("load agent member issuer: %w", err)
	}
	if !server.UserSessionIssuerID.Valid {
		return uuid.Nil, nil
	}
	return server.UserSessionIssuerID.UUID, nil
}
