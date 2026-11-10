// The agent gateway MCP surface: one URL per agent, whose members are derived
// from the agent's own grants rather than from stored membership rows. It
// reuses the meta-server runtime wholesale — same tool contract, same
// member dispatch — so the only thing particular to it is how the request is
// authenticated and how membership is resolved.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/agentgateway"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AgentGatewayRoute is the gateway's path, addressed by agent rather than by
// slug so it can never be shadowed by a server whose slug is "agent".
const AgentGatewayRoute = "/agent-mcp/{agentID}"

// ServeAgentGateway terminates MCP for /agent-mcp/{agentID}. Only the agent
// whose key is presented may reach its own gateway: the key is authenticated
// first, then matched against the id in the path, so a valid key for another
// agent reads as not found rather than as a denial.
func (s *Service) ServeAgentGateway(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil || agentID == uuid.Nil {
		return oops.C(oops.CodeNotFound)
	}
	// The synthetic id is what every downstream record carries, so logging it
	// here is what ties those records back to this request.
	logger := s.logger.With(attr.SlogMetaMcpServerID(agentgateway.GatewayID(agentID).String()))

	authedCtx, authCtx, err := s.authenticateAgentGatewayKey(ctx, r, agentID)
	if err != nil {
		return err
	}
	r = r.WithContext(authedCtx)
	ctx = authedCtx

	// The same lockdown ServePublic applies, at the only scope this
	// surface has. enforceCustomDomainLockdown takes a project id purely to
	// read the owning organization off it, and the policy it then applies is
	// the organization's; an agent gateway has no single project, but its
	// organization is exactly what the agent key names. Skipping it would let a
	// valid key reach the same servers on the platform hostname, outside the
	// allowlist the org's ingress enforces.
	if err := s.enforceAgentGatewayCustomDomainLockdown(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return err
	}

	// Synthetic, never persisted: the meta runtime is shaped around rows, and
	// an agent gateway has none. Only the fields the runtime reads are
	// meaningful; the rest are zeroed rather than invented.
	metaServer := &metamcprepo.MetaMcpServer{
		ID:             agentgateway.GatewayID(agentID),
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.Nil,
		// No issuer: the agent key is the credential, so the OAuth issuer gate
		// must not run. Leaving this invalid is what keeps it off.
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:                "Agent gateway",
		// NULL serves Speakeasy's built-in gateway instructions.
		Instructions:      pgtype.Text{String: "", Valid: false},
		DiscoveryMode:     "progressive",
		Visibility:        visibility.Private,
		NetworkAccessMode: pgtype.Text{String: "", Valid: false},
		CreatedAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UpdatedAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		DeletedAt:         pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Deleted:           false,
	}
	endpoint := &mcpendpointsrepo.McpEndpoint{ //nolint:exhaustruct // synthetic: only the fields the meta runtime reads are meaningful
		ProjectID: uuid.Nil,
	}

	if err := s.serveResolvedMetaMCPEndpoint(w, r, logger, endpoint, metaServer, metaMembership{agentID: agentID, organization: false}, nil); err != nil {
		return fmt.Errorf("serve agent gateway: %w", err)
	}
	return nil
}

// enforceAgentGatewayCustomDomainLockdown 403s a platform-host agent gateway
// request when the caller's organization has an IP allowlist on its custom
// domain. The two exemptions are the ones customDomainLockdownApplies makes: a
// request that arrived through the private ingress is governed by that
// surface's own admission, and one that arrived on the custom domain already
// passed the allowlist at the edge.
//
// The custom-domain exemption is bound to the owning organization. A domain
// only proves its own organization's allowlist was applied, so another
// organization's domain must not excuse this one's lockdown.
func (s *Service) enforceAgentGatewayCustomDomainLockdown(ctx context.Context, logger *slog.Logger, organizationID string) error {
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface == requestorigin.SurfacePrivateNetwork {
		return nil
	}
	if domainCtx := customdomains.FromContext(ctx); domainCtx != nil && domainCtx.OrganizationID == organizationID {
		return nil
	}

	lockedDown, err := s.organizationCustomDomainLockdown(ctx, logger, organizationID)
	if err != nil {
		return err
	}
	if lockedDown {
		return oops.E(oops.CodeForbidden, nil, "this MCP server is only accessible via its custom domain")
	}
	return nil
}

// authenticateAgentGatewayKey admits only a live agent key whose subject is
// the agent named in the path.
func (s *Service) authenticateAgentGatewayKey(
	ctx context.Context,
	r *http.Request,
	agentID uuid.UUID,
) (context.Context, *contextvalues.AuthContext, error) {
	token := httpheaders.AuthorizationBearerToken(r)
	if token == "" {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}

	keyCtx, err := s.auth.AuthorizeWithPostAuthenticationCheck(ctx, token, &security.APIKeyScheme{
		Name:           constants.KeySecurityScheme,
		Scopes:         nil,
		RequiredScopes: []string{"consumer"},
	}, func(ctx context.Context) error {
		mode, keyOK := contextvalues.APIKeyAuthorization(ctx)
		actor, actorOK := contextvalues.AuthenticatedActor(ctx)
		// Principal mode and agent type together: a legacy or user key must not
		// reach a surface whose whole admission model is the agent's own policy.
		if !keyOK || mode != contextvalues.APIKeyAuthorizationModePrincipal || !actorOK || actor.Type != urn.PrincipalTypeAgent {
			return oops.C(oops.CodeUnauthorized)
		}
		if actor.ID != agentID.String() {
			// Another agent's key is a wrong address, not a denial.
			return oops.C(oops.CodeNotFound)
		}
		return nil
	})
	if err != nil {
		return ctx, nil, fmt.Errorf("authenticate agent gateway key: %w", err)
	}

	authCtx, ok := contextvalues.GetAuthContext(keyCtx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	return s.identityValidator.StampAgent(keyCtx, agentID), authCtx, nil
}

// resolveMemberTokens honors a selected member before serving an agent key
// without a choice for it by the member's own clients. An agent gateway has
// no issuer itself.
func (s *Service) resolveMemberTokens(ctx context.Context, gate *metaGateContext, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	tokens, _, err := s.resolveMemberTokensSkipping(ctx, gate, member)
	return tokens, err
}

// resolveMemberTokensSkipping is resolveMemberTokens that also returns the
// first of an agent member's clients left out for a missing connection
// (agentMemberTokens), for routing to name when no credential serves the
// member.
func (s *Service) resolveMemberTokensSkipping(ctx context.Context, gate *metaGateContext, member metaMember) (tokens map[uuid.UUID]remotesessions.UpstreamToken, skipped error, err error) {
	if gate.agentID == uuid.Nil || (gate.memberChoices != nil && gate.memberChoices.serves(member)) {
		tokens, err := gate.memberTokens(ctx, member)
		if err == nil && gate.agentID != uuid.Nil && member.chosenClientID.Valid {
			usable := false
			for _, token := range tokens {
				usable = usable || token.Token != "" || token.SessionUnavailable()
			}
			if !usable {
				if shared := sharedCredentialError(member, tokens); shared != nil {
					return nil, nil, shared
				}
				return nil, nil, gate.agentReconnectError(member)
			}
		}
		return tokens, nil, err
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.serverID, ProjectID: member.projectID})
	if err != nil {
		return nil, nil, fmt.Errorf("load agent member issuer: %w", err)
	}
	// A member must never turn a missing attachment into an anonymous
	// upstream request.
	if !server.UserSessionIssuerID.Valid {
		_, credential, err := s.authorizeAgentMemberClient(ctx, gate, member, uuid.Nil, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "")
		gate.recordServedCredential(uuid.Nil, credential)
		return nil, nil, err
	}
	clients, err := s.agentServerClients(ctx, agentServerTarget{organizationID: gate.organizationID, agentID: gate.agentID, serverID: member.serverID, projectID: member.projectID, slug: member.slug}, server.UserSessionIssuerID.UUID)
	if err != nil {
		return nil, nil, err
	}
	if len(clients) == 0 {
		// Distinguish a legitimate header/no-auth member from a required OAuth
		// member whose last client was removed. No runtime catalog repairs.
		_, credential, err := s.authorizeAgentMemberClient(ctx, gate, member, server.UserSessionIssuerID.UUID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "")
		gate.recordServedCredential(uuid.Nil, credential)
		return nil, nil, err
	}
	return s.agentMemberTokens(ctx, gate, member, server.UserSessionIssuerID.UUID, clients)
}

// agentMemberTokens resolves the agent's credential for each of a member's
// clients. A client the agent holds no usable connection of its own for does
// not fail a member another client serves: the member's clients can include
// another server's personal client beside its own service account. Any other
// failure, a refused use among them, refuses the member, as the server's own
// endpoint does (authorizeDirectAgentKey). skipped is the first client left
// out, which routing names when no credential serves the member after all.
func (s *Service) agentMemberTokens(ctx context.Context, gate *metaGateContext, member metaMember, issuer uuid.UUID, clients []remotesessions.Client) (tokens map[uuid.UUID]remotesessions.UpstreamToken, skipped error, err error) {
	tokens = make(map[uuid.UUID]remotesessions.UpstreamToken, len(clients))
	for _, client := range clients {
		resolved, err := s.agentMemberClientTokens(ctx, gate, member, issuer, client.ID, agentClientType(client))
		if agentConnectionMissing(err) {
			if skipped == nil {
				skipped = err
			}
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		maps.Copy(tokens, resolved)
	}
	if len(tokens) == 0 && skipped != nil {
		return nil, nil, skipped
	}
	return tokens, skipped, nil
}

// agentConnectionMissing reports a client the agent holds no usable
// connection of its own for. The agent's sign-in step fixes it, and it does
// not refuse a server another of its clients serves. A refused use is not
// one: it refuses the server on every surface.
func agentConnectionMissing(err error) bool {
	_, missing := errors.AsType[agentCredentialError](err)
	return missing
}

// agentMemberClientTokens authorizes one client of a member an agent key has
// no choice for and reads its credential from the connection the permit was
// decided for: the exact attachment, pinned, or for an unattached agent key
// its validated authorizer's own connection. Once the organization enforces,
// the member's auth client authorizes the registration without a sign-in
// link. A gate collecting served credentials records which auth client and
// connection the registration's tokens come from.
func (s *Service) agentMemberClientTokens(ctx context.Context, gate *metaGateContext, member metaMember, issuer, clientID uuid.UUID, clientType string) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	mode, err := s.authClientMode(ctx, gate.organizationID)
	if err != nil {
		return nil, err
	}
	connection, credential, err := s.authorizeAgentMemberClient(ctx, gate, member, issuer, uuid.NullUUID{UUID: clientID, Valid: true}, clientType)
	if err != nil {
		return nil, err
	}
	gate.recordServedCredential(clientID, credential)
	subject := urn.NewAgentSubject(gate.agentID)
	if connection.authorizer {
		subject = urn.NewUserSubject(connection.owner)
	}
	var resolved map[uuid.UUID]remotesessions.UpstreamToken
	if mode == feature.AuthClientResolverEnforced {
		resolved, err = s.upstreamTokenSource().ResolveAuthClientAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
			ProjectID:           member.projectID,
			OrganizationID:      gate.organizationID,
			UserSessionIssuerID: issuer,
			Subject:             subject,
			RegistrationID:      clientID,
			SkipUnusable:        false,
			Attribution:         remotesessions.RefreshIn(gate.connectionScope(ctx)).FromRequest(gate.request),
		})
	} else {
		resolved, err = s.upstreamTokenSource().ResolveClientAccessTokens(ctx, remotesessions.RefreshIn(gate.connectionScope(ctx)).FromRequest(gate.request), member.projectID, gate.organizationID, issuer, subject, clientID, false)
	}
	if errors.Is(err, remotesessions.ErrRemoteSessionUnavailable) {
		return nil, memberConnectionError{metaMemberClientCredentialError(member, err), resolveFailureHappened(err)}
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("resolve agent member credential: %w", ctx.Err())
	}
	// A service account's credential is the server's, shared by every
	// caller: the agent's sign-in step does not set it.
	if err != nil && clientType == authClientTypeServiceAccount {
		return nil, memberConnectionError{metaMemberClientCredentialError(member, err), resolveFailureHappened(err)}
	}
	if err != nil {
		return nil, memberConnectionError{gate.agentReconnectError(member), resolveFailureHappened(err)}
	}
	resolved, err = pinAttachedSession(resolved, connection.attachedSession, false)
	if err != nil {
		return nil, memberConnectionError{gate.agentReconnectError(member), resolveFailureHappened(err)}
	}
	return resolved, nil
}

// agentServerRefusal is the text every agent refusal from the shared
// authorization path carries, so surfaces other than the agent gateway read
// the same whatever the cause. The agent gateway replaces it with the
// cause's own fix (withAgentMemberFix).
func agentServerRefusal(slug string) *metaMemberError {
	return &metaMemberError{message: fmt.Sprintf("server %q's connection is unavailable or not permitted; reconnect it before retrying", slug)}
}

func agentServerCredentialError(slug string) error {
	return agentCredentialError{agentServerRefusal(slug)}
}

// agentCredentialError is an agent's connection to a server that is missing
// or unusable, which a person fixes by reconnecting it rather than a retry.
// It is not a memberCredentialError: the call's log row and connection
// attempts keep the resolve failure that caused it.
type agentCredentialError struct{ *metaMemberError }

func (e agentCredentialError) Unwrap() error { return e.metaMemberError }

// agentServerAccessError refuses an agent the use of a connection to a server
// that policy does not permit. Reconnecting does not change it.
func agentServerAccessError(slug string) error {
	return agentAccessError{agentServerRefusal(slug), false}
}

// agentServerGapError refuses an agent a server that no single auth client
// serves. The server's auth clients fix it, not the agent's permissions.
func agentServerGapError(slug string) error {
	return agentAccessError{agentServerRefusal(slug), true}
}

// agentAccessError is an agent refused the use of a connection to a server,
// which a person fixes in the agent's permissions or, when server is set,
// in the server's auth clients, not by reconnecting.
type agentAccessError struct {
	*metaMemberError
	server bool
}

func (e agentAccessError) Unwrap() error { return e.metaMemberError }

// authorizeAgentMemberClient checks the existing catalog and the exact attached
// source before acquiring a legacy agent credential. It does not create a choice
// or grant. Off/shadow keep their connect-equivalent policy; enforced uses the
// same owner-sensitive permit as selected members. It also names the auth
// client and connection owner a permitted use serves, for the call's log row.
func (s *Service) authorizeAgentMemberClient(ctx context.Context, gate *metaGateContext, member metaMember, issuer uuid.UUID, registration uuid.NullUUID, clientType string) (authClientConnectionOwner, contextvalues.UpstreamCredential, error) {
	connection, credential, err := s.authorizeAgentServerClient(ctx, agentServerTarget{organizationID: gate.organizationID, agentID: gate.agentID, serverID: member.serverID, projectID: member.projectID, slug: member.slug}, issuer, registration, clientType)
	return connection, credential, gate.withAgentMemberFix(member, err)
}

// agentServerClients lists the OAuth registrations that serve the target
// server for an agent key with no choice for it. Once the organization
// enforces, they are the registrations of the server's own enabled personal
// and service-account auth clients, whether or not the sign-in links them.
// Before that, they are the clients issuer binds that belong to the server;
// an issuer can also bind clients of sibling servers, which never serve this
// one.
func (s *Service) agentServerClients(ctx context.Context, target agentServerTarget, issuer uuid.UUID) ([]remotesessions.Client, error) {
	mode, err := s.authClientMode(ctx, target.organizationID)
	if err != nil {
		return nil, err
	}
	if mode == feature.AuthClientResolverEnforced {
		offered, err := authclientsrepo.New(s.db).ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{McpServerID: target.serverID, ProjectID: target.projectID, OrganizationID: target.organizationID})
		if err != nil {
			return nil, fmt.Errorf("load agent server auth clients: %w", err)
		}
		var registrations []uuid.UUID
		for _, client := range offered {
			oauthBacked := client.ClientType == authClientTypeRemoteSession || client.ClientType == authClientTypeServiceAccount
			if client.Enabled && oauthBacked && client.RemoteSessionClientID.Valid && !slices.Contains(registrations, client.RemoteSessionClientID.UUID) {
				registrations = append(registrations, client.RemoteSessionClientID.UUID)
			}
		}
		clients, err := s.remoteChallengeMgr.ListAuthClientRegistrations(ctx, target.projectID, target.organizationID, registrations)
		if err != nil {
			return nil, fmt.Errorf("load agent server registrations: %w", err)
		}
		return clients, nil
	}
	clients, err := s.remoteChallengeMgr.ListClients(ctx, target.projectID, target.organizationID, issuer)
	if err != nil {
		return nil, fmt.Errorf("load agent server clients: %w", err)
	}
	associated, err := s.serverAssociatedClients(ctx, target.organizationID, []uuid.UUID{target.serverID})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(clients, func(client remotesessions.Client) bool {
		return !associated[target.serverID][client.ID]
	}), nil
}

// agentClientType is the auth client type of an issuer client an agent uses.
func agentClientType(client remotesessions.Client) string {
	if client.CredentialOwner == remotesessions.CredentialOwnerSelf {
		return authClientTypeServiceAccount
	}
	return authClientTypeRemoteSession
}

// agentServerTarget is the server an agent key reaches, through its agent
// gateway or on the server's own endpoint.
type agentServerTarget struct {
	organizationID string
	agentID        uuid.UUID
	serverID       uuid.UUID
	projectID      uuid.UUID
	slug           string
}

// authorizeAgentServerClient is authorizeAgentMemberClient for any surface an
// agent key reaches a server through, so the server's own endpoint applies
// the same catalog, attachment and mcp:auth checks as the agent gateway.
func (s *Service) authorizeAgentServerClient(ctx context.Context, target agentServerTarget, issuer uuid.UUID, registration uuid.NullUUID, clientType string) (authClientConnectionOwner, contextvalues.UpstreamCredential, error) {
	var none authClientConnectionOwner
	var noCredential contextvalues.UpstreamCredential
	rows, err := upstreamclientsrepo.New(s.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
		ProposeAllClients: false,
		Proposed:          true, ProposedNoAuth: false, ProposedAuthClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ProposedRegistrationID: registration, CredentialKind: upstreamclients.CredentialKindUserSession,
		CredentialID: uuid.Nil, McpServerIds: []uuid.UUID{target.serverID}, OrganizationID: target.organizationID,
	})
	if err != nil {
		return none, noCredential, fmt.Errorf("load agent member auth client: %w", err)
	}
	if len(rows) != 1 {
		return none, noCredential, agentServerGapError(target.slug)
	}
	res := decideAuthClient(rows[0])
	mode, err := s.authClientMode(ctx, target.organizationID)
	if err != nil {
		return none, noCredential, err
	}
	// Before typed enforcement, configured legacy credential headers remain
	// served by the proxy even when no catalog client has been provisioned.
	// This does not waive the enforced catalog/permit barrier.
	if mode != feature.AuthClientResolverEnforced && !registration.Valid && (rows[0].CredentialHeadersUncovered || (rows[0].ServerHasHeaderClients && !rows[0].ServerHasOauthClients && !rows[0].HeadersStale)) {
		// The proxy serves the server's own headers; name the header client
		// the catalog resolved for them, when it resolved one.
		return none, res.servedCredential(), nil
	}
	if (res.outcome == authClientGap && (mode == feature.AuthClientResolverEnforced || !registration.Valid)) || (res.outcome == authClientLegacy && res.clientID == uuid.Nil && mode == feature.AuthClientResolverEnforced) {
		return none, noCredential, agentServerGapError(target.slug)
	}
	permit := authClientPermitRequest{
		OrganizationID: target.organizationID, ProjectID: target.projectID, ServerID: target.serverID,
		ClientID: res.clientID, ClientType: clientType, RegistrationID: registration,
		Subject: urn.NewAgentSubject(target.agentID), UserSessionIssuerID: issuer,
		CredentialKind: "", CredentialID: uuid.Nil, ConnectionOwner: "",
	}
	if res.clientType != "" {
		permit.ClientType = res.clientType
	}
	connection, err := s.authClientConnection(ctx, permit)
	if err != nil {
		return none, noCredential, err
	}
	// A personal client is served by the exact attachment, or for an
	// unattached agent key by its validated authorizer's own connection. That
	// fallback needs the mandatory mcp:auth check on a catalog client, which
	// names the authorizer as the owner: before enforcement no such check runs.
	if connection.authorizer && (mode != feature.AuthClientResolverEnforced || res.clientID == uuid.Nil) {
		connection = none
	}
	if permit.ClientType == authClientTypeRemoteSession && connection.attachedSession == uuid.Nil && !connection.authorizer {
		return none, noCredential, agentServerCredentialError(target.slug)
	}
	permit.ConnectionOwner = connection.owner
	if res.clientID != uuid.Nil {
		permitter, err := s.authClientPermitFor(ctx, target.organizationID)
		if err != nil {
			return none, noCredential, err
		}
		allowed, err := permitter.Permitted(ctx, permit)
		if err != nil {
			return none, noCredential, fmt.Errorf("authorize agent member client: %w", err)
		}
		if !allowed {
			return none, noCredential, agentServerAccessError(target.slug)
		}
	}
	// A gap leaves the registration serving its token without the auth client
	// it named, so that client did not sign the call in.
	servedClientID := res.clientID
	if res.outcome == authClientGap {
		servedClientID = uuid.Nil
	}
	return connection, servedCredential(servedClientID, permit.ClientType, connection.owner), nil
}
