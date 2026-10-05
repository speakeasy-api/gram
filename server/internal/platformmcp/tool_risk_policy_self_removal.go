package platformmcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerRemoveSelfFromRiskPolicy(reg *Registrar, handlers *RiskMutationHandlers) {
	description := "Self-removal and effective exclusion are unavailable pending organization-scoped coordination of audience grants. This tool always refuses without changing policies or replaying receipts. Do not bypass this refusal with an audience delta or replacement, policy disablement, role or membership changes, or a risk exclusion."
	schema := closedObject(map[string]*jsonschema.Schema{
		"project_slug":     stringSchema("Exact project slug; never defaults.", 1, 0),
		"policy_id":        uuidSchema("Exact policy ID."),
		"expected_version": stringSchema("Opaque version from a fresh get_risk_policy read.", 1, 0),
		"idempotency_key":  stringSchema("Stable replay key for this confirmed self removal.", 1, 128),
		"confirmed":        {Type: "boolean", Enum: []any{true}},
	}, []string{"project_slug", "policy_id", "expected_version", "idempotency_key", "confirmed"})
	addTool(reg, &mcp.Tool{Meta: nil, OutputSchema: nil, Icons: nil, Name: operationRemoveSelfFromRiskPolicy, Title: "Remove Self From Risk Policy", Description: description, InputSchema: schema, Annotations: &mcp.ToolAnnotations{OpenWorldHint: nil, ReadOnlyHint: false, Title: "", DestructiveHint: new(true), IdempotentHint: true}}, ToolMeta{DiscoveryScopes: nil, Authorization: ExternalAuthorizationOrgAdmin, Audiences: []Audience{AudienceExternal}, ProjectScope: ProjectScopeExplicit}, instrumentRiskMutation(reg, operationRemoveSelfFromRiskPolicy, handlers.RemoveSelf))
}

func (s *riskPolicyMutationService) removeSelfFromPolicyTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
	return s.mutatePolicyTool(ctx, raw, operationRemoveSelfFromRiskPolicy)
}

func selfRemovalRefusal(message string) error {
	return &RiskMutationError{Code: "invalid_request", Message: message, Cause: ErrRiskMutationInvalid}
}

// Effective exclusion needs organization-scoped coordination across all grant writers.
// Do not replace this refusal with a table-wide lock or a positive-grant deletion.
func selfRemovalUnavailable() error {
	return &RiskMutationError{Code: unavailableCode, Message: "Self-removal and effective exclusion are unavailable pending organization-scoped coordination of audience grants. No policy was changed. Do not bypass this refusal with an audience delta or replacement, policy disablement, role or membership changes, or a risk exclusion.", Cause: ErrRiskMutationUnavailable}
}
