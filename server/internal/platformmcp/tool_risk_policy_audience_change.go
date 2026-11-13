package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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

// ChangeRiskPolicyAudiencePreview is what an unconfirmed audience change
// reports: the policy, its current explicit audience, the delta, and the
// audience a confirmed call with the same input would leave. Nothing is
// written for it.
type ChangeRiskPolicyAudiencePreview struct {
	Project          RiskMutationReceiptProject `json:"project"`
	Policy           RiskPolicyReceiptSummary   `json:"policy"`
	PolicyName       string                     `json:"policy_name"`
	ExpectedVersion  string                     `json:"expected_version"`
	AudienceBefore   []string                   `json:"audience_before"`
	AddPrincipals    []string                   `json:"add_principals"`
	RemovePrincipals []string                   `json:"remove_principals"`
	AudienceAfter    []string                   `json:"audience_after"`
}

const riskPolicyAudiencePreviewMessage = "Nothing was changed. Show the user the policy, its current audience, who is added and removed, and the resulting audience in this preview, then call again with the same input, the same idempotency key, and confirmed: true once they confirm."

type riskPolicyAudiencePreviewResult struct {
	featureUnavailableResult
	Preview ChangeRiskPolicyAudiencePreview `json:"preview"`
}

func registerChangeRiskPolicyAudience(reg *Registrar, catalogAvailable bool, handlers *RiskMutationHandlers) {
	handler := unavailableRiskMutationTool[UpdateRiskPolicyToolOutput]()
	description := "Change exact positive user/role grants on a targeted risk policy. Risk policy mutations are not enabled in this rollout."
	if catalogAvailable && handlers != nil && handlers.Controls != nil && handlers.ChangeAudience != nil {
		handler = handlers.ChangeAudience
		description = "Atomically add and remove exact organization user/ROLE principal URNs on one targeted risk policy, preserving settings and unrelated grants. Read get_risk_policy first. Call first with confirmed: false: the same checks as a confirmed call run but nothing is written, and the confirmation_required refusal returns a preview of the policy, its current audience, the principals added and removed, and the resulting audience; any other refusal means the change cannot be made as requested. Show it to the user, then call again with the same input, the same idempotency key, and confirmed: true once they confirm. Both arrays are required (empty allowed), max 100 each, with at least one change; duplicates, overlap, already-present additions and absent removals are refused. The resulting audience must contain 1–100 principals. Everyone deltas are refused. Removal changes only explicit positive grants, not role-derived or inherited coverage, and never creates an exclusion or proves effective non-membership. For removal of the authenticated user use remove_self_from_risk_policy; do not bypass its refusal. Reuse the idempotency key only for the same confirmed request; re-read after writing."
	}
	principals := func(description string) *jsonschema.Schema {
		return &jsonschema.Schema{Type: "array", Description: description, Items: stringSchema("Exact organization user or ROLE principal URN; Everyone is not supported.", 1, 0), MaxItems: new(100), UniqueItems: true}
	}
	schema := closedObject(map[string]*jsonschema.Schema{
		"project_slug":     stringSchema(projectSlugDescription, 1, 0),
		"policy_id":        uuidSchema("Exact policy ID."),
		"expected_version": stringSchema("Opaque version from a fresh get_risk_policy read.", 1, 0),
		"idempotency_key":  stringSchema("Stable replay key for this confirmed delta.", 1, 128),
		"confirmed":        {Type: "boolean", Description: "false to preview the delta without writing; true only after the user confirms the exact change the preview shows."},
		"add_principals":   principals("Principals to grant; may be empty when remove_principals is not. At least one of the two arrays must name a principal."), "remove_principals": principals("Principals whose exact grant to remove; may be empty when add_principals is not."),
	}, []string{"project_slug", "policy_id", "expected_version", "idempotency_key", "confirmed", "add_principals", "remove_principals"})
	addTool(reg, &mcp.Tool{Meta: nil, OutputSchema: nil, Icons: nil, Name: operationChangeRiskPolicyAudience, Title: "Change Risk Policy Audience", Description: description, InputSchema: schema, Annotations: &mcp.ToolAnnotations{OpenWorldHint: new(false), ReadOnlyHint: false, Title: "", DestructiveHint: new(true), IdempotentHint: true}}, ToolMeta{DiscoveryScopes: nil, Authorization: ExternalAuthorizationOrgAdmin, Audiences: []Audience{AudienceExternal}, ProjectScope: ProjectScopeExplicit}, instrumentRiskMutation(reg, operationChangeRiskPolicyAudience, handler))
}

func (s *riskPolicyMutationService) changePolicyAudienceTool(ctx context.Context, _ *mcp.CallToolRequest, raw map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
	return s.mutatePolicyTool(ctx, raw, operationChangeRiskPolicyAudience)
}

// previewAudienceChange runs the confirmed write in a transaction it rolls
// back, so the preview and the confirmed call refuse the same changes. Nothing
// is charged, stored, or published; build reads what mutate captured.
func (s *riskPolicyMutationService) previewAudienceChange(ctx context.Context, idempotencyKey string, mutate func(context.Context, pgx.Tx) (RiskMutationReceiptResult, error), build func() ChangeRiskPolicyAudiencePreview) (ChangeRiskPolicyAudiencePreview, error) {
	if key := strings.TrimSpace(idempotencyKey); key == "" || len(idempotencyKey) > 128 {
		return ChangeRiskPolicyAudiencePreview{}, invalidRiskPolicyRequest()
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return ChangeRiskPolicyAudiencePreview{}, riskMutationUnavailableWithCause(fmt.Errorf("begin risk policy audience preview: %w", err))
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	if _, err := mutate(ctx, tx); err != nil {
		return ChangeRiskPolicyAudiencePreview{}, err
	}
	return build(), nil
}

// riskPolicyAudiencePreviewRefusal is the confirmation_required refusal of an
// unconfirmed audience change, carrying its preview.
func riskPolicyAudiencePreviewRefusal(preview ChangeRiskPolicyAudiencePreview) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
	payload, err := json.Marshal(riskPolicyAudiencePreviewResult{
		featureUnavailableResult: featureUnavailableResult{Code: "confirmation_required", Feature: "risk_mutations", Message: riskPolicyAudiencePreviewMessage},
		Preview:                  preview,
	})
	if err != nil {
		return nil, UpdateRiskPolicyToolOutput{}, fmt.Errorf("encode risk policy audience preview: %w", err)
	}
	return nil, UpdateRiskPolicyToolOutput{}, &ToolRefusalError{Code: "confirmation_required", Payload: string(payload)}
}

// Validate both sides before canonicalizing so duplicates and conflicts cannot
// disappear during receipt normalization. Removals need live org validation too.
func validateRiskPolicyAudienceDelta(input changeRiskPolicyAudienceInput) ([]urn.Principal, error) {
	if input.AddPrincipals == nil || input.RemovePrincipals == nil || len(input.AddPrincipals) > 100 || len(input.RemovePrincipals) > 100 {
		return nil, invalidRiskPolicyRequest()
	}
	// The input schema cannot require one change, because Anthropic rejects
	// a top-level anyOf, so an empty delta is refused here.
	if len(input.AddPrincipals)+len(input.RemovePrincipals) == 0 {
		return nil, &RiskMutationError{Code: "invalid_request", Message: "Add or remove at least one principal. No policy was changed.", Cause: ErrRiskMutationInvalid}
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
