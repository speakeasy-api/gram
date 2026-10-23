package authz

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RequireBuiltinConnect is an unconditional admission boundary for code-owned
// MCP servers. Human actors, legacy credentials, scope overrides and broad agent
// roles cannot grant access. A live machine admission must explicitly name the
// built-in in the agent policy, and every independent policy must authorize it.
func (e *Engine) RequireBuiltinConnect(ctx context.Context, serverID, projectID string, checks ...Check) error {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || (actor.Type != urn.PrincipalTypeAgent && actor.Type != urn.PrincipalTypeWorkload) {
		return oops.C(oops.CodeForbidden)
	}
	if _, assistant := contextvalues.GetAssistantPrincipal(ctx); assistant {
		return oops.C(oops.CodeForbidden)
	}
	// Do not fall back to GrantsFromContext: demo/support overrides and legacy
	// grant loading are not evidence that a machine credential was admitted.
	authorization, ok := ctx.Value(admittedPoliciesContextKey).(grantAuthorization)
	if !ok || len(authorization.policies) != 3 || serverID == "" || projectID == "" {
		return oops.C(oops.CodeForbidden)
	}
	// Both principal-credential and workload admission preserve the agent's
	// live policy at index one, independently of credential and owner ceilings.
	explicit := false
	for _, grant := range authorization.policies[1] {
		if grant.Scope == ScopeMCPConnect &&
			grant.Selector[SelectorKeyResourceKind] == ResourceKindMCP &&
			grant.Selector[SelectorKeyResourceID] == serverID &&
			grant.Selector[SelectorKeyProjectID] == projectID {
			explicit = true
			break
		}
	}
	if !explicit {
		return oops.C(oops.CodeForbidden)
	}
	return e.evaluateRequired(ctx, authorization, append([]Check{MCPCheck(ScopeMCPConnect, serverID, projectID)}, checks...)...)
}
