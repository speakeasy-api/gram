// Agent and workload credentials at a stored gateway. The gate admits the
// credential without a single-server target, and every request admits each
// member against the credential's ceiling and the agent's and owner's live
// policies, public members included.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
// gateway's project, where all its members live, and on the gateway itself.
// The gateway grant adds no authority; it binds the session to this gateway,
// so the gate's byte-for-byte check refuses it at one recreated later at the
// same URL, issuer and project.
func encodeGatewayWorkloadSessionPolicy(gatewayID, gatewayProjectID uuid.UUID, version runtimepolicy.DelegatedPolicyVersion) ([]byte, error) {
	if gatewayID == uuid.Nil || gatewayProjectID == uuid.Nil {
		return nil, errors.New("gateway workload session policy requires a gateway and its project")
	}
	connect := func(resourceID string) authz.Grant {
		return authz.Grant{
			PrincipalUrn: "",
			Scope:        authz.ScopeMCPConnect,
			Selector: authz.Selector{
				authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
				authz.SelectorKeyResourceID:   resourceID,
				authz.SelectorKeyProjectID:    gatewayProjectID.String(),
			},
			Inferred: false,
		}
	}
	policy, err := runtimepolicy.NewDelegatedPolicy(version, []authz.Grant{connect(authz.WildcardResource), connect(gatewayID.String())})
	if err != nil {
		return nil, fmt.Errorf("construct gateway workload session policy: %w", err)
	}
	encoded, err := runtimepolicy.EncodeDelegatedPolicy(version, policy)
	if err != nil {
		return nil, fmt.Errorf("encode gateway workload session policy: %w", err)
	}
	return encoded, nil
}

// endpointWorkloadSessionPolicy is the ceiling of a workload session minted
// for endpoint's own resource: the one MCP server's agent session policy, or a
// stored gateway's. ok is false for an endpoint that can carry neither.
func endpointWorkloadSessionPolicy(endpoint *ResolvedMcpEndpoint, version runtimepolicy.DelegatedPolicyVersion) ([]byte, bool, error) {
	if agentGatewayAddressable(endpoint) {
		policy, err := encodeGatewayWorkloadSessionPolicy(endpoint.MetaMcpServerID.UUID, endpoint.ProjectID, version)
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
// credential may enter endpoint. A stored gateway checks each member per
// request instead.
func (s *Service) requireAgentCredentialAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint) (context.Context, error) {
	if agentGatewayAddressable(endpoint) {
		return ctx, nil
	}
	return s.requireAgentSessionAuthorization(ctx, endpoint)
}

// delegatedCaller reports whether the request acts as an agent or a workload.
// Public visibility opens nothing past such a caller's ceiling and agent and
// owner policies, so it needs mcp:connect on public servers too.
func delegatedCaller(ctx context.Context) bool {
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return true
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && (actor.Type == urn.PrincipalTypeAgent || actor.Type == urn.PrincipalTypeWorkload)
}

// metaMemberConnectCheck is a gateway member's mcp:connect check. Grants on
// toolset-backed servers key on the toolset id and on proxy backends on the
// mcp_servers id; checking the wrong one hides the member from every grant.
func metaMemberConnectCheck(serverID uuid.UUID, toolsetID uuid.NullUUID, projectID uuid.UUID) authz.Check {
	resourceID := serverID
	if toolsetID.Valid {
		resourceID = toolsetID.UUID
	}
	return authz.MCPCheck(authz.ScopeMCPConnect, resourceID.String(), projectID.String())
}

// errGatewayMemberUnreachable refuses a gateway workload grant whose workload
// reaches no member. It answers invalid_grant like an untrusted identity, to
// reveal nothing, but is logged under its own reason.
var errGatewayMemberUnreachable = errors.New("workload reaches no member of this gateway")

// gatewayWorkloadReach is the members a workload grant's preflight admitted.
type gatewayWorkloadReach struct {
	// members holds each admitted member's mcp_servers id.
	members map[uuid.UUID]struct{}
}

// resolveGatewayWorkloadReach admits a gateway's members for a workload as
// its requests will, and refuses one that reaches none.
func (s *Service) resolveGatewayWorkloadReach(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) (context.Context, gatewayWorkloadReach, error) {
	reach := gatewayWorkloadReach{members: map[uuid.UUID]struct{}{}}
	ctx, members, err := s.resolveEndpointMemberSnapshot(withMandatoryAdmission(ctx), logger, mandatorySurfaceTokenIssuance, endpoint)
	if err != nil {
		return ctx, reach, fmt.Errorf("resolve gateway members for workload: %w", err)
	}
	for _, member := range members {
		reach.members[member.serverID] = struct{}{}
	}
	if len(reach.members) == 0 {
		return ctx, reach, fmt.Errorf("%w: %w", errCredentialRejected, errGatewayMemberUnreachable)
	}
	return ctx, reach, nil
}

// freshMemberChecks re-reads membership in the issuance transaction and
// returns the connect check of each preflight member that still serves, so a
// member removed, disabled, or whose toolset stopped serving is not counted.
func (r gatewayWorkloadReach) freshMemberChecks(ctx context.Context, db toolsets_repo.DBTX, endpoint *ResolvedMcpEndpoint) ([]authz.Check, error) {
	rows, err := metamcprepo.New(db).ListServableMetaMCPMembers(ctx, metamcprepo.ListServableMetaMCPMembersParams{
		MetaMcpServerID: endpoint.MetaMcpServerID.UUID,
		ProjectID:       endpoint.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("re-read gateway members for issuance: %w", err)
	}
	checks := make([]authz.Check, 0, len(rows))
	for _, row := range rows {
		if _, reached := r.members[row.McpServerID]; !reached || row.McpServerUnproxiedMcpServerID.Valid {
			continue
		}
		if row.McpServerToolsetID.Valid {
			_, _, servable, err := loadServableMemberToolset(ctx, db, row.McpServerID, row.McpServerToolsetID.UUID, endpoint.ProjectID)
			if err != nil {
				return nil, fmt.Errorf("re-read hosted gateway member for issuance: %w", err)
			}
			if !servable {
				continue
			}
		}
		checks = append(checks, metaMemberConnectCheck(row.McpServerID, row.McpServerToolsetID, endpoint.ProjectID))
	}
	return checks, nil
}

// gatewayMemberReached reports whether any of checks is allowed.
func gatewayMemberReached(ctx context.Context, engine *authz.Engine, checks []authz.Check) (bool, error) {
	for _, check := range checks {
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
