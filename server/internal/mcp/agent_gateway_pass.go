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

// gatewayWorkloadReach is what a workload grant's preflight found the
// workload reaches at a stored gateway.
type gatewayWorkloadReach struct {
	// checks is the connect check of each member the preflight admitted,
	// public or private: a workload is held to mcp:connect on both.
	checks []authz.Check
}

// resolveGatewayWorkloadReach admits a stored gateway's live members for an
// admitted workload, as its requests will, and refuses a workload that
// reaches none of them: a session for an empty gateway serves nothing.
func (s *Service) resolveGatewayWorkloadReach(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint) (context.Context, gatewayWorkloadReach, error) {
	reach := gatewayWorkloadReach{checks: nil}
	ctx, members, err := s.resolveEndpointMemberSnapshot(withMandatoryAdmission(ctx), logger, mandatorySurfaceTokenIssuance, endpoint)
	if err != nil {
		return ctx, reach, fmt.Errorf("resolve gateway members for workload: %w", err)
	}
	for _, member := range members {
		reach.checks = append(reach.checks, metaMemberConnectCheck(member.serverID, member.toolsetID, member.projectID))
	}
	if len(reach.checks) == 0 {
		return ctx, reach, fmt.Errorf("%w: workload reaches no member of this gateway", errCredentialRejected)
	}
	return ctx, reach, nil
}

// recheck reports whether freshly loaded authority still reaches a member the
// preflight admitted.
func (r gatewayWorkloadReach) recheck(ctx context.Context, engine *authz.Engine) (bool, error) {
	for _, check := range r.checks {
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
