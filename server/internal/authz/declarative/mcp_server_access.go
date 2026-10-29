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

// MCPServerPolicyOutcome is one admitted policy's answer, with the
// relationships that produced it.
type MCPServerPolicyOutcome struct {
	Outcome string
	Reason  string
	Path    []string
}

// MCPServerDecision is the answer for one permission on one server, with the
// trace that produced it: every policy the caller acts under must allow.
type MCPServerDecision struct {
	Permission     MCPServerPermission
	Object         string
	OrganizationID string
	Allowed        bool
	Policies       []MCPServerPolicyOutcome
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
	decision, err := decideMCPServer(set, permission, server)
	if err != nil {
		return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: false, DenyReason: "", Trace: "", Err: err}) //nolint:wrapcheck // authz errors carry their response code.
	}
	return engine.RecordDecision(ctx, legacy, authz.ExternalDecision{Allowed: decision.Allowed, DenyReason: decision.denyReason(), Trace: decision.trace(), Err: nil}) //nolint:wrapcheck // authz errors carry their response code.
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
		decision, err := decideMCPServer(set, permission, server)
		if err != nil {
			return false, err
		}
		return decision.Allowed, nil
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

// ExplainMCPServer is for inspection, not enforcement: it returns the
// decision and trace for permission on server through the schema, whether or
// not the schema is active, so the access-explain screen (today
// access/explain_access.go, which still explains the legacy rules) can show
// why someone has or lacks access.
func ExplainMCPServer(ctx context.Context, engine *authz.Engine, permission MCPServerPermission, server MCPServerRef) (MCPServerDecision, error) {
	set, err := engine.PolicySet(ctx)
	if err != nil {
		return MCPServerDecision{}, fmt.Errorf("load policies: %w", err)
	}
	if !set.Enforced {
		return MCPServerDecision{Permission: permission, Object: typeMCPServer + ":" + server.ID, OrganizationID: "", Allowed: true, Policies: nil}, nil
	}
	return decideMCPServer(set, permission, server)
}

// decideMCPServer is the single decision behind RequireMCPServer,
// MCPServerCapabilities and ExplainMCPServer: it evaluates permission on
// server for every policy in set and allows only if all allow. It performs no
// I/O.
func decideMCPServer(set authz.PolicySet, permission MCPServerPermission, server MCPServerRef) (MCPServerDecision, error) {
	decision := MCPServerDecision{
		Permission:     permission,
		Object:         typeMCPServer + ":" + server.ID,
		OrganizationID: set.OrganizationID,
		Allowed:        false,
		Policies:       nil,
	}
	if _, err := permission.legacyScope(); err != nil {
		return decision, err
	}
	if server.ID == "" || server.ID == authz.WildcardResource || server.ProjectID == "" || server.OrganizationID == "" {
		return decision, errMCPServerRefInvalid
	}
	if set.OrganizationID == "" {
		// The policies were loaded or admitted for another organization than
		// the request is in, or the binding was lost. Nothing they say applies.
		decision.Policies = []MCPServerPolicyOutcome{{
			Outcome: relschema.Undetermined.String(),
			Reason:  "policies are not bound to the request's organization",
			Path:    nil,
		}}
		return decision, nil
	}

	object := relschema.Object{
		Type: typeMCPServer,
		ID:   server.ID,
		Ancestors: []relschema.ObjectRef{
			{Type: typeProject, ID: server.ProjectID},
			{Type: typeOrganization, ID: server.OrganizationID},
		},
	}
	decision.Allowed = len(set.Policies) > 0
	for _, policy := range set.Policies {
		relationships, err := decodeMCPServerPolicy(set.OrganizationID, policy)
		if err != nil {
			return decision, err
		}
		result := mcpServerSchema.Evaluate(set.OrganizationID, relationships, object, string(permission), relschema.Request{Facts: nil})
		decision.Policies = append(decision.Policies, MCPServerPolicyOutcome{
			Outcome: result.Outcome.String(),
			Reason:  result.Reason,
			Path:    result.Path,
		})
		if result.Outcome != relschema.Allowed {
			decision.Allowed = false
		}
	}
	return decision, nil
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

func (x MCPServerDecision) trace() string {
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

func (x MCPServerDecision) denyReason() authzrepo.Reason {
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
