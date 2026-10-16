// Per-server upstream OAuth client selection for issuer-gated serving.
//
// A user session credential records, when it is issued, which upstream OAuth
// client it uses for each MCP server it can reach, or that a server needs
// none. With FlagUpstreamClientSelectionServing on, proxied (remote and
// tunneled) servers resolve the caller's upstream credential through that
// choice alone: the token comes from the chosen client and routes by that
// client's own remote session issuer, never by the issuer derived for the
// server's user session issuer. A missing or unusable choice fails closed
// with a reconnect challenge; it is never inferred on a request and never
// replaced by another client.
//
// Credentials without a session row (principal credentials, agent keys,
// assistant tokens) and hosted toolsets keep the issuer-derived path, as does
// every request while the flag is off.

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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizations_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
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
	// associated with the server or no longer exists, or a no-auth choice for
	// a server that has since been given a client.
	errUpstreamSelectionUnusable = errors.New("credential's upstream client choice for this server is no longer usable")
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
	service        *Service
	organizationID string
	credentialID   uuid.UUID
}

// upstreamSelectorFor returns the selector for the credential in ctx, or nil
// when the request keeps the issuer-derived path: the flag is off for the
// organization, or the credential has no session row.
func (s *Service) upstreamSelectorFor(ctx context.Context, endpoint *ResolvedMcpEndpoint) (*upstreamSelector, error) {
	identity, ok := mcpidentity.FromContext(ctx)
	if !ok || identity.SessionJTI() == "" {
		return nil, nil
	}
	switch identity.Kind() {
	case mcpidentity.KindUserSession, mcpidentity.KindAgent, mcpidentity.KindWorkload:
	default:
		return nil, nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID != endpoint.OrganizationID {
		return nil, nil
	}
	if !s.upstreamSelectionServing(ctx, endpoint.OrganizationID) {
		return nil, nil
	}
	policy, err := s.loadSessionPolicy(ctx, endpoint, identity.SessionJTI())
	if err != nil {
		return nil, fmt.Errorf("load session credential: %w", err)
	}
	return &upstreamSelector{service: s, organizationID: endpoint.OrganizationID, credentialID: policy.credentialID}, nil
}

// forServer returns the credential's usable choice for serverID, or
// errUpstreamSelectionMissing or errUpstreamSelectionUnusable.
func (u *upstreamSelector) forServer(ctx context.Context, serverID uuid.UUID) (upstreamChoice, error) {
	var none upstreamChoice
	row, err := usersessions_repo.New(u.service.db).GetUpstreamClientSelection(ctx, usersessions_repo.GetUpstreamClientSelectionParams{
		CredentialKind: upstreamclients.CredentialKindUserSession,
		CredentialID:   u.credentialID,
		McpServerID:    serverID,
		OrganizationID: u.organizationID,
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
	if selector == nil {
		return none, false, nil
	}
	choice, err := selector.forServer(ctx, endpoint.McpServerID.UUID)
	switch {
	case errors.Is(err, errUpstreamSelectionMissing), errors.Is(err, errUpstreamSelectionUnusable):
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: "+err.Error(), issuerGateReasonUpstreamSelectionUnavailable)
		return none, true, writeRemoteSessionReconnectChallenge(w, authentication)
	case err != nil:
		return none, true, oops.E(oops.CodeUnexpected, err, "load upstream client choice").LogError(ctx, s.logger)
	}
	authentication.chosenClient = choice.clientID
	return choice, true, nil
}

// resolveAccessTokens resolves the authenticated caller's upstream
// credentials: every client the issuer binds, or only the chosen client once
// directUpstreamChoice has narrowed authentication to one.
func (s *Service) resolveAccessTokens(ctx context.Context, authentication *issuerGateAuthentication, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	endpoint := authentication.endpoint
	if authentication.chosenClient.Valid {
		tokens, err := s.remoteChallengeMgr.ResolveClientAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject, authentication.chosenClient.UUID, skipUnusable)
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
// per member per request. Hosted members keep the issuer-derived path.
type memberChoices struct {
	selector *upstreamSelector
	resolve  func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error)

	mu       sync.Mutex
	byServer map[uuid.UUID]map[uuid.UUID]remotesessions.UpstreamToken
}

// newMemberChoices returns the gateway's member choices for the credential
// authentication admitted, or nil when the gateway keeps the issuer-derived
// path.
func (s *Service) newMemberChoices(ctx context.Context, authentication *issuerGateAuthentication) (*memberChoices, error) {
	selector, err := s.upstreamSelectorFor(ctx, authentication.endpoint)
	if err != nil || selector == nil {
		return nil, err
	}
	endpoint := authentication.endpoint
	return &memberChoices{
		selector: selector,
		resolve: func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
			tokens, err := s.remoteChallengeMgr.ResolveClientAccessTokens(ctx, member.projectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject, member.chosenClientID.UUID, true)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, s.logger)
			}
			return tokens, nil
		},
		mu:       sync.Mutex{},
		byServer: map[uuid.UUID]map[uuid.UUID]remotesessions.UpstreamToken{},
	}, nil
}

// apply rewrites each proxied member's routing identity from the credential's
// choice: the chosen client's issuer, none for no-auth, or the reason the
// member cannot be reached. A member's failure degrades that member alone.
func (c *memberChoices) apply(ctx context.Context, logger *slog.Logger, members []metaMember) {
	for i := range members {
		member := &members[i]
		if member.backend != metaMemberBackendProxied {
			continue
		}
		choice, err := c.selector.forServer(ctx, member.serverID)
		switch {
		case errors.Is(err, errUpstreamSelectionMissing), errors.Is(err, errUpstreamSelectionUnusable):
			member.upstreamUnavailable = &metaMemberError{message: fmt.Sprintf("server %q needs this gateway to be signed in to again to choose its connection", member.slug)}
			continue
		case err != nil:
			logger.ErrorContext(ctx, "load gateway member upstream client choice", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(err))
			member.upstreamUnavailable = &metaMemberError{message: fmt.Sprintf("server %q could not load its connection credentials; retry shortly", member.slug)}
			continue
		}
		member.chosenClientID = choice.clientID
		member.remoteSessionIssuerID = choice.remoteSessionIssuerID
		member.upstreamNoAuth = choice.noAuth()
	}
}

// tokens is the token map a chosen member routes from: empty for no-auth,
// otherwise its chosen client's credential.
func (c *memberChoices) tokens(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if member.upstreamUnavailable != nil {
		return nil, member.upstreamUnavailable
	}
	if !member.chosenClientID.Valid {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if tokens, ok := c.byServer[member.serverID]; ok {
		return tokens, nil
	}
	tokens, err := c.resolve(ctx, member)
	if err != nil {
		return nil, &metaMemberError{message: fmt.Sprintf("server %q could not load its connection credentials; retry shortly", member.slug)}
	}
	c.byServer[member.serverID] = tokens
	return tokens, nil
}

// upstreamSelectionServing reports whether the organization serves proxied
// MCP servers from per-server client associations and credential choices.
// An unavailable flag provider keeps the issuer-derived path, which is what
// served before the rollout.
func (s *Service) upstreamSelectionServing(ctx context.Context, organizationID string) bool {
	if s.features == nil || organizationID == "" {
		return false
	}
	slug := ""
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID == organizationID {
		slug = authCtx.OrganizationSlug
	}
	if slug == "" {
		org, err := organizations_repo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
		if err != nil {
			s.logger.WarnContext(ctx, "load organization for upstream client selection flag", attr.SlogError(err))
			return false
		}
		slug = org.Slug
	}
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagUpstreamClientSelectionServing, organizationID, feature.OrgProjectGroups(slug, ""))
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate upstream client selection flag", attr.SlogError(err))
		return false
	}
	return enabled
}

// serverAssociatedClients returns, when the organization serves from
// associations, the live clients associated with each of serverIDs; nil
// otherwise. A server with none maps to an empty set.
func (s *Service) serverAssociatedClients(ctx context.Context, organizationID string, serverIDs []uuid.UUID) (map[uuid.UUID]map[uuid.UUID]bool, error) {
	if len(serverIDs) == 0 || !s.upstreamSelectionServing(ctx, organizationID) {
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

// attachAssociatedClients records each proxied member's associated clients
// when the request's organization serves from associations, so consent and
// gateway eligibility match a client to the members it is configured for
// rather than to every member sharing its authorization server. Hosted
// members, and every member while the flag is off, keep the issuer match.
func (s *Service) attachAssociatedClients(ctx context.Context, members []metaMember) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		if member.backend == metaMemberBackendProxied {
			ids = append(ids, member.serverID)
		}
	}
	associated, err := s.serverAssociatedClients(ctx, authCtx.ActiveOrganizationID, ids)
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
// of its associated clients when they are known, otherwise a client of the
// member's authorization server in the member's own project when a project
// owns it.
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
// a client: it has associated clients when they are known, otherwise it has a
// derived authorization server.
func memberNeedsSignIn(member metaMember) bool {
	if member.associatedClients != nil {
		return len(member.associatedClients) > 0
	}
	return member.remoteSessionIssuerID.Valid
}
