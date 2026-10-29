package declarative

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authz/declarative/relschema"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// MCPServerRef is the trusted identity of one MCP server: its RBAC resource id
// and the project and organization it was loaded from. Services build it from
// a tenant-scoped row, never from request input.
type MCPServerRef struct {
	ID             string
	ProjectID      string
	OrganizationID string
}

// Capabilities is what the caller may do with one server. It comes from the
// same evaluation RequireMCPServer enforces.
type Capabilities struct {
	Read    bool
	Edit    bool
	Connect bool
}

// MCPServerPolicyDecision is one admitted policy's answer, with its trace.
type MCPServerPolicyDecision struct {
	Outcome string
	Reason  string
	Path    []string
}

// MCPServerExplanation is the full request-to-decision trace for one
// permission: every policy the caller acts under must allow.
type MCPServerExplanation struct {
	Permission     MCPServerPermission
	Object         string
	OrganizationID string
	Allowed        bool
	Policies       []MCPServerPolicyDecision
}

var errMCPServerRefInvalid = errors.New("mcp server reference is incomplete")

// RequireMCPServer enforces permission on server for the caller. Where the
// schema is not active for the caller's organization it applies the legacy
// scope check unchanged.
func RequireMCPServer(ctx context.Context, engine *authz.Engine, permission MCPServerPermission, server MCPServerRef) error {
	scope, err := permission.legacyScope()
	if err != nil {
		return engine.MapError(ctx, err)
	}
	legacy := authz.MCPCheck(scope, server.ID, server.ProjectID)
	active, err := engine.MCPServerSchemaActive(ctx)
	if err != nil {
		return engine.MapError(ctx, err)
	}
	if !active {
		return engine.Require(ctx, legacy) //nolint:wrapcheck // authz errors carry their response code.
	}

	set, err := engine.PolicySet(ctx)
	if err != nil {
		return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: false, DenyReason: "", Trace: "", Err: err}) //nolint:wrapcheck // authz errors carry their response code.
	}
	if !set.Enforced {
		return nil
	}
	explanation, err := explainMCPServer(set, permission, server)
	if err != nil {
		return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: false, DenyReason: "", Trace: "", Err: err}) //nolint:wrapcheck // authz errors carry their response code.
	}
	return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: explanation.Allowed, DenyReason: explanation.denyReason(), Trace: explanation.trace(), Err: nil}) //nolint:wrapcheck // authz errors carry their response code.
}

// MCPServerCapabilities reports what the caller may do with server, from the
// same evaluation RequireMCPServer enforces. It records no challenge.
func MCPServerCapabilities(ctx context.Context, engine *authz.Engine, server MCPServerRef) (Capabilities, error) {
	active, err := engine.MCPServerSchemaActive(ctx)
	if err != nil {
		return Capabilities{}, engine.MapError(ctx, err)
	}
	var set authz.PolicySet
	if active {
		if set, err = engine.PolicySet(ctx); err != nil {
			return Capabilities{}, engine.MapError(ctx, err)
		}
	}
	allowed := func(permission MCPServerPermission) (bool, error) {
		if !active {
			scope, err := permission.legacyScope()
			if err != nil {
				return false, err
			}
			return engine.Evaluate(ctx, authz.MCPCheck(scope, server.ID, server.ProjectID)) //nolint:wrapcheck // mapped below.
		}
		if !set.Enforced {
			return true, nil
		}
		explanation, err := explainMCPServer(set, permission, server)
		if err != nil {
			return false, err
		}
		return explanation.Allowed, nil
	}
	var caps Capabilities
	for _, p := range []struct {
		permission MCPServerPermission
		into       *bool
	}{{MCPServerRead, &caps.Read}, {MCPServerEdit, &caps.Edit}, {MCPServerConnect, &caps.Connect}} {
		if *p.into, err = allowed(p.permission); err != nil {
			return Capabilities{}, engine.MapError(ctx, err)
		}
	}
	return caps, nil
}

// ExplainMCPServer returns the request-to-decision trace for permission on
// server through the schema, whether or not it is active.
func ExplainMCPServer(ctx context.Context, engine *authz.Engine, permission MCPServerPermission, server MCPServerRef) (MCPServerExplanation, error) {
	set, err := engine.PolicySet(ctx)
	if err != nil {
		return MCPServerExplanation{}, fmt.Errorf("load policies: %w", err)
	}
	if !set.Enforced {
		return MCPServerExplanation{Permission: permission, Object: typeMCPServer + ":" + server.ID, OrganizationID: "", Allowed: true, Policies: nil}, nil
	}
	return explainMCPServer(set, permission, server)
}

// explainMCPServer evaluates permission on server for every policy in set.
// It performs no I/O.
func explainMCPServer(set authz.PolicySet, permission MCPServerPermission, server MCPServerRef) (MCPServerExplanation, error) {
	explanation := MCPServerExplanation{
		Permission:     permission,
		Object:         typeMCPServer + ":" + server.ID,
		OrganizationID: set.OrganizationID,
		Allowed:        false,
		Policies:       nil,
	}
	if _, err := permission.legacyScope(); err != nil {
		return explanation, err
	}
	if server.ID == "" || server.ID == authz.WildcardResource || server.ProjectID == "" || server.OrganizationID == "" {
		return explanation, errMCPServerRefInvalid
	}
	if set.OrganizationID == "" {
		// The policies were loaded or admitted for another organization than
		// the request is in, or the binding was lost. Nothing they say applies.
		explanation.Policies = []MCPServerPolicyDecision{{
			Outcome: relschema.Undetermined.String(),
			Reason:  "policies are not bound to the request's organization",
			Path:    nil,
		}}
		return explanation, nil
	}

	object := relschema.Object{
		Type: typeMCPServer,
		ID:   server.ID,
		Ancestors: []relschema.ObjectRef{
			{Type: typeProject, ID: server.ProjectID},
			{Type: typeOrganization, ID: server.OrganizationID},
		},
	}
	explanation.Allowed = len(set.Policies) > 0
	for _, policy := range set.Policies {
		relationships, err := decodeMCPServerPolicy(set.OrganizationID, policy)
		if err != nil {
			return explanation, err
		}
		decision := mcpServerSchema.Evaluate(set.OrganizationID, relationships, object, string(permission), relschema.Request{Facts: nil})
		explanation.Policies = append(explanation.Policies, MCPServerPolicyDecision{
			Outcome: decision.Outcome.String(),
			Reason:  decision.Reason,
			Path:    decision.Path,
		})
		if decision.Outcome != relschema.Allowed {
			explanation.Allowed = false
		}
	}
	return explanation, nil
}

// decodeMCPServerPolicy maps one policy's stored grants onto the schema. Rows
// outside the schema belong to other checks and are skipped; a row in it that
// the schema cannot express fails the evaluation.
func decodeMCPServerPolicy(organizationID string, grants []authz.Grant) ([]relschema.Relationship, error) {
	relationships := make([]relschema.Relationship, 0, len(grants))
	for i := range grants {
		grant := &grants[i]
		rel, mapped, err := mcpServerSchema.Decode(organizationID, relschema.Row{
			Principal: grant.PrincipalUrn,
			Scope:     string(grant.Scope),
			Selector:  grant.Selector,
		}, relschema.DecodeOptions{Strict: authz.IsAPIKeyGrant(grant)})
		if err != nil {
			return nil, fmt.Errorf("decode grant for mcp server schema: %w", err)
		}
		if mapped {
			relationships = append(relationships, rel)
		}
	}
	return relationships, nil
}

func (x MCPServerExplanation) trace() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s allowed=%t", x.Object, x.Permission, x.Allowed)
	for i, p := range x.Policies {
		fmt.Fprintf(&b, "; policy %d %s (%s)", i, p.Outcome, p.Reason)
		for _, step := range p.Path {
			fmt.Fprintf(&b, " <- %s", step)
		}
	}
	return b.String()
}

func (x MCPServerExplanation) denyReason() authzrepo.Reason {
	if len(x.Policies) == 0 {
		return authzrepo.ReasonNoGrants
	}
	for _, p := range x.Policies {
		if strings.HasPrefix(p.Reason, "withdrawn") {
			return authzrepo.ReasonDenyGrant
		}
	}
	return authzrepo.ReasonScopeUnsatisfied
}
