package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKitReasoningBootstrap(t *testing.T) {
	t.Parallel()
	for _, effort := range []string{"", "low", "medium", "high"} {
		t.Run(effort, func(t *testing.T) {
			t.Parallel()
			b := kitTestWorkloadBootstrap()
			b.ReasoningEffort = effort
			require.NoError(t, validateKitBootstrap(b))
			expected := effort
			if expected == "" {
				expected = "medium"
			}
			require.Contains(t, kitBootstrapConfig(b), fmt.Sprintf("\nreasoning_effort = %q\n[inference_providers.speakeasy]", expected))
			raw, err := kitLocalBootstrapPayload(b, map[string]string{"SPEAKEASY_ORIGIN": "https://ai.speakeasy.com"})
			require.NoError(t, err)
			var payload kitATEBootstrapRequest
			require.NoError(t, json.Unmarshal(raw, &payload))
			var configTOML string
			for _, f := range payload.Files {
				if f.Path == ".kit/config.toml" {
					configTOML = string(f.Content)
				}
			}
			require.Contains(t, configTOML, fmt.Sprintf("\nreasoning_effort = %q\n[inference_providers.speakeasy]", expected))
		})
	}
	for _, effort := range []string{"none", "default", "invalid", "HIGH"} {
		t.Run(effort, func(t *testing.T) {
			t.Parallel()
			b := kitTestWorkloadBootstrap()
			b.ReasoningEffort = effort
			require.ErrorContains(t, validateKitBootstrap(b), "does not support reasoning effort")
			require.Empty(t, kitBootstrapConfig(b))
			_, err := kitLocalBootstrapPayload(b, nil)
			require.ErrorContains(t, err, "does not support reasoning effort")
		})
	}
}

func TestKitProtocolReasoningCatalog(t *testing.T) {
	t.Parallel()
	for _, effort := range []string{"low", "medium", "high"} {
		for _, choices := range []string{
			`[{"value":"low"},{"value":"medium"},{"value":"high"}]`,
			`[{"groupId":"reasoning-effort","options":[{"value":"low"},{"value":"medium"},{"value":"high"}]}]`,
		} {
			raw := json.RawMessage(fmt.Sprintf(`{"configOptions":[{"configId":"reasoning_effort","type":"select","currentValue":%q,"options":%s}]}`, effort, choices))
			selection, current, err := kitProtocolReasoningOption(raw, effort)
			require.NoError(t, err)
			require.Equal(t, effort, selection)
			require.Equal(t, effort, current)
		}
	}
	for _, raw := range []string{
		`{}`, `broken`,
		`{"configOptions":[{"configId":"reasoning_effort","type":"boolean"}]}`,
		`{"configOptions":[{"configId":"reasoning_effort","type":"select","options":[{"value":"default","name":"medium"}]}]}`,
		`{"configOptions":[{"configId":"reasoning_effort","type":"select","options":[{"value":"medium"},{"value":"medium"}]}]}`,
		`{"configOptions":[{"configId":"reasoning_effort","type":"select","options":[{"value":"medium"}]},{"configId":"reasoning_effort","type":"select","options":[{"value":"medium"}]}]}`,
	} {
		_, _, err := kitProtocolReasoningOption(json.RawMessage(raw), "medium")
		require.Error(t, err, raw)
	}
	_, _, err := kitProtocolReasoningOption(json.RawMessage(`{}`), "none")
	require.ErrorContains(t, err, "does not support reasoning effort")
}

func TestKitProtocolRejectsUnsupportedReasoningBeforeStartup(t *testing.T) {
	t.Parallel()
	p := &KitRuntimeProtocol{}
	err := p.RunTurn(context.Background(), assistantRuntimeRecord{}, runTurnRequest{Model: "openai/gpt-4.1", ReasoningEffort: "none"})
	require.ErrorContains(t, err, "does not support reasoning effort")
}
