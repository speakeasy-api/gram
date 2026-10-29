// Agent and workload credentials at a stored gateway. A gateway is a member
// set, not one MCP server, so a credential carries no single-server target
// into it. The gate admits the credential, and every request then admits each
// live member against the credential's ceiling, the agent's live policy, and
// its owner's live policy. Unlike a person, such a caller needs mcp:connect on
// a public member too, as the issuer gate requires of it at a public single
// server.
//
// A member that signs in upstream is called with the credential the caller
// would use at that member's own server: an agent key's attachment, a
// principal credential's authorizer's connection, or a workload's agent
// owner's own connection. A member the agent is not granted is never called.

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
// gateway's project, the authority an issuer-wide session over that project
// carries, and mcp:connect on the gateway itself. A stored gateway's members
// all live in its project.
//
// The gateway's own grant adds no authority, since the project wildcard
// already covers it. It names the gateway the session was minted for, as a
// single-server ceiling names its server, so the byte-for-byte check at the
// gate refuses the session at a gateway created later at the same URL, issuer
// and project.
//
// It bounds nothing finer, for the reason an issuer-wide ceiling does not.
// The session's audience confines it to the gateway, the gateway's live
// membership decides which servers a request can name, and authorization
// intersects this ceiling with the assigned agent's and owner's live policy
// at each of them.
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
// credential may enter endpoint. A stored gateway admits it without a
// single-server check; each member is checked on every request instead.
func (s *Service) requireAgentCredentialAuthorization(ctx context.Context, endpoint *ResolvedMcpEndpoint) (context.Context, error) {
	if agentGatewayAddressable(endpoint) {
		return ctx, nil
	}
	return s.requireAgentSessionAuthorization(ctx, endpoint)
}

// delegatedCaller reports whether the request acts as an agent or a workload:
// an agent key, a principal credential, or an agent or workload session. Its
// authority is a delegated ceiling intersected with the agent's and its
// owner's live policies, and a server's public visibility opens nothing past
// that: the issuer gate requires such a caller's mcp:connect at a public
// single server, and a stored gateway requires it on each public member.
func delegatedCaller(ctx context.Context) bool {
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return true
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && (actor.Type == urn.PrincipalTypeAgent || actor.Type == urn.PrincipalTypeWorkload)
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

// errGatewayMemberUnreachable marks a workload grant for a stored gateway
// refused because the workload reaches none of the gateway's members. The
// token endpoint answers it with the same invalid_grant as an untrusted
// identity, so the answer reveals no gateway, member, or grant, but the
// refusal is logged under its own reason.
var errGatewayMemberUnreachable = errors.New("workload reaches no member of this gateway")

// gatewayWorkloadReach is the members of a stored gateway a workload grant's
// preflight found the workload reaches.
type gatewayWorkloadReach struct {
	// members is the mcp_servers id of each member the preflight admitted and
	// could serve: public or private, since a workload is held to mcp:connect
	// on both, and for a hosted member only while its toolset serves.
	members map[uuid.UUID]struct{}
}

// resolveGatewayWorkloadReach admits a stored gateway's live members for an
// admitted workload, as its requests will, and refuses a workload that
// reaches none of them: a session for an empty gateway serves nothing.
//
// A hosted member counts only when its toolset loads as it would on
// drill-down, so a member that lists but reads as nonexistent there does not
// make the session issuable. The workload acts in the gateway's own
// organization.
func (s *Service) resolveGatewayWorkloadReach(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) (context.Context, gatewayWorkloadReach, error) {
	reach := gatewayWorkloadReach{members: map[uuid.UUID]struct{}{}}
	ctx, members, err := s.resolveEndpointMemberSnapshot(withMandatoryAdmission(ctx), logger, mandatorySurfaceTokenIssuance, endpoint)
	if err != nil {
		return ctx, reach, fmt.Errorf("resolve gateway members for workload: %w", err)
	}
	access := memberToolsetAccess{authenticated: true, connectPublic: true}
	for _, member := range members {
		if member.backend == metaMemberBackendHosted {
			_, _, _, err := s.loadMemberToolset(ctx, logger, access, member)
			if memberReadsAsNonexistent(err) {
				continue
			}
			if err != nil {
				return ctx, reach, fmt.Errorf("load hosted gateway member for workload: %w", err)
			}
		}
		reach.members[member.serverID] = struct{}{}
	}
	if len(reach.members) == 0 {
		return ctx, reach, fmt.Errorf("%w: %w", errCredentialRejected, errGatewayMemberUnreachable)
	}
	return ctx, reach, nil
}

// freshMemberChecks re-reads the gateway's servable members through db, the
// issuance transaction, and returns the connect check of each member the
// preflight reached that still is one, so issuance never counts a member
// removed, disabled, or rewired since the preflight read it.
func (r gatewayWorkloadReach) freshMemberChecks(ctx context.Context, db metamcprepo.DBTX, endpoint *ResolvedMcpEndpoint) ([]authz.Check, error) {
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
		checks = append(checks, metaMemberConnectCheck(row.McpServerID, row.McpServerToolsetID, endpoint.ProjectID))
	}
	return checks, nil
}

// gatewayMemberReached reports whether freshly loaded authority allows any of
// checks, the connect check of each member issuance still counts.
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
