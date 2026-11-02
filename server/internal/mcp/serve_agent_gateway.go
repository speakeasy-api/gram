// The agent gateway MCP surface: one URL per agent, whose members are derived
// from the agent's own grants rather than from stored membership rows. It
// reuses the meta-server runtime wholesale — same tool contract, same
// member dispatch — so the only thing particular to it is how the request is
// authenticated and how membership is resolved.

package mcp

import (
	"context"
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

	"github.com/speakeasy-api/gram/server/internal/attr"
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

// agentGatewayNamespace derives the synthetic meta-server id an agent gateway
// reports. There is no meta_mcp_servers row behind it, so it must never be
// used to read one back; it exists so telemetry and session records group per
// agent instead of collapsing onto a single nil id.
var agentGatewayNamespace = uuid.MustParse("a6e17a1e-0000-4000-8000-000000000001")

func agentGatewayID(agentID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(agentGatewayNamespace, agentID[:])
}

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
	logger := s.logger.With(attr.SlogMetaMcpServerID(agentGatewayID(agentID).String()))

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
		ID:             agentGatewayID(agentID),
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

// resolveMemberTokens honors a selected member before falling back to the
// member issuer for legacy agent keys. An agent gateway has no issuer itself.
func (s *Service) resolveMemberTokens(ctx context.Context, gate *metaGateContext, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if gate.agentID == uuid.Nil || (gate.memberChoices != nil && member.backend == metaMemberBackendProxied && gate.memberChoices.serves(member)) {
		tokens, err := gate.memberTokens(ctx, member)
		if err == nil && gate.agentID != uuid.Nil && member.chosenClientID.Valid {
			usable := false
			for _, token := range tokens {
				usable = usable || token.Token != ""
			}
			if !usable {
				return nil, agentMemberCredentialError(member)
			}
		}
		return tokens, err
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: member.serverID, ProjectID: member.projectID})
	if err != nil {
		return nil, fmt.Errorf("load agent member issuer: %w", err)
	}
	if !server.UserSessionIssuerID.Valid {
		if member.backend == metaMemberBackendProxied {
			_, err := s.authorizeAgentMemberClient(ctx, gate, member, uuid.Nil, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "")
			return nil, err
		}
		return nil, nil
	}
	// Hosted members retain partial credentials: their tools declare their own
	// inputs. Proxied members must never turn a missing attachment into an
	// anonymous upstream request.
	if member.backend != metaMemberBackendProxied {
		tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, member.projectID, gate.organizationID, server.UserSessionIssuerID.UUID, urn.NewAgentSubject(gate.agentID))
		if err != nil {
			return nil, fmt.Errorf("resolve hosted member credentials: %w", err)
		}
		return tokens, nil
	}
	clients, err := s.remoteChallengeMgr.ListClients(ctx, member.projectID, gate.organizationID, server.UserSessionIssuerID.UUID)
	if err != nil {
		return nil, fmt.Errorf("load agent member clients: %w", err)
	}
	associated, err := s.serverAssociatedClients(ctx, gate.organizationID, []uuid.UUID{member.serverID})
	if err != nil {
		return nil, err
	}
	// An issuer can also bind clients belonging to sibling servers.
	clients = slices.DeleteFunc(clients, func(client remotesessions.Client) bool {
		return !associated[member.serverID][client.ID]
	})
	if len(clients) == 0 {
		// Distinguish a legitimate header/no-auth member from a required OAuth
		// member whose last client was removed. No runtime catalog repairs.
		_, err := s.authorizeAgentMemberClient(ctx, gate, member, server.UserSessionIssuerID.UUID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "")
		return nil, err
	}
	tokens := make(map[uuid.UUID]remotesessions.UpstreamToken, len(clients))
	for _, client := range clients {
		clientType := authClientTypeRemoteSession
		if client.CredentialOwner == remotesessions.CredentialOwnerSelf {
			clientType = authClientTypeServiceAccount
		}
		resolved, err := s.agentMemberClientTokens(ctx, gate, member, server.UserSessionIssuerID.UUID, client.ID, clientType)
		if err != nil {
			return nil, err
		}
		maps.Copy(tokens, resolved)
	}
	return tokens, nil
}

// agentMemberClientTokens authorizes one legacy agent member client and reads
// its credential from the connection the permit was decided for: the exact
// attachment, pinned, or for an unattached agent key its validated
// authorizer's own connection.
func (s *Service) agentMemberClientTokens(ctx context.Context, gate *metaGateContext, member metaMember, issuer, clientID uuid.UUID, clientType string) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	connection, err := s.authorizeAgentMemberClient(ctx, gate, member, issuer, uuid.NullUUID{UUID: clientID, Valid: true}, clientType)
	if err != nil {
		return nil, err
	}
	subject := urn.NewAgentSubject(gate.agentID)
	if connection.authorizer {
		subject = urn.NewUserSubject(connection.owner)
	}
	resolved, err := s.upstreamTokenSource().ResolveClientAccessTokens(ctx, member.projectID, gate.organizationID, issuer, subject, clientID, false)
	if err != nil {
		return nil, agentMemberCredentialError(member)
	}
	resolved, err = pinAttachedSession(resolved, connection.attachedSession, false)
	if err != nil {
		return nil, agentMemberCredentialError(member)
	}
	return resolved, nil
}

func agentMemberCredentialError(member metaMember) error {
	return agentServerCredentialError(member.slug)
}

func agentServerCredentialError(slug string) error {
	return &metaMemberError{message: fmt.Sprintf("server %q's connection is unavailable or not permitted; reconnect it before retrying", slug)}
}

// authorizeAgentMemberClient checks the existing catalog and the exact attached
// source before acquiring a legacy agent credential. It does not create a choice
// or grant. Off/shadow keep their connect-equivalent policy; enforced uses the
// same owner-sensitive permit as selected members.
func (s *Service) authorizeAgentMemberClient(ctx context.Context, gate *metaGateContext, member metaMember, issuer uuid.UUID, registration uuid.NullUUID, clientType string) (authClientConnectionOwner, error) {
	return s.authorizeAgentServerClient(ctx, agentServerTarget{organizationID: gate.organizationID, agentID: gate.agentID, serverID: member.serverID, projectID: member.projectID, slug: member.slug}, issuer, registration, clientType)
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
func (s *Service) authorizeAgentServerClient(ctx context.Context, target agentServerTarget, issuer uuid.UUID, registration uuid.NullUUID, clientType string) (authClientConnectionOwner, error) {
	var none authClientConnectionOwner
	rows, err := upstreamclientsrepo.New(s.db).GetAuthClientSelection(ctx, upstreamclientsrepo.GetAuthClientSelectionParams{
		Proposed: true, ProposedAuthClientID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ProposedRegistrationID: registration, CredentialKind: upstreamclients.CredentialKindUserSession,
		CredentialID: uuid.Nil, McpServerIds: []uuid.UUID{target.serverID}, OrganizationID: target.organizationID,
	})
	if err != nil {
		return none, fmt.Errorf("load agent member auth client: %w", err)
	}
	if len(rows) != 1 {
		return none, agentServerCredentialError(target.slug)
	}
	res := decideAuthClient(rows[0])
	mode, err := s.authClientMode(ctx, target.organizationID)
	if err != nil {
		return none, err
	}
	// Before typed enforcement, configured legacy credential headers remain
	// served by the proxy even when no catalog client has been provisioned.
	// This does not waive the enforced catalog/permit barrier.
	if mode != feature.AuthClientResolverEnforced && !registration.Valid && (rows[0].CredentialHeadersUncovered || (rows[0].ServerHasHeaderClients && !rows[0].ServerHasOauthClients && !rows[0].HeadersStale)) {
		return none, nil
	}
	if (res.outcome == authClientGap && (mode == feature.AuthClientResolverEnforced || !registration.Valid)) || (res.outcome == authClientLegacy && res.clientID == uuid.Nil && mode == feature.AuthClientResolverEnforced) {
		return none, agentServerCredentialError(target.slug)
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
		return none, err
	}
	// A personal client is served by the exact attachment, or for an
	// unattached agent key by its validated authorizer's own connection. That
	// fallback needs the mandatory mcp:auth check on a catalog client, which
	// names the authorizer as the owner: before enforcement no such check runs.
	if connection.authorizer && (mode != feature.AuthClientResolverEnforced || res.clientID == uuid.Nil) {
		connection = none
	}
	if permit.ClientType == authClientTypeRemoteSession && connection.attachedSession == uuid.Nil && !connection.authorizer {
		return none, agentServerCredentialError(target.slug)
	}
	permit.ConnectionOwner = connection.owner
	if res.clientID != uuid.Nil {
		permitter, err := s.authClientPermitFor(ctx, target.organizationID)
		if err != nil {
			return none, err
		}
		allowed, err := permitter.Permitted(ctx, permit)
		if err != nil {
			return none, fmt.Errorf("authorize agent member client: %w", err)
		}
		if !allowed {
			return none, agentServerCredentialError(target.slug)
		}
	}
	return connection, nil
}
