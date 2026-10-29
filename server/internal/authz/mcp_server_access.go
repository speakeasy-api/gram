package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz/relschema"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// MCPServerSchemaActivation reports whether organizationID evaluates MCP
// server permissions through the relationship schema rather than the legacy
// scope checks. Organizations keep the legacy rules until their migration
// takes effect.
type MCPServerSchemaActivation func(ctx context.Context, organizationID string) (bool, error)

// MCPServerRef is the trusted identity of one MCP server: its RBAC resource id
// and the project and organization it was loaded from. Services build it from
// a tenant-scoped row, never from request input.
type MCPServerRef struct {
	ID             string
	ProjectID      string
	OrganizationID string
}

// MCPServerCapabilities is what the caller may do with one server. It comes
// from the same evaluation RequireMCPServer enforces.
type MCPServerCapabilities struct {
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
	Schema         bool
	Allowed        bool
	Policies       []MCPServerPolicyDecision
}

var errMCPServerRefInvalid = errors.New("mcp server reference is incomplete")

// RequireMCPServer enforces permission on server for the caller. Where the
// relationship schema is not active for the caller's organization it applies
// the legacy scope check unchanged.
func (e *Engine) RequireMCPServer(ctx context.Context, permission MCPServerPermission, server MCPServerRef) error {
	scope, err := permission.legacyScope()
	if err != nil {
		return e.mapError(ctx, err)
	}
	legacy := MCPCheck(scope, server.ID, server.ProjectID)
	active, err := e.mcpServerSchemaActiveFor(ctx)
	if err != nil {
		return e.mapError(ctx, err)
	}
	if !active {
		return e.Require(ctx, legacy)
	}

	explanation, enforced, err := e.explainMCPServer(ctx, permission, server)
	if err != nil {
		challengeLogger{
			Operation:            authzrepo.OperationRequire,
			Outcome:              authzrepo.OutcomeError,
			Reason:               authzrepo.ReasonInvalidCheck,
			Checks:               []Check{legacy},
			Focus:                &legacy,
			Matches:              nil,
			EvaluatedGrantCount:  0,
			FilterCandidateCount: 0,
			FilterAllowedCount:   0,
		}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
		return e.mapError(ctx, err)
	}
	if !enforced {
		return nil
	}
	e.logger.DebugContext(ctx, "mcp server permission decided",
		attr.SlogResourceID(server.ID),
		attr.SlogReason(explanation.trace()),
	)

	outcome, reason := authzrepo.OutcomeAllow, authzrepo.ReasonGrantMatched
	if !explanation.Allowed {
		outcome, reason = authzrepo.OutcomeDeny, explanation.denyReason()
	}
	challengeLogger{
		Operation:            authzrepo.OperationRequire,
		Outcome:              outcome,
		Reason:               reason,
		Checks:               []Check{legacy},
		Focus:                &legacy,
		Matches:              nil,
		EvaluatedGrantCount:  0,
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
	if !explanation.Allowed {
		return e.mapError(ctx, Denied(scope, legacy.selector()))
	}
	return nil
}

// MCPServerCapabilities reports what the caller may do with server, from the
// same evaluation RequireMCPServer enforces. It records no challenge.
func (e *Engine) MCPServerCapabilities(ctx context.Context, server MCPServerRef) (MCPServerCapabilities, error) {
	active, err := e.mcpServerSchemaActiveFor(ctx)
	if err != nil {
		return MCPServerCapabilities{}, e.mapError(ctx, err)
	}
	allowed := func(permission MCPServerPermission) (bool, error) {
		if !active {
			scope, err := permission.legacyScope()
			if err != nil {
				return false, err
			}
			return e.Evaluate(ctx, MCPCheck(scope, server.ID, server.ProjectID))
		}
		explanation, enforced, err := e.explainMCPServer(ctx, permission, server)
		if err != nil {
			return false, e.mapError(ctx, err)
		}
		return !enforced || explanation.Allowed, nil
	}
	var caps MCPServerCapabilities
	if caps.Read, err = allowed(MCPServerRead); err != nil {
		return MCPServerCapabilities{}, err
	}
	if caps.Edit, err = allowed(MCPServerEdit); err != nil {
		return MCPServerCapabilities{}, err
	}
	if caps.Connect, err = allowed(MCPServerConnect); err != nil {
		return MCPServerCapabilities{}, err
	}
	return caps, nil
}

// ExplainMCPServer returns the request-to-decision trace for permission on
// server through the relationship schema, whether or not it is active.
func (e *Engine) ExplainMCPServer(ctx context.Context, permission MCPServerPermission, server MCPServerRef) (MCPServerExplanation, error) {
	explanation, _, err := e.explainMCPServer(ctx, permission, server)
	return explanation, err
}

func (e *Engine) mcpServerSchemaActiveFor(ctx context.Context) (bool, error) {
	if e.mcpServerSchemaActive == nil {
		return false, nil
	}
	// Activation follows the authenticated organization. Whether the loaded
	// policies belong to it is decided by the evaluation, which denies a
	// contradictory binding rather than falling back to the legacy check.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return false, nil
	}
	active, err := e.mcpServerSchemaActive(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return false, fmt.Errorf("check mcp server schema activation: %w", err)
	}
	return active, nil
}

// explainMCPServer evaluates permission on server for every policy the caller
// acts under. enforced is false where RBAC does not apply to the caller, which
// allows exactly as Require does for a non-auth-client check.
func (e *Engine) explainMCPServer(ctx context.Context, permission MCPServerPermission, server MCPServerRef) (MCPServerExplanation, bool, error) {
	explanation := MCPServerExplanation{
		Permission:     permission,
		Object:         relTypeMCPServer + ":" + server.ID,
		OrganizationID: "",
		Schema:         true,
		Allowed:        false,
		Policies:       nil,
	}
	if _, err := permission.legacyScope(); err != nil {
		return explanation, false, err
	}
	if server.ID == "" || server.ID == WildcardResource || server.ProjectID == "" || server.OrganizationID == "" {
		return explanation, false, errMCPServerRefInvalid
	}
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return explanation, false, err
	}
	if !enforce {
		explanation.Allowed = true
		return explanation, false, nil
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return explanation, true, ErrMissingGrants
	}
	explanation.OrganizationID = authorization.organizationID
	if authorization.organizationID == "" {
		// The policies were loaded or admitted for another organization than
		// the request is in, or the binding was lost. Nothing they say applies.
		explanation.Policies = []MCPServerPolicyDecision{{
			Outcome: relschema.Undetermined.String(),
			Reason:  "policies are not bound to the request's organization",
			Path:    nil,
		}}
		return explanation, true, nil
	}

	object := relschema.Object{
		Type: relTypeMCPServer,
		ID:   server.ID,
		Ancestors: []relschema.ObjectRef{
			{Type: relTypeProject, ID: server.ProjectID},
			{Type: relTypeOrganization, ID: server.OrganizationID},
		},
	}
	explanation.Allowed = len(authorization.policies) > 0
	for _, policy := range authorization.policies {
		relationships, err := decodeMCPServerPolicy(authorization.organizationID, policy)
		if err != nil {
			return explanation, true, err
		}
		decision := mcpServerSchema.Evaluate(authorization.organizationID, relationships, object, string(permission), relschema.Request{Facts: nil})
		explanation.Policies = append(explanation.Policies, MCPServerPolicyDecision{
			Outcome: decision.Outcome.String(),
			Reason:  decision.Reason,
			Path:    decision.Path,
		})
		if decision.Outcome != relschema.Allowed {
			explanation.Allowed = false
		}
	}
	return explanation, true, nil
}

// decodeMCPServerPolicy maps one policy's stored grants onto the schema. Rows
// outside the schema belong to other checks and are skipped; a row in it that
// the schema cannot express fails the evaluation.
func decodeMCPServerPolicy(organizationID string, grants []Grant) ([]relschema.Relationship, error) {
	relationships := make([]relschema.Relationship, 0, len(grants))
	for i := range grants {
		grant := &grants[i]
		rel, mapped, err := mcpServerSchema.Decode(organizationID, relschema.Row{
			Principal: grant.PrincipalUrn,
			Scope:     string(grant.Scope),
			Selector:  grant.Selector,
		}, relschema.DecodeOptions{Strict: isAPIKeyGrant(grant)})
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
