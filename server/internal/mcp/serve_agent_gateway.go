// The agent gateway MCP surface: one URL per agent, whose members are derived
// from the agent's own grants rather than from stored membership rows. It
// reuses the meta-server runtime wholesale — same four-tool contract, same
// member dispatch — so the only thing particular to it is how the request is
// authenticated and how membership is resolved.

package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

	// The same lockdown ServeMCPEndpoint applies, at the only scope this
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
		// NULL serves Gram's built-in gateway instructions.
		Instructions:      pgtype.Text{String: "", Valid: false},
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

	if err := s.serveResolvedMetaMCPEndpoint(w, r, logger, endpoint, metaServer, agentID); err != nil {
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
	return keyCtx, authCtx, nil
}
