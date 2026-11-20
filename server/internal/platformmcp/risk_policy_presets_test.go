package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/presets"
)

func TestApplyRiskPolicyPresetExpandsStandardPreset(t *testing.T) {
	t.Parallel()

	input, err := applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: "customer_pii_egress", IdempotencyKey: "key"})
	require.NoError(t, err)
	require.Empty(t, input.Preset)
	require.Equal(t, "standard", input.PolicyType)
	require.Equal(t, "Customer personal data", input.Name)
	require.Equal(t, "flag", input.Action)
	require.NotNil(t, input.Score)
	require.InDelta(t, 6, *input.Score, 0)
	require.Equal(t, []string{"presidio"}, input.Sources)
	require.Contains(t, input.PresidioEntities, "US_SSN")
	require.Nil(t, input.Enabled)
	require.Nil(t, input.UserMessage)
	require.Empty(t, input.Prompt)
}

func TestApplyRiskPolicyPresetKeepsCallerOverrides(t *testing.T) {
	t.Parallel()

	score := 3.0
	enabled := false
	message := "custom"
	input, err := applyRiskPolicyPreset(createRiskPolicyInput{
		ProjectSlug: "project", Preset: "secrets_and_credentials", IdempotencyKey: "key",
		Name: "Team secrets", Action: "warn", Score: &score, Enabled: &enabled, UserMessage: &message,
	})
	require.NoError(t, err)
	require.Equal(t, "Team secrets", input.Name)
	require.Equal(t, "warn", input.Action)
	require.InDelta(t, 3, *input.Score, 0)
	require.False(t, *input.Enabled)
	require.Equal(t, "custom", *input.UserMessage)
	require.Equal(t, []string{"gitleaks"}, input.Sources)
	require.Empty(t, input.PresidioEntities)
}

func TestApplyRiskPolicyPresetExpandsPromptPreset(t *testing.T) {
	t.Parallel()

	preset, ok := presets.ByID("destructive_production_actions")
	require.True(t, ok)

	input, err := applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: preset.ID, IdempotencyKey: "key"})
	require.NoError(t, err)
	require.Equal(t, "prompt_based", input.PolicyType)
	require.Equal(t, preset.Prompt, input.Prompt)
	require.Equal(t, "block", input.Action)
	require.Empty(t, input.Sources)
	require.NotNil(t, input.UserMessage)

	input, err = applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: preset.ID, Prompt: "Only the billing database.", IdempotencyKey: "key"})
	require.NoError(t, err)
	require.Equal(t, "Only the billing database.", input.Prompt)
}

func TestApplyRiskPolicyPresetRefusesUnknownOrConflictingInput(t *testing.T) {
	t.Parallel()

	_, err := applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: "missing", IdempotencyKey: "key"})
	require.ErrorIs(t, err, ErrRiskMutationInvalid)

	_, err = applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: "secrets_and_credentials", PolicyType: "standard", IdempotencyKey: "key"})
	require.ErrorIs(t, err, ErrRiskMutationInvalid)

	_, err = applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: "secrets_and_credentials", Prompt: "ignored by a standard policy", IdempotencyKey: "key"})
	require.ErrorIs(t, err, ErrRiskMutationInvalid, "a prompt override on a standard preset would be dropped silently")

	_, err = applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", Preset: "destructive_production_actions", PresidioEntities: []string{"US_SSN"}, IdempotencyKey: "key"})
	require.ErrorIs(t, err, ErrRiskMutationInvalid, "entity overrides do not apply to a prompt-based preset")

	input, err := applyRiskPolicyPreset(createRiskPolicyInput{ProjectSlug: "project", PolicyType: "standard", Sources: []string{"gitleaks"}, IdempotencyKey: "key"})
	require.NoError(t, err)
	require.Equal(t, "standard", input.PolicyType)
}

func TestCreateRiskPolicySchemaAcceptsPresetBranch(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-preset-schema-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	registerUnavailableRiskTools(reg)
	create := descriptorByName(t, reg, "create_risk_policy")
	ctx := ContextWithPrincipal(t.Context(), testRiskPrincipal("user"))

	_, err := create.Invoke(ctx, json.RawMessage(`{"project_slug":"project","preset":"secrets_and_credentials","idempotency_key":"key"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal, "a preset request passes schema validation and reaches the handler")
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)

	_, err = create.Invoke(ctx, json.RawMessage(`{"project_slug":"project","preset":"secrets_and_credentials","name":"Team secrets","action":"warn","score":3,"enabled":false,"approved_email_domains":["example.com"],"idempotency_key":"key"}`))
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)

	// The schema is one flat object (Anthropic rejects a top-level oneOf), so
	// it still refuses unknown values and fields, and the handler refuses the
	// wrong mix of fields for each kind of request.
	for name, arguments := range map[string]string{
		"unknown preset":            `{"project_slug":"project","preset":"missing","idempotency_key":"key"}`,
		"unknown policy type":       `{"project_slug":"project","policy_type":"regex","name":"policy","enabled":true,"idempotency_key":"key"}`,
		"unknown field":             `{"project_slug":"project","preset":"secrets_and_credentials","idempotency_key":"key","unknown":true}`,
		"scope with unknown field":  `{"project_slug":"project","preset":"secrets_and_credentials","mcp_scope":{"server":"github"},"idempotency_key":"key"}`,
		"scope with bad annotation": `{"project_slug":"project","preset":"secrets_and_credentials","mcp_scope":{"all_servers":true,"tool_annotations":["dangerous"]},"idempotency_key":"key"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := create.Invoke(ctx, json.RawMessage(arguments))
			require.ErrorContains(t, err, "arguments do not match the tool schema")
		})
	}

	type property struct {
		Description string `json:"description"`
		Enum        []any  `json:"enum"`
		Items       struct {
			Description string `json:"description"`
		} `json:"items"`
	}
	var schema struct {
		Type       string              `json:"type"`
		Properties map[string]property `json:"properties"`
		Required   []string            `json:"required"`
		OneOf      []json.RawMessage   `json:"oneOf"`
	}
	require.NoError(t, json.Unmarshal(create.InputSchema, &schema))
	require.Equal(t, "object", schema.Type)
	require.Empty(t, schema.OneOf)
	require.ElementsMatch(t, []string{"project_slug", "idempotency_key"}, schema.Required)
	properties := schema.Properties
	require.Contains(t, properties["sources"].Description, "gitleaks: Secrets")
	require.Contains(t, properties["sources"].Items.Description, "gitleaks: Secrets")
	require.Contains(t, properties["presidio_entities"].Description, "US_SSN: US social security number")
	require.Contains(t, properties["action"].Description, "block: Deny")
	require.Contains(t, properties["policy_type"].Description, "Built-in detectors")
	require.Contains(t, properties["policy_type"].Description, "policy model")
	require.ElementsMatch(t, []any{"standard", "prompt_based"}, properties["policy_type"].Enum)
	require.Contains(t, properties["preset"].Description, "secrets_and_credentials: Secrets and credentials")
	require.Len(t, properties["preset"].Enum, len(presets.IDs()))
	require.Contains(t, properties["mcp_scope"].Description, "before the tool runs")

	_, err = create.Invoke(ctx, json.RawMessage(`{"project_slug":"project","preset":"secrets_and_credentials","mcp_scope":{"servers":[{"mcp_server_id":"11111111-1111-4111-8111-111111111111","tools":["delete_repository"]}],"tool_annotations":["destructiveHint"]},"idempotency_key":"key"}`))
	require.ErrorAs(t, err, &refusal, "a scoped request passes schema validation and reaches the handler")
	require.Contains(t, refusal.Payload, `"code":"feature_unavailable"`)
}

func TestValidateCreateRiskPolicyKind(t *testing.T) {
	t.Parallel()

	enabled := true
	threshold := 0.5
	for name, test := range map[string]struct {
		input   createRiskPolicyInput
		refusal string
	}{
		"preset":                    {input: createRiskPolicyInput{Preset: "secrets_and_credentials", Name: "Team secrets", ApprovedEmailDomains: []string{"example.com"}}},
		"preset prompt override":    {input: createRiskPolicyInput{Preset: "destructive_production_actions", Prompt: "Only the billing database."}},
		"standard":                  {input: createRiskPolicyInput{PolicyType: "standard", Name: "policy", Enabled: &enabled, Sources: []string{"gitleaks"}}},
		"prompt based":              {input: createRiskPolicyInput{PolicyType: "prompt_based", Name: "policy", Enabled: &enabled, Prompt: "instruction"}},
		"preset with policy_type":   {input: createRiskPolicyInput{Preset: "secrets_and_credentials", PolicyType: "standard"}, refusal: "not both"},
		"preset with sources":       {input: createRiskPolicyInput{Preset: "secrets_and_credentials", Sources: []string{"gitleaks"}}, refusal: "sources cannot override a preset"},
		"preset with threshold":     {input: createRiskPolicyInput{Preset: "customer_pii_egress", PresidioScoreThreshold: &threshold}, refusal: "presidio_score_threshold cannot override a preset"},
		"preset with scopes":        {input: createRiskPolicyInput{Preset: "customer_pii_egress", DetectionScopes: []riskDetectionScope{{Category: "pii", MessageTypes: []string{"user"}}}}, refusal: "detection_scopes cannot override a preset"},
		"preset with empty sources": {input: createRiskPolicyInput{Preset: "secrets_and_credentials", Sources: []string{}}, refusal: "sources cannot override a preset"},
		"preset with empty rules":   {input: createRiskPolicyInput{Preset: "secrets_and_credentials", DisabledRules: []string{}}, refusal: "disabled_rules cannot override a preset"},
		"neither preset nor type":   {input: createRiskPolicyInput{Name: "policy", Enabled: &enabled, Sources: []string{"gitleaks"}}, refusal: "Name a preset, or set policy_type"},
		"standard without name":     {input: createRiskPolicyInput{PolicyType: "standard", Enabled: &enabled, Sources: []string{"gitleaks"}}, refusal: "needs name and enabled"},
		"standard without enabled":  {input: createRiskPolicyInput{PolicyType: "standard", Name: "policy", Sources: []string{"gitleaks"}}, refusal: "needs name and enabled"},
		"standard without sources":  {input: createRiskPolicyInput{PolicyType: "standard", Name: "policy", Enabled: &enabled}, refusal: "at least one source"},
		"standard with prompt":      {input: createRiskPolicyInput{PolicyType: "standard", Name: "policy", Enabled: &enabled, Sources: []string{"gitleaks"}, Prompt: "instruction"}, refusal: "prompt is only for policy_type prompt_based"},
		"prompt without prompt":     {input: createRiskPolicyInput{PolicyType: "prompt_based", Name: "policy", Enabled: &enabled}, refusal: "needs a prompt"},
		"prompt with sources":       {input: createRiskPolicyInput{PolicyType: "prompt_based", Name: "policy", Enabled: &enabled, Prompt: "instruction", Sources: []string{"gitleaks"}}, refusal: "takes no detector settings"},
		"prompt with email domains": {input: createRiskPolicyInput{PolicyType: "prompt_based", Name: "policy", Enabled: &enabled, Prompt: "instruction", ApprovedEmailDomains: []string{"example.com"}}, refusal: "takes no detector settings"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateCreateRiskPolicyKind(test.input)
			if test.refusal == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrRiskMutationInvalid)
			var mutation *RiskMutationError
			require.ErrorAs(t, err, &mutation)
			require.Equal(t, "invalid_request", mutation.Code)
			require.Contains(t, mutation.Message, test.refusal)
			require.Contains(t, mutation.Message, "No policy was created.")
		})
	}
}

func TestRiskPresetToolsAnswerWithoutServices(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "risk-preset-tools-test", Version: "0.0.1"}, nil)
	reg := newRegistrar(server)
	registerRiskPresetTools(reg)
	ctx := ContextWithPrincipal(t.Context(), testRiskPrincipal("user"))

	list := descriptorByName(t, reg, "list_risk_presets")
	require.Equal(t, ProjectScopeNone, list.Meta.ProjectScope)
	require.True(t, list.Annotations.ReadOnlyHint)
	listed, err := list.Invoke(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	var presetsOut ListRiskPresetsOutput
	roundTrip(t, listed, &presetsOut)
	require.Len(t, presetsOut.Presets, len(presets.IDs()))
	for _, preset := range presetsOut.Presets {
		require.NotNil(t, preset.Sources)
		require.NotNil(t, preset.PresidioEntities)
		sessionOnly := preset.ID == "unapproved_mcp_servers" || preset.ID == "non_corporate_accounts"
		require.Equal(t, !sessionOnly, preset.CanScopeToMCP, preset.ID)
	}
	_, err = list.Invoke(ctx, json.RawMessage(`{"project_slug":"project"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")

	suggest := descriptorByName(t, reg, "suggest_risk_policy")
	require.True(t, suggest.Annotations.ReadOnlyHint)
	suggested, err := suggest.Invoke(ctx, json.RawMessage(`{"description":"stop agents deleting anything in production"}`))
	require.NoError(t, err)
	var suggestion SuggestRiskPolicyOutput
	roundTrip(t, suggested, &suggestion)
	require.Equal(t, "destructive_production_actions", suggestion.Preset)
	require.Equal(t, "destructive_production_actions", suggestion.Draft.Preset)
	require.Equal(t, "prompt_based", suggestion.Draft.PolicyType)
	require.Equal(t, "block", suggestion.Draft.Action)
	require.True(t, suggestion.Draft.Enabled, "the draft carries the enabled default the create branches require")
	require.True(t, suggestion.RequiresPromptPolicies)
	require.True(t, suggestion.CanScopeToMCP)
	require.NotEmpty(t, suggestion.Draft.Prompt)
	require.Greater(t, suggestion.Confidence, 0.0)
	require.Contains(t, suggestion.NextStep, "create_risk_policy")

	_, err = suggest.Invoke(ctx, json.RawMessage(`{"description":"     "}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal, "a whitespace-only description is refused instead of drafting an empty prompt")
	require.Contains(t, refusal.Payload, `"code":"invalid_request"`)

	bespoke, err := suggest.Invoke(ctx, json.RawMessage(`{"description":"Agents must never promise refunds to customers"}`))
	require.NoError(t, err)
	var bespokeOut SuggestRiskPolicyOutput
	roundTrip(t, bespoke, &bespokeOut)
	require.Empty(t, bespokeOut.Preset)
	require.Equal(t, "prompt_based", bespokeOut.Draft.PolicyType)
	require.Equal(t, "Agents must never promise refunds to customers", bespokeOut.Draft.Prompt)
	require.Empty(t, bespokeOut.Alternatives)

	_, err = suggest.Invoke(ctx, json.RawMessage(`{"description":"no"}`))
	require.ErrorContains(t, err, "arguments do not match the tool schema")
}

func roundTrip(t *testing.T, value any, target any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, target))
}
