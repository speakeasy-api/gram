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

	for name, arguments := range map[string]string{
		"unknown preset":            `{"project_slug":"project","preset":"missing","idempotency_key":"key"}`,
		"preset with policy_type":   `{"project_slug":"project","preset":"secrets_and_credentials","policy_type":"standard","idempotency_key":"key"}`,
		"preset with sources":       `{"project_slug":"project","preset":"secrets_and_credentials","sources":["gitleaks"],"idempotency_key":"key"}`,
		"standard without sources":  `{"project_slug":"project","policy_type":"standard","name":"policy","enabled":true,"idempotency_key":"key"}`,
		"standard without policy":   `{"project_slug":"project","name":"policy","enabled":true,"sources":["gitleaks"],"idempotency_key":"key"}`,
		"prompt branch with preset": `{"project_slug":"project","policy_type":"prompt_based","name":"policy","enabled":true,"prompt":"instruction","preset":"prompt_injection","idempotency_key":"key"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := create.Invoke(ctx, json.RawMessage(arguments))
			require.ErrorContains(t, err, "arguments do not match the tool schema")
		})
	}

	var schema struct {
		OneOf []struct {
			Properties map[string]struct {
				Description string `json:"description"`
				Enum        []any  `json:"enum"`
				Items       struct {
					Description string `json:"description"`
				} `json:"items"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	require.NoError(t, json.Unmarshal(create.InputSchema, &schema))
	require.Len(t, schema.OneOf, 3)
	require.Contains(t, schema.OneOf[0].Properties["sources"].Description, "gitleaks: Secrets")
	require.Contains(t, schema.OneOf[0].Properties["sources"].Items.Description, "gitleaks: Secrets")
	require.Contains(t, schema.OneOf[0].Properties["presidio_entities"].Description, "US_SSN: US social security number")
	require.Contains(t, schema.OneOf[0].Properties["action"].Description, "block: Deny")
	require.Contains(t, schema.OneOf[0].Properties["policy_type"].Description, "Built-in detectors")
	require.Contains(t, schema.OneOf[1].Properties["policy_type"].Description, "policy model")
	require.Contains(t, schema.OneOf[2].Properties["preset"].Description, "secrets_and_credentials: Secrets and credentials")
	require.Len(t, schema.OneOf[2].Properties["preset"].Enum, len(presets.IDs()))
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
	require.NotEmpty(t, suggestion.Draft.Prompt)
	require.Greater(t, suggestion.Confidence, 0.0)
	require.Contains(t, suggestion.NextStep, "create_risk_policy")

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
