// Typed auth client resolution for issuer-gated servers and gateway consent.
//
// A credential's choice for a server names a typed auth client
// (mcp_auth_clients). When the auth-client-resolver flag is on for the
// organization for a direct server, or a gateway has a consent catalogue,
// that client decides how the request authenticates upstream:
// a personal connection or a service account through its OAuth registration,
// a static credential or a forwarded header through the client's own headers,
// or no upstream authentication at all. A client that is gone, disabled,
// detached from the server or outside its tenant is a gap the caller fixes by
// reconnecting, never an implied no-auth and never another client.
//
// Some choices are handed back to the upstream client choice, which keeps
// serving them as before: EMA clients, choices not yet mapped onto auth
// clients, header copies that no longer match the server's headers, a
// registration that may still be served by identity chaining, and
// headers-only clients on tunneled servers. With the flag off, the shadow
// flag runs the resolver beside the choice and reports disagreement, without
// acquiring credentials.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Auth client types, as stored in mcp_auth_clients.client_type. They are the
// values mcp:auth checks carry.
const (
	authClientTypeRemoteSession    = authz.AuthClientTypeRemoteSession
	authClientTypeServiceAccount   = authz.AuthClientTypeServiceAccount
	authClientTypeStaticCredential = authz.AuthClientTypeStaticCredential
	authClientTypePassThrough      = authz.AuthClientTypePassThrough
	authClientTypeEMA              = authz.AuthClientTypeEMA
)

// authClientOutcome is how a credential's typed auth client resolves for a
// server.
type authClientOutcome string

const (
	// authClientResolved: the client supplies the upstream credential.
	authClientResolved authClientOutcome = "resolved"
	// authClientNoAuth: the server needs no upstream authentication.
	authClientNoAuth authClientOutcome = "no_auth"
	// authClientGap: the choice cannot be used; reconnecting records a new one.
	authClientGap authClientOutcome = "reconnect"
	// authClientNoAccess: the caller may not use the chosen client.
	// Reconnecting would choose it again, so this is a refusal with a link to
	// request access, never a reconnect.
	authClientNoAccess authClientOutcome = "no_access"
	// authClientLegacy: the upstream client choice serves the request.
	authClientLegacy authClientOutcome = "legacy"
)

// authClientParityError is the parity outcome of a resolver that failed.
const authClientParityError = "error"

// Reasons for an outcome. They are a closed set: the parity metric carries
// them as a label.
const (
	authClientReasonAmbiguous       = "ambiguous_choice"
	authClientReasonMissingChoice   = "missing_choice"
	authClientReasonUnmapped        = "unmapped_choice"
	authClientReasonNoAuthDeclared  = "no_auth_declared"
	authClientReasonClientGone      = "client_gone"
	authClientReasonClientDeleted   = "client_deleted"
	authClientReasonClientDisabled  = "client_disabled"
	authClientReasonUnassociated    = "client_unassociated"
	authClientReasonRegistration    = "registration_gone"
	authClientReasonDetached        = "registration_detached"
	authClientReasonUnknownType     = "unknown_type"
	authClientReasonNoAuthForbidden = "no_auth_forbidden"
	authClientReasonNoAuthStale     = "no_auth_server_has_clients"
	authClientReasonNotPermitted    = "not_permitted"
	authClientReasonSetup           = "setup"
	authClientReasonHeadersStale    = "client_headers_stale"
	authClientReasonLegacyMayChain  = "legacy_may_chain"
	authClientReasonTunneledHeaders = "tunneled_headers_only"
	authClientReasonEMA             = "ema"
	authClientReasonNone            = "none"
)

// authClientResolution is one credential's typed auth client decision for one
// server.
type authClientResolution struct {
	outcome    authClientOutcome
	reason     string
	clientID   uuid.UUID
	clientType string

	// serverName names the server in a request-access link.
	serverName string
	// tunneled is a server reached through a tunnel, which is sent no client
	// headers.
	tunneled bool

	// choice routes OAuth-backed clients: the registration and its issuer.
	choice upstreamChoice

	// headers are the client's own headers, loaded only when it serves.
	headers []proxy.ConfiguredHeader

	// noAuthAllowed is a server explicitly marked as needing no upstream
	// authentication.
	noAuthAllowed bool

	// attachedSession is the exact remote session an attached agent's
	// permit was decided for. Its token must come from that row, so a binding
	// that moves to another user's connection after the check serves nothing.
	attachedSession uuid.UUID
}

// headersOnly reports a resolved client whose material is its headers alone,
// with no bearer to acquire.
func (r *authClientResolution) headersOnly() bool {
	return r.outcome == authClientResolved && isHeadersOnlyType(r.clientType)
}

// sendsClientHeaders reports whether the request sends the client's own
// headers in place of the server's: a resolved client, or a mapped client
// whose registration the upstream client choice serves because it may chain
// an identity. A tunneled server is sent no client headers.
func (r *authClientResolution) sendsClientHeaders() bool {
	if r.tunneled {
		return false
	}
	return r.outcome == authClientResolved || (r.outcome == authClientLegacy && r.reason == authClientReasonLegacyMayChain)
}

func isHeadersOnlyType(clientType string) bool {
	return clientType == authClientTypeStaticCredential || clientType == authClientTypePassThrough
}

// authClientPermitRequest is one use of a resolved client to check.
type authClientPermitRequest struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ServerID       uuid.UUID
	ClientID       uuid.UUID
	ClientType     string
	// RegistrationID is the upstream OAuth client behind an OAuth-backed
	// client; personal checks name it.
	RegistrationID uuid.NullUUID
	// CredentialKind and CredentialID identify the credential whose choice
	// named the client.
	CredentialKind string
	CredentialID   uuid.UUID
	// Subject is the user the credential represents.
	Subject urn.SessionSubject
	// UserSessionIssuerID is the sign-in an agent attachment is keyed by.
	UserSessionIssuerID uuid.UUID
	// ConnectionOwner is the stored owner of the personal connection the
	// client serves, resolved by checkAuthClientPermission before the
	// permitter decides: the subject itself for a user, the user who owns the
	// exact attached connection for an attached agent, and empty when there is
	// none, which no personal grant matches.
	ConnectionOwner string
}

// connectionOwner is the owner the mcp:auth check names: the resolved
// ConnectionOwner, or for a request built without resolving it, a user's own
// subject. Nothing else is ever assumed, so an unresolved agent has no owner.
func (r authClientPermitRequest) connectionOwner() string {
	if r.ConnectionOwner != "" {
		return r.ConnectionOwner
	}
	if r.ClientType == authClientTypeRemoteSession && r.Subject.Kind == urn.SessionSubjectKindUser {
		return r.Subject.ID
	}
	return ""
}

// authClientPermitter decides whether the caller may use a resolved client.
type authClientPermitter interface {
	Permitted(ctx context.Context, req authClientPermitRequest) (bool, error)
}

// connectPermitter keeps today's rule until per-client grants exist
// (AIM-589): every client associated with the server is usable by its
// callers. Server access itself (mcp:connect) is checked later, when the
// backend is prepared, and still applies.
type connectPermitter struct{}

func (connectPermitter) Permitted(context.Context, authClientPermitRequest) (bool, error) {
	return true, nil
}

// mandatoryPermitter decides use of a resolved client with the mandatory
// mcp:auth check, for organizations with upstream auth enforcement enabled.
// It admits the caller on its own path, including callers the generic check
// skips, and a check it cannot evaluate is a refusal, never an allow. It
// makes the same check the shadow records: a personal client is checked
// against the user whose connection the resolver loaded and the upstream
// OAuth client that connection uses.
type mandatoryPermitter struct {
	engine *authz.Engine
}

func (p mandatoryPermitter) Permitted(ctx context.Context, req authClientPermitRequest) (bool, error) {
	authorization, err := p.engine.PrepareMandatory(ctx)
	if err != nil {
		return false, fmt.Errorf("admit caller for auth client: %w", err)
	}
	decision, err := authorization.Decide(req.OrganizationID, mandatoryAuthClientCheck(req.ProjectID, req.ClientID, req.RegistrationID, req.ClientType, req.connectionOwner()))
	if err != nil {
		return false, fmt.Errorf("evaluate auth client check: %w", err)
	}
	return decision.Allowed, nil
}

// gapResolution is a choice that cannot be used, for reason.
func gapResolution(reason string) authClientResolution {
	none := upstreamChoice{clientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, remoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}
	return authClientResolution{outcome: authClientGap, reason: reason, clientID: uuid.Nil, clientType: "", serverName: "", tunneled: false, choice: none, headers: nil, noAuthAllowed: false, attachedSession: uuid.Nil}
}

// decideAuthClient resolves a recorded choice without side effects.
func decideAuthClient(row upstreamclientsrepo.GetAuthClientSelectionRow) authClientResolution {
	res := gapResolution("")
	res.serverName = row.ServerName.String
	res.tunneled = row.Tunneled
	res.noAuthAllowed = row.RequiresUpstreamAuth.Valid && !row.RequiresUpstreamAuth.Bool
	if row.Ambiguous {
		res.reason = authClientReasonAmbiguous
		return res
	}
	if !row.AuthClientID.Valid {
		if !row.NoAuth {
			// An OAuth choice recorded before choices name auth clients.
			res.outcome = authClientLegacy
			res.reason = authClientReasonUnmapped
			return res
		}
		switch {
		case row.ServerHasHeaderClients && !row.ServerHasOauthClients:
			// A no-auth choice from before the backfill gave the server its
			// header clients; the old path still serves it with the server's
			// headers.
			res.outcome = authClientLegacy
			res.reason = authClientReasonUnmapped
			if res.noAuthAllowed && !row.CredentialHeadersUncovered {
				// The server declares it needs no authentication, so the
				// choice stays usable where nothing falls back to the old path.
				res.reason = authClientReasonNoAuthDeclared
			}
		case row.RequiresUpstreamAuth.Valid && row.RequiresUpstreamAuth.Bool:
			res.reason = authClientReasonNoAuthForbidden
		case !row.RequiresUpstreamAuth.Valid && row.ServerHasClients:
			res.reason = authClientReasonNoAuthStale
		case row.CredentialHeadersUncovered:
			res.reason = authClientReasonSetup
		default:
			res.outcome = authClientNoAuth
		}
		return res
	}
	res.clientID = row.AuthClientID.UUID
	switch {
	case !row.ClientID.Valid:
		res.reason = authClientReasonClientGone
		return res
	case row.ClientDeleted.Bool:
		res.reason = authClientReasonClientDeleted
		return res
	case !row.ClientEnabled.Bool:
		res.reason = authClientReasonClientDisabled
		return res
	case !row.Associated:
		res.reason = authClientReasonUnassociated
		return res
	}
	res.clientType = row.ClientType.String
	switch res.clientType {
	case authClientTypeRemoteSession, authClientTypeServiceAccount:
		switch {
		case !row.RegistrationID.Valid || !row.RegistrationLive:
			res.reason = authClientReasonRegistration
		case !row.RegistrationAttached:
			res.reason = authClientReasonDetached
		case row.HeadersStale && !row.Tunneled:
			// The client's copies of the server's auxiliary credential
			// headers no longer match it; the old path sends the current ones.
			// A tunneled server is sent no client headers, so nothing stale
			// reaches it. The registration validated above still names the
			// client's permit; the old path replaces the choice it serves.
			res.outcome = authClientLegacy
			res.reason = authClientReasonHeadersStale
			res.choice = upstreamChoice{clientID: row.RegistrationID, remoteSessionIssuerID: row.RegistrationIssuerID}
		case row.CredentialHeadersUncovered, row.RequiresDeniedSource:
			res.reason = authClientReasonSetup
		case res.clientType == authClientTypeRemoteSession && row.RegistrationHasEma:
			// The upstream client choice may chain an identity for this
			// registration when its token is unusable; the resolver does not.
			res.outcome = authClientLegacy
			res.reason = authClientReasonLegacyMayChain
			res.choice = upstreamChoice{clientID: row.RegistrationID, remoteSessionIssuerID: row.RegistrationIssuerID}
		default:
			res.outcome = authClientResolved
			res.choice = upstreamChoice{clientID: row.RegistrationID, remoteSessionIssuerID: row.RegistrationIssuerID}
		}
	case authClientTypeStaticCredential, authClientTypePassThrough:
		switch {
		case row.Tunneled:
			// Tunneled servers are sent no configured headers yet; serving a
			// client's headers through a tunnel is not designed.
			res.outcome = authClientLegacy
			res.reason = authClientReasonTunneledHeaders
		case row.HeadersStale:
			res.outcome = authClientLegacy
			res.reason = authClientReasonHeadersStale
		case row.CredentialHeadersUncovered, row.RequiresDeniedSource:
			res.reason = authClientReasonSetup
		default:
			res.outcome = authClientResolved
		}
	case authClientTypeEMA:
		res.outcome = authClientLegacy
		res.reason = authClientReasonEMA
	default:
		res.reason = authClientReasonUnknownType
	}
	return res
}

// resolveAuthClient reads the credential's choice for serverID and decides it,
// without checking permission, acquiring any credential or loading client
// headers.
func (u *upstreamSelector) resolveAuthClient(ctx context.Context, serverID uuid.UUID) (authClientResolution, error) {
	choices, err := u.resolveAuthClients(ctx, []uuid.UUID{serverID})
	if err != nil {
		return gapResolution(""), err
	}
	if res, ok := choices[serverID]; ok {
		return res, nil
	}
	return gapResolution(authClientReasonMissingChoice), nil
}

func (u *upstreamSelector) resolveAuthClients(ctx context.Context, serverIDs []uuid.UUID) (map[uuid.UUID]authClientResolution, error) {
	rows, err := upstreamclientsrepo.New(u.service.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
		Proposed: false, ProposedRegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CredentialKind: u.credentialKindOrDefault(), CredentialID: u.credentialID,
		McpServerIds: serverIDs, OrganizationID: u.organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("load auth client choices: %w", err)
	}
	choices := make(map[uuid.UUID]authClientResolution, len(rows))
	for _, row := range rows {
		choices[row.McpServerID] = decideAuthClient(row)
	}
	return choices, nil
}

// resolveAuthClientForServing resolves the credential's client for serverID
// as a request will use it: checked against the permitter, with the client's
// headers loaded. The caller turns the outcome into a response, so direct
// endpoints and gateway members can answer it their own way.
func (s *Service) resolveAuthClientForServing(ctx context.Context, selector *upstreamSelector, serverID, projectID uuid.UUID, subject urn.SessionSubject) (authClientResolution, error) {
	res, err := selector.resolveAuthClient(ctx, serverID)
	if err != nil {
		return res, err
	}
	resolved := res.outcome == authClientResolved
	req := authClientPermitRequest{
		OrganizationID: selector.organizationID, ProjectID: projectID, ServerID: serverID, ClientID: res.clientID, ClientType: res.clientType, RegistrationID: res.choice.clientID,
		CredentialKind: selector.credentialKindOrDefault(), CredentialID: selector.credentialID, Subject: subject,
		UserSessionIssuerID: selector.userSessionIssuerID, ConnectionOwner: "",
	}
	return s.prepareAuthClient(ctx, req, res, func(permit authClientPermitRequest, _ bool) {
		if resolved {
			// The shadow compares the mandatory check with the
			// connect-equivalent rule served before it, whichever of the two
			// decided here.
			legacyPermitted, _ := connectPermitter{}.Permitted(ctx, permit)
			s.shadowAuthClientUse(ctx, selector.organizationID, legacyPermitted, projectID, res.clientID, res.choice.clientID, res.clientType, permit.ConnectionOwner)
		}
	})
}

// resolveAuthClientForConsent validates a proposed choice through the same
// resolver as an established credential, before a credential row exists.
func (s *Service) resolveAuthClientForConsent(ctx context.Context, organizationID string, serverID, projectID uuid.UUID, registrationID uuid.NullUUID, subject urn.SessionSubject) (authClientResolution, error) {
	rows, err := upstreamclientsrepo.New(s.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
		Proposed: true, ProposedRegistrationID: registrationID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: uuid.Nil, McpServerIds: []uuid.UUID{serverID}, OrganizationID: organizationID,
	})
	if err != nil {
		return gapResolution(authClientReasonMissingChoice), fmt.Errorf("resolve proposed auth client: %w", err)
	}
	if len(rows) != 1 {
		return gapResolution(authClientReasonMissingChoice), nil
	}
	row := rows[0]
	res := decideAuthClient(row)
	if res.outcome == authClientLegacy {
		res.choice = upstreamChoice{clientID: row.RegistrationID, remoteSessionIssuerID: row.RegistrationIssuerID}
	}

	return s.prepareAuthClient(ctx, authClientPermitRequest{OrganizationID: organizationID, ProjectID: projectID, ServerID: serverID, ClientID: res.clientID, ClientType: res.clientType, RegistrationID: res.choice.clientID, CredentialKind: upstreamclients.CredentialKindUserSession, CredentialID: uuid.Nil, Subject: subject, UserSessionIssuerID: uuid.Nil, ConnectionOwner: ""}, res, nil)
}

func (s *Service) prepareAuthClient(ctx context.Context, permit authClientPermitRequest, res authClientResolution, observe func(permit authClientPermitRequest, permitted bool)) (authClientResolution, error) {
	res, err := s.checkAuthClientPermission(ctx, permit, res, observe)
	if err != nil {
		return res, err
	}
	if res.sendsClientHeaders() {
		headers, err := s.loadAuthClientHeaders(ctx, permit.OrganizationID, []uuid.UUID{res.clientID})
		if err != nil {
			return res, err
		}
		clientHeaders := headers[res.clientID]
		if clientHeaders.err != nil {
			return res, clientHeaders.err
		}
		res = applyAuthClientHeaders(res, clientHeaders.headers)
	}
	annotateAuthClient(ctx, s.logger, res)
	return res, nil
}

// observe, when set, receives the permit request, with its connection owner
// resolved, and the decision before it is applied.
func (s *Service) checkAuthClientPermission(ctx context.Context, permit authClientPermitRequest, res authClientResolution, observe func(permit authClientPermitRequest, permitted bool)) (authClientResolution, error) {
	// A mapped client keeps its permit even when serving falls back to the
	// registration or current server headers.
	if res.outcome == authClientResolved || (res.outcome == authClientLegacy && res.clientID != uuid.Nil) {
		permitter, err := s.authClientPermitFor(ctx, permit.OrganizationID)
		if err != nil {
			return res, err
		}
		connection, err := s.authClientConnection(ctx, permit)
		if err != nil {
			return res, fmt.Errorf("check auth client permission: %w", err)
		}
		permit.ConnectionOwner = connection.owner
		res.attachedSession = connection.attachedSession
		permitted, err := permitter.Permitted(ctx, permit)
		if err != nil {
			return res, fmt.Errorf("check auth client permission: %w", err)
		}
		if observe != nil {
			observe(permit, permitted)
		}
		if !permitted {
			res.outcome = authClientNoAccess
			res.reason = authClientReasonNotPermitted
		}
	}
	return res, nil
}

// resolveEnforcedAuthClient resolves a server's client for an organization
// that enforces. A choice that names no auth client (an unmapped or
// ambiguous one) is a gap, never served by the upstream client choice. A
// named client that hands the request back to the upstream client choice,
// for a compatibility path the resolver does not serve itself, has already
// passed the mandatory check for that client in checkAuthClientPermission.
func (s *Service) resolveEnforcedAuthClient(ctx context.Context, selector *upstreamSelector, serverID, projectID uuid.UUID, subject urn.SessionSubject) (authClientResolution, error) {
	res, err := s.resolveAuthClientForServing(ctx, selector, serverID, projectID, subject)
	if err != nil || res.outcome != authClientLegacy {
		return res, err
	}
	if res.clientID == uuid.Nil {
		res = withoutLegacyFallback(res)
		annotateAuthClient(ctx, s.logger, res)
	}
	return res, nil
}

// withoutLegacyFallback decides a choice that names no auth client where the
// upstream client choice does not serve it: no-auth on a server that declares
// it needs none, otherwise a gap.
func withoutLegacyFallback(res authClientResolution) authClientResolution {
	if res.reason == authClientReasonNoAuthDeclared {
		res.outcome = authClientNoAuth
		res.reason = ""
		return res
	}
	res.outcome = authClientGap
	return res
}

func applyAuthClientHeaders(res authClientResolution, headers []proxy.ConfiguredHeader) authClientResolution {
	var blocked bool
	res.headers, blocked = withoutDeniedSources(headers)
	// A headers-only client must not silently become an anonymous connection.
	if blocked || (res.headersOnly() && len(res.headers) == 0 && !res.noAuthAllowed) {
		res.outcome = authClientGap
		res.reason = authClientReasonSetup
		res.headers = nil
	}
	return res
}

// refuseForeignCaller refuses, once the organization enforces, a caller
// authenticated to another organization, before any upstream credential is
// used. Public servers otherwise admit such a caller, so every public serving
// path runs it: the ungated identity probes and the issuer gate. The caller is
// never served as anonymous instead. A caller with no organization (an
// organization-less session) carries no other tenant's identity or grants and
// is served as anonymous callers are. Before enforcement a foreign caller is
// served as today, with no project context.
func (s *Service) refuseForeignCaller(ctx context.Context, organizationID string) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" || authCtx.ActiveOrganizationID == organizationID {
		return nil
	}
	mode, err := s.authClientMode(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load upstream auth enforcement").LogError(ctx, s.logger)
	}
	if mode != feature.AuthClientResolverEnforced {
		return nil
	}
	return oops.E(oops.CodeForbidden, nil, "this credential belongs to another organization, and this MCP server only accepts its own organization's credentials").LogWarn(ctx, s.logger, attr.SlogOrganizationID(organizationID))
}

// authClientPermitFor is how use of a resolved client is decided for
// organizationID: the mandatory mcp:auth check once the organization enforces
// and authz is configured, and the connect-equivalent rule otherwise, so a
// serving path that resolves typed auth clients before enforcement (a
// connection catalog, consent) keeps today's access. A test override decides
// every organization.
func (s *Service) authClientPermitFor(ctx context.Context, organizationID string) (authClientPermitter, error) {
	if s.authClientPermitter != nil {
		return s.authClientPermitter, nil
	}
	mode, err := s.authClientMode(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if mode != feature.AuthClientResolverEnforced {
		return connectPermitter{}, nil
	}
	// An enforcing organization never falls back to the connect-equivalent
	// rule: without the engine its clients are refused.
	if s.authz == nil {
		return nil, errors.New("upstream auth enforcement needs the authorization engine")
	}
	return mandatoryPermitter{engine: s.authz}, nil
}

// withoutDeniedSources drops client headers that read a denied inbound header,
// such as Authorization, which on an issuer-gated endpoint carries the
// caller's Speakeasy session token. It reports whether a dropped header was
// required, which the client cannot serve without.
func withoutDeniedSources(headers []proxy.ConfiguredHeader) ([]proxy.ConfiguredHeader, bool) {
	kept := make([]proxy.ConfiguredHeader, 0, len(headers))
	blocked := false
	for _, h := range headers {
		if h.ValueFromRequestHeader != "" && proxy.IsDeniedPassThroughSource(h.ValueFromRequestHeader) {
			blocked = blocked || h.IsRequired
			continue
		}
		kept = append(kept, h)
	}
	return kept, blocked
}

// authClientHeaders keeps one client's invalid secret from failing other members.
type authClientHeaders struct {
	// headers are the revealed values this client may send.
	headers []proxy.ConfiguredHeader
	// err is a client-specific decryption failure.
	err error
}

// loadAuthClientHeaders reads and reveals resolved clients' own headers.
func (s *Service) loadAuthClientHeaders(ctx context.Context, organizationID string, clientIDs []uuid.UUID) (map[uuid.UUID]authClientHeaders, error) {
	if len(clientIDs) == 0 {
		return nil, nil
	}
	rows, err := upstreamclientsrepo.New(s.db).ListAuthClientHeaders(ctx, upstreamclientsrepo.ListAuthClientHeadersParams{
		AuthClientIds:  clientIDs,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list auth client headers: %w", err)
	}
	headers := make(map[uuid.UUID]authClientHeaders)
	for _, row := range rows {
		current := headers[row.AuthClientID]
		if current.err != nil {
			continue
		}
		value := row.Value.String
		if row.IsSecret && value != "" {
			if value, err = s.enc.Decrypt(value); err != nil {
				headers[row.AuthClientID] = authClientHeaders{headers: nil, err: fmt.Errorf("decrypt auth client header %s: %w", row.Name, err)}
				continue
			}
		}
		current.headers = append(current.headers, proxy.ConfiguredHeader{
			IsRequired:             row.IsRequired,
			Name:                   row.Name,
			StaticValue:            value,
			ValueFromRequestHeader: row.ValueFromRequestHeader.String,
		})
		headers[row.AuthClientID] = current
	}
	return headers, nil
}

// annotateAuthClient records the resolution on the request span and, at debug
// level, the log. Ids and types only: header values never leave the resolver.
func annotateAuthClient(ctx context.Context, logger *slog.Logger, res authClientResolution) {
	attrs := []attribute.KeyValue{attr.AuthClientOutcome(string(res.outcome))}
	logAttrs := []any{attr.SlogAuthClientOutcome(string(res.outcome))}
	if res.clientID != uuid.Nil {
		attrs = append(attrs, attr.AuthClientID(res.clientID.String()))
		logAttrs = append(logAttrs, attr.SlogAuthClientID(res.clientID.String()))
	}
	if res.clientType != "" {
		attrs = append(attrs, attr.AuthClientType(res.clientType))
		logAttrs = append(logAttrs, attr.SlogAuthClientType(res.clientType))
	}
	if res.reason != "" {
		attrs = append(attrs, attr.AuthClientReason(res.reason))
		logAttrs = append(logAttrs, attr.SlogAuthClientReason(res.reason))
	}
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
	logger.DebugContext(ctx, "auth client resolved", logAttrs...)
}

// enforceAuthClient applies the resolver to a direct proxied endpoint. It
// reports false when the upstream client choice serves the request. A gap
// writes the reconnect challenge and a refused client a request-access error.
func (s *Service) enforceAuthClient(ctx context.Context, w http.ResponseWriter, authentication *issuerGateAuthentication, selector *upstreamSelector) (upstreamChoice, bool, error) {
	var none upstreamChoice
	endpoint := authentication.endpoint
	res, err := s.resolveEnforcedAuthClient(ctx, selector, endpoint.McpServerID.UUID, endpoint.ProjectID, authentication.subject)
	if err != nil {
		return none, true, oops.E(oops.CodeUnexpected, err, "resolve auth client").LogError(ctx, s.logger)
	}
	// The connection the permit was decided for binds every token path,
	// including a handback to the upstream client choice.
	authentication.attachedSession = res.attachedSession
	switch res.outcome {
	case authClientLegacy:
		if res.sendsClientHeaders() {
			authentication.chainingAuthClient = &res
		}
		return none, false, nil
	case authClientGap:
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: auth client unusable: "+res.reason, issuerGateReasonUpstreamSelectionUnavailable)
		return none, true, writeRemoteSessionReconnectChallenge(w, authentication)
	case authClientNoAccess:
		return none, true, s.authClientNoAccessError(ctx, endpoint.McpServerID.UUID, res)
	case authClientNoAuth:
		authentication.chosenClient = none.clientID
		authentication.chosenNoAuth = true
		authentication.authClient = &res
		return none, true, nil
	case authClientResolved:
	}
	authentication.authClient = &res
	if res.headersOnly() {
		authentication.chosenClient = none.clientID
		authentication.chosenNoAuth = true
		return none, true, nil
	}
	authentication.chosenClient = res.choice.clientID
	authentication.chosenNoAuth = false
	return res.choice, true, nil
}

// authClientNoAccessError refuses a caller the chosen client, with a link to
// request access to the server.
func (s *Service) authClientNoAccessError(ctx context.Context, serverID uuid.UUID, res authClientResolution) error {
	message := "you don't have access to the connection this MCP server uses. Ask an organization administrator to grant access."
	if link := strings.TrimSpace(s.requestAccessURL(ctx, serverID.String(), res.serverName)); link != "" {
		message += "\n\nRequest access:\n" + link
	}
	return oops.E(oops.CodeForbidden, nil, "%s", message).LogWarn(ctx, s.logger, attr.SlogAuthClientID(res.clientID.String()), attr.SlogAuthClientReason(res.reason))
}

// refreshNeedsAuthClientReconnect is the resolver's view of a refresh for an
// organization it serves: whether the refreshed session must reconnect, and
// whether the resolver decided at all. Undecided, the upstream client choice
// decides as before.
func (s *Service) refreshNeedsAuthClientReconnect(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, jti string) (reconnect bool, decided bool, err error) {
	if !endpoint.McpServerID.Valid {
		return false, false, nil
	}
	mode, err := s.authClientMode(ctx, endpoint.OrganizationID)
	if err != nil {
		return false, false, err
	}
	if mode != feature.AuthClientResolverEnforced {
		return false, false, nil
	}
	selector, err := s.refreshSelector(ctx, endpoint, jti)
	if err != nil {
		return false, false, err
	}
	res, err := selector.resolveAuthClient(ctx, endpoint.McpServerID.UUID)
	if err != nil {
		return false, false, err
	}
	if res.outcome == authClientResolved && res.choice.clientID.Valid {
		// The resolved registration's stored grant decides, whichever sign-in
		// it was consented through.
		reconnect, err := s.remoteChallengeMgr.AuthClientNeedsReconnect(ctx, remotesessions.AuthClientTokenRequest{
			ProjectID:           endpoint.ProjectID,
			OrganizationID:      endpoint.OrganizationID,
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Subject:             subject,
			RegistrationID:      res.choice.clientID.UUID,
			SkipUnusable:        false,
		})
		if err != nil {
			return false, false, fmt.Errorf("check auth client reconnect: %w", err)
		}
		return reconnect, true, nil
	}
	reconnect, decided = refreshDecision(res)
	return reconnect, decided, nil
}

// refreshDecision is what a resolution means for a refresh: a gap reconnects,
// and a choice that needs no upstream session refreshes. OAuth clients and
// choices served the old way are left to the upstream client choice.
func refreshDecision(res authClientResolution) (reconnect bool, decided bool) {
	switch {
	case res.outcome == authClientGap:
		return true, true
	case res.outcome == authClientNoAuth, res.headersOnly():
		return false, true
	}
	return false, false
}

// legacyChoiceOutcome names the upstream client choice's outcome in the
// resolver's terms, for shadow comparison.
func legacyChoiceOutcome(choice upstreamChoice, err error) authClientOutcome {
	switch {
	case err != nil:
		return authClientGap
	case choice.noAuth():
		return authClientNoAuth
	default:
		return authClientResolved
	}
}

// shadowAuthClient compares the resolver with the upstream client choice for
// the same request and reports disagreement. It never changes the request and
// never asks the permitter.
func (s *Service) shadowAuthClient(ctx context.Context, selector *upstreamSelector, serverID, projectID uuid.UUID, subject urn.SessionSubject, choice upstreamChoice, choiceErr error) {
	res, err := selector.resolveAuthClient(ctx, serverID)
	if err != nil {
		s.metrics.RecordAuthClientParity(ctx, false, authClientParityError, string(legacyChoiceOutcome(choice, choiceErr)), authClientReasonNone)
		s.logger.WarnContext(ctx, "auth client resolver shadow failed", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
		return
	}
	// Compare only a legacy decision: a missing or unusable choice refuses
	// the request, any other error is not a decision.
	legacyDenied := errors.Is(choiceErr, errUpstreamSelectionMissing) || errors.Is(choiceErr, errUpstreamSelectionUnusable)
	if res.outcome == authClientResolved && (choiceErr == nil || legacyDenied) {
		connection, err := s.authClientConnection(ctx, authClientPermitRequest{
			OrganizationID: selector.organizationID, ProjectID: projectID, ServerID: serverID, ClientID: res.clientID, ClientType: res.clientType, RegistrationID: res.choice.clientID,
			CredentialKind: selector.credentialKindOrDefault(), CredentialID: selector.credentialID, Subject: subject, UserSessionIssuerID: selector.userSessionIssuerID, ConnectionOwner: "",
		})
		if err != nil {
			s.logger.WarnContext(ctx, "auth client shadow could not resolve the connection owner", attr.SlogMcpServerID(serverID.String()), attr.SlogError(err))
		} else {
			s.shadowAuthClientUse(ctx, selector.organizationID, choiceErr == nil, projectID, res.clientID, res.choice.clientID, res.clientType, connection.owner)
		}
	}
	annotateAuthClient(ctx, s.logger, res)
	s.reportAuthClientParity(ctx, selector.organizationID, serverID, res, choice, choiceErr)
}

// reportAuthClientParity counts one shadow comparison and logs a
// disagreement, ids and outcomes only. A choice the resolver hands back to the
// upstream client choice is compared only when that hand-back hides a
// difference; an unmapped choice is counted without a warning.
func (s *Service) reportAuthClientParity(ctx context.Context, organizationID string, serverID uuid.UUID, res authClientResolution, choice upstreamChoice, choiceErr error) {
	if choiceErr != nil && !errors.Is(choiceErr, errUpstreamSelectionMissing) && !errors.Is(choiceErr, errUpstreamSelectionUnusable) {
		return
	}
	legacy := legacyChoiceOutcome(choice, choiceErr)
	var agreed bool
	warn := true
	switch {
	case res.outcome == authClientLegacy && (res.reason == authClientReasonUnmapped || res.reason == authClientReasonNoAuthDeclared):
		agreed, warn = false, false
	case res.outcome == authClientLegacy && (res.reason == authClientReasonHeadersStale || res.reason == authClientReasonLegacyMayChain):
		agreed = false
	case res.outcome == authClientLegacy:
		return
	default:
		// The upstream client choice serves a headers-only client as no-auth.
		expected := res.outcome
		if res.headersOnly() {
			expected = authClientNoAuth
		}
		agreed = legacy == expected
		if agreed && expected == authClientResolved {
			agreed = res.choice.clientID == choice.clientID
		}
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.AuthClientLegacyOutcome(string(legacy)), attr.AuthClientParityAgreed(agreed))
	reason := res.reason
	if reason == "" {
		reason = authClientReasonNone
	}
	s.metrics.RecordAuthClientParity(ctx, agreed, string(res.outcome), string(legacy), reason)
	if agreed || !warn {
		return
	}
	s.logger.WarnContext(ctx, "auth client resolver parity mismatch",
		attr.SlogMcpServerID(serverID.String()),
		attr.SlogOrganizationID(organizationID),
		attr.SlogAuthClientID(res.clientID.String()),
		attr.SlogAuthClientType(res.clientType),
		attr.SlogAuthClientOutcome(string(res.outcome)),
		attr.SlogAuthClientReason(res.reason),
		attr.SlogAuthClientLegacyOutcome(string(legacy)),
		attr.SlogRemoteSessionClientID(choice.clientID.UUID.String()),
	)
}

// authClientMode is the organization's resolver mode. The resolver is
// authoritative only for an organization staff enabled enforcement for, after
// its coverage was checked, and until they disable it; the flags alone only
// shadow. A state that cannot be read is an error, so the request is refused
// rather than served without the mandatory check.
func (s *Service) authClientMode(ctx context.Context, organizationID string) (feature.AuthClientResolverMode, error) {
	enforced, err := s.authEnforcement.Enforced(ctx, organizationID)
	if err != nil {
		return feature.AuthClientResolverOff, fmt.Errorf("read upstream auth enforcement: %w", err)
	}
	if enforced {
		return feature.AuthClientResolverEnforced, nil
	}
	if feature.AuthClientResolver(ctx, s.features, organizationID) == feature.AuthClientResolverOff {
		return feature.AuthClientResolverOff, nil
	}
	return feature.AuthClientResolverShadow, nil
}
