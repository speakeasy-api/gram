package platformmcp

import (
	"context"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type changeRiskPolicyAudienceInput struct {
	ProjectSlug      string   `json:"project_slug"`
	PolicyID         string   `json:"policy_id"`
	ExpectedVersion  string   `json:"expected_version"`
	IdempotencyKey   string   `json:"idempotency_key"`
	Confirmed        bool     `json:"confirmed"`
	AddPrincipals    []string `json:"add_principals"`
	RemovePrincipals []string `json:"remove_principals"`
}

func registerChangeRiskPolicyAudience(reg *Registrar, catalogAvailable bool, handlers *RiskMutationHandlers) {
	handler := unavailableRiskMutationTool[UpdateRiskPolicyToolOutput]()
	description := "Change exact positive user/role grants on a targeted risk policy. Risk policy mutations are not enabled in this rollout."
	if catalogAvailable && handlers != nil && handlers.Controls != nil && handlers.ChangeAudience != nil {
		handler = handlers.ChangeAudience
		description = "Atomically add and remove exact organization user/ROLE principal URNs on one targeted risk policy, preserving settings and unrelated grants. Read get_risk_policy first and confirm the delta. Both arrays are required (empty allowed), max 100 each, with at least one change; duplicates, overlap, already-present additions and absent removals are refused. The resulting audience must contain 1–100 principals. Everyone deltas are refused. Removal changes only explicit positive grants, not role-derived or inherited coverage, and never creates an exclusion or proves effective non-membership. For removal of the authenticated user use remove_self_from_risk_policy; do not bypass its refusal. Reuse the idempotency key only for the same confirmed request; re-read after writing."
	}
	principals := func() *jsonschema.Schema {
		return &jsonschema.Schema{Type: "array", Items: stringSchema("Exact organization user or ROLE principal URN; Everyone is not supported.", 1, 0), MaxItems: new(100), UniqueItems: true}
	}
	schema := closedObject(map[string]*jsonschema.Schema{
		"project_slug":     stringSchema("Exact project slug; never defaults.", 1, 0),
		"policy_id":        uuidSchema("Exact policy ID."),
		"expected_version": stringSchema("Opaque version from a fresh get_risk_policy read.", 1, 0),
		"idempotency_key":  stringSchema("Stable replay key for this confirmed delta.", 1, 128),
		"confirmed":        {Type: "boolean", Enum: []any{true}},
		"add_principals":   principals(), "remove_principals": principals(),
	}, []string{"project_slug", "policy_id", "expected_version", "idempotency_key", "confirmed", "add_principals", "remove_principals"})
	schema.AnyOf = []*jsonschema.Schema{
		{Properties: map[string]*jsonschema.Schema{"add_principals": {MinItems: new(1)}}},
		{Properties: map[string]*jsonschema.Schema{"remove_principals": {MinItems: new(1)}}},
	}
	addTool(reg, &mcp.Tool{Meta: nil, OutputSchema: nil, Icons: nil, Name: operationChangeRiskPolicyAudience, Title: "Change Risk Policy Audience", Description: description, InputSchema: schema, Annotations: &mcp.ToolAnnotations{OpenWorldHint: nil, ReadOnlyHint: false, Title: "", DestructiveHint: new(true), IdempotentHint: true}}, ToolMeta{DiscoveryScopes: nil, Authorization: ExternalAuthorizationOrgAdmin, Audiences: []Audience{AudienceExternal}, ProjectScope: ProjectScopeExplicit}, instrumentRiskMutation(reg, operationChangeRiskPolicyAudience, handler))
}

func (s *riskPolicyMutationService) changePolicyAudienceTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
	return s.mutatePolicyTool(ctx, raw, operationChangeRiskPolicyAudience)
}

// Validate both sides before canonicalizing so duplicates and conflicts cannot
// disappear during receipt normalization. Removals need live org validation too.
func validateRiskPolicyAudienceDelta(input changeRiskPolicyAudienceInput) ([]urn.Principal, error) {
	if !input.Confirmed || input.AddPrincipals == nil || input.RemovePrincipals == nil || len(input.AddPrincipals) > 100 || len(input.RemovePrincipals) > 100 || len(input.AddPrincipals)+len(input.RemovePrincipals) == 0 {
		return nil, invalidRiskPolicyRequest()
	}
	seen := map[string]bool{}
	principals := make([]urn.Principal, 0, len(input.AddPrincipals)+len(input.RemovePrincipals))
	for _, values := range [][]string{input.AddPrincipals, input.RemovePrincipals} {
		for _, value := range values {
			principal, err := urn.ParsePrincipal(value)
			if err != nil || principal.String() != value || (principal.Type != urn.PrincipalTypeUser && principal.Type != urn.PrincipalTypeRole) || principal == authz.AllUsersPrincipal() || seen[value] {
				return nil, invalidRiskPolicyRequest()
			}
			seen[value] = true
			principals = append(principals, principal)
		}
	}
	return principals, nil
}

func validateRiskPolicyAudienceDeltaTarget(kind string, audience []string, input changeRiskPolicyAudienceInput) error {
	if kind != "targeted" {
		return selfRemovalRefusal("Audience deltas require a targeted audience; Everyone-except-one is not supported.")
	}
	remaining := make(map[string]bool, len(audience))
	for _, value := range audience {
		principal, err := urn.ParsePrincipal(value)
		if err != nil || principal.String() != value || principal == authz.AllUsersPrincipal() || (principal.Type != urn.PrincipalTypeUser && principal.Type != urn.PrincipalTypeRole) {
			return invalidRiskPolicyRequest()
		}
		remaining[value] = true
	}
	for _, value := range input.RemovePrincipals {
		if !remaining[value] {
			return selfRemovalRefusal("A removal has no exact audience grant. No policy was changed.")
		}
		delete(remaining, value)
	}
	for _, value := range input.AddPrincipals {
		if remaining[value] {
			return selfRemovalRefusal("An addition already has an exact audience grant. No policy was changed.")
		}
		remaining[value] = true
	}
	if len(remaining) == 0 || len(remaining) > 100 {
		return selfRemovalRefusal("The resulting targeted audience must contain between 1 and 100 principals. No policy was changed.")
	}
	return nil
}

func riskPolicyAudienceReplacementSchema() *jsonschema.Schema {
	branch := func(kind string, minimum, maximum int) *jsonschema.Schema {
		return closedObject(map[string]*jsonschema.Schema{
			"type":           constSchema(kind),
			"principal_urns": {Type: "array", Items: stringSchema("Exact organization user or role principal URN. Positive grants only.", 1, 0), MinItems: new(minimum), MaxItems: new(maximum)},
			"confirm":        {Type: "boolean", Enum: []any{true}},
		}, []string{"type", "principal_urns", "confirm"})
	}
	return &jsonschema.Schema{OneOf: []*jsonschema.Schema{branch("everyone", 0, 0), branch("targeted", 1, 100)}}
}
