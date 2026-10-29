// Agent and workload credentials at a stored gateway. A gateway is a member
// set, not one MCP server, so a credential carries no single-server target
// into it. The gate admits the credential, and every request then admits each
// live member against the credential's ceiling, the agent's live policy, and
// its owner's live policy, the way it admits each member for a person.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
)

// agentGatewayAddressable reports whether an agent or workload credential may
// address endpoint as a stored gateway. Organization and project gateways
// derive their members from a signed-in person's access, so they are not.
func agentGatewayAddressable(endpoint *ResolvedMcpEndpoint) bool {
	return endpoint.MetaMcpServerID.Valid && endpoint.MetaMcpServerID.UUID != uuid.Nil &&
		!endpoint.IsOrganizationGateway() &&
		endpoint.ProjectID != uuid.Nil && endpoint.UserSessionIssuerID != uuid.Nil && endpoint.OrganizationID != ""
}

// encodeGatewayWorkloadSessionPolicy is the ceiling of a workload session
// minted for a stored gateway: mcp:connect on every MCP server in the
// gateway's project, the ceiling an issuer-wide session over that project
// carries. A stored gateway's members all live in its project.
//
// It bounds nothing finer, for the reason an issuer-wide ceiling does not.
// The session's audience confines it to the gateway, the gateway's live
// membership decides which servers a request can name, and authorization
// intersects this ceiling with the assigned agent's and owner's live policy
// at each of them.
func encodeGatewayWorkloadSessionPolicy(gatewayProjectID uuid.UUID, version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
	if gatewayProjectID == uuid.Nil {
		return nil, errors.New("gateway workload session policy requires a project")
	}
	return encodeIssuerWorkloadSessionPolicy(gatewayProjectID, version)
}

// endpointWorkloadSessionPolicy is the ceiling of a workload session minted
// for endpoint's own resource: the one MCP server's agent session policy, or a
// stored gateway's. ok is false for an endpoint that can carry neither.
func endpointWorkloadSessionPolicy(endpoint *ResolvedMcpEndpoint, version runtimepolicy.DelegatedPolicyVersion) ([]byte, bool, error) {
	if agentGatewayAddressable(endpoint) {
		policy, err := encodeGatewayWorkloadSessionPolicy(endpoint.ProjectID, version)
		return policy, true, err
	}
	target, ok := agentAuthorizationTarget(endpoint)
	if !ok {
		return nil, false, nil
	}
	policy, err := encodeAgentSessionPolicy(*target, version)
	return policy, true, err
}

// workloadSessionAddressable reports whether a workload session can be minted
// for endpoint's own resource.
func workloadSessionAddressable(endpoint *ResolvedMcpEndpoint) bool {
	if agentGatewayAddressable(endpoint) {
		return true
	}
	_, ok := agentAuthorizationTarget(endpoint)
	return ok
}

// requireAgentCredentialAuthorization checks an agent key or principal
// credential may enter endpoint. A stored gateway admits it without a
// single-server check; each member is checked on every request instead.
func (s *Service) requireAgentCredentialAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint) (context.Context, error) {
	if agentGatewayAddressable(endpoint) {
		return ctx, nil
	}
	return s.requireAgentSessionAuthorization(ctx, endpoint)
}

// metaMemberConnectCheck is the mcp:connect check a gateway member is admitted
// by. Grants on toolset-backed servers key on the toolset id, and grants on
// proxy backends on the mcp_servers id; checking the wrong one silently hides
// the member from every resource-scoped grant the dashboard writes.
func metaMemberConnectCheck(serverID uuid.UUID, toolsetID uuid.NullUUID, projectID uuid.UUID) authz.Check {
	resourceID := serverID
	if toolsetID.Valid {
		resourceID = toolsetID.UUID
	}
	return authz.MCPCheck(authz.ScopeMCPConnect, resourceID.String(), projectID.String())
}

// gatewayWorkloadReach is what a workload grant's preflight found the
// workload reaches at a stored gateway.
type gatewayWorkloadReach struct {
	// public reports a public member, which a gateway serves to every caller
	// its gate admits.
	public bool

	// private is the connect check of each private member the preflight
	// admitted.
	private []authz.Check
}

// resolveGatewayWorkloadReach admits a stored gateway's live members for an
// admitted workload, as its requests will, and refuses a workload that
// reaches none of them: a session for an empty gateway serves nothing.
func (s *Service) resolveGatewayWorkloadReach(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) (context.Context, gatewayWorkloadReach, error) {
	reach := gatewayWorkloadReach{public: false, private: nil}
	ctx, members, err := s.resolveEndpointMemberSnapshot(withMandatoryAdmission(ctx), logger, mandatorySurfaceTokenIssuance, endpoint)
	if err != nil {
		return ctx, reach, fmt.Errorf("resolve gateway members for workload: %w", err)
	}
	for _, member := range members {
		if member.visibility == mcpservers.VisibilityPublic {
			reach.public = true
			continue
		}
		reach.private = append(reach.private, metaMemberConnectCheck(member.serverID, member.toolsetID, member.projectID))
	}
	if !reach.public && len(reach.private) == 0 {
		return ctx, reach, fmt.Errorf("%w: workload reaches no member of this gateway", errCredentialRejected)
	}
	return ctx, reach, nil
}

// recheck reports whether freshly loaded authority still reaches a member the
// preflight admitted.
func (r gatewayWorkloadReach) recheck(ctx context.Context, engine *authz.Engine) (bool, error) {
	if r.public {
		return true, nil
	}
	for _, check := range r.private {
		allowed, err := engine.Evaluate(ctx, check)
		if err != nil {
			return false, fmt.Errorf("recheck gateway member authority: %w", err)
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}
