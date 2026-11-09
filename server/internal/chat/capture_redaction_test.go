package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// This fixed synthetic credential exercises the provider rule, not a live key.
const captureSyntheticPAT = "ghp_" + "b847c2d93a5e4f6071cb9368d014a2e75f69"

func TestCaptureRedactionProtectsAllMessageRepresentations(t *testing.T) {
	t.Parallel()
	text := "結果 🐕\nMISE_GITHUB_TOKEN=" + captureSyntheticPAT + "\nkeep this output"
	arguments, err := json.Marshal(map[string]string{"output": text})
	require.NoError(t, err)
	toolCalls, err := json.Marshal([]any{map[string]any{"function": map[string]any{"name": "shell", "arguments": string(arguments)}}})
	require.NoError(t, err)
	raw, err := json.Marshal([]any{map[string]any{"type": "text", "text": text}})
	require.NoError(t, err)
	var params repo.CreateChatMessageParams
	params.Content, params.ContentRaw, params.ToolCalls = text, raw, toolCalls
	params.Source = conv.ToPGText("codex")
	original := []MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceHook}}
	captured := redactCaptureWrites(t.Context(), original)
	require.NotContains(t, captured[0].Params.Content+string(captured[0].Params.ContentRaw)+string(captured[0].Params.ToolCalls), captureSyntheticPAT)
	require.Contains(t, captured[0].Params.Content, "keep this output")
	require.True(t, json.Valid(captured[0].Params.ContentRaw))
	require.True(t, json.Valid(captured[0].Params.ToolCalls))
	require.Equal(t, text, original[0].Params.Content)
	require.Equal(t, raw, original[0].Params.ContentRaw)
	require.Equal(t, toolCalls, original[0].Params.ToolCalls)
}

func TestCaptureRedactionLeavesLiveAssistantHistoryUntouched(t *testing.T) {
	t.Parallel()
	var params repo.CreateChatMessageParams
	params.Content = "MISE_GITHUB_TOKEN=" + captureSyntheticPAT
	params.ContentRaw = []byte(`"` + params.Content + `"`)
	params.Source = conv.ToPGText("codex")
	original := []MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceAssistant}}
	require.Equal(t, original, redactCaptureWrites(t.Context(), original))
}

func TestCaptureRedactionProtectsInlineAndBlobBeforeHashing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := assetstest.NewTestBlobStore(t)
	text := strings.Repeat("visible output ", 5000) + "\nMISE_GITHUB_TOKEN=" + captureSyntheticPAT
	row := chatMessageRow{
		chatID: uuid.New(), projectID: uuid.New(), role: "user",
		content: openrouter.CreateMessageUser(text), metadata: httpMetadata{Source: "codex"},
	}
	writes, err := prepareMessages(ctx, testenv.NewLogger(t), store, []chatMessageRow{row})
	require.NoError(t, err)
	require.Len(t, writes, 1)
	param := writes[0].Params
	require.NotContains(t, param.Content, captureSyntheticPAT)
	require.NotContains(t, string(param.ContentRaw), captureSyntheticPAT)
	body, err := blobio.ReadAllString(ctx, store, param.ContentAssetUrl.String, MaxAssetReadSize)
	require.NoError(t, err)
	require.NotContains(t, body, captureSyntheticPAT)
	require.Contains(t, body, "visible output")
	require.Equal(t, text, openrouter.GetText(row.content))
	require.NotContains(t, DerivedTitle("MISE_GITHUB_TOKEN="+captureSyntheticPAT), captureSyntheticPAT)
}

func TestCaptureRedactionIdentityDistinguishesCredentialEdits(t *testing.T) {
	t.Parallel()
	first := "MISE_GITHUB_TOKEN=" + captureSyntheticPAT
	second := "MISE_GITHUB_TOKEN=ghp_" + "c958d3e04b6f507182dc0479e125b3f86a70"
	stored := slotFromStored("user", "MISE_GITHUB_TOKEN=[redacted]", "", nil)
	identity := captureSlot(stored, captureContentHash(first))
	require.Equal(t, captureSlot(slotFromStored("user", first, "", nil), nil), identity)
	require.NotEqual(t, captureSlot(slotFromStored("user", second, "", nil), nil), identity)
}

func TestCaptureRedactionUsesContextAcrossMessageFields(t *testing.T) {
	t.Parallel()
	// Synthetic pair also used by the detector's format fixtures.
	const accessID = "ASIAZ2XY3WNBQR5TUVWX"
	const secret = "wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"
	var params repo.CreateChatMessageParams
	params.Content = "AWS_ACCESS_KEY_ID=" + accessID
	params.ToolCalls = []byte(`[{"function":{"name":"shell","arguments":"{\"SecretAccessKey\":\"` + secret + `\"}"}}]`)
	params.Source = conv.ToPGText("codex")
	got := redactCaptureWrites(t.Context(), []MessageWrite{{Params: params, WorkloadSource: metering.WorkloadSourceHook}})
	require.NotContains(t, string(got[0].Params.ToolCalls), secret)
	require.Contains(t, got[0].Params.Content, accessID)
	require.Nil(t, got[0].Params.ContentRaw)
	require.True(t, json.Valid(got[0].Params.ToolCalls))
}

func TestCaptureRedactionDisablesOnlyPassiveRawTextTee(t *testing.T) {
	t.Parallel()
	require.False(t, canTeeCompletionText(t.Context(), billing.ModelUsageSource("codex")))
	require.True(t, canTeeCompletionText(t.Context(), billing.ModelUsageSourceAssistants))
	require.True(t, canTeeCompletionText(t.Context(), billing.ModelUsageSourcePlayground))
	ctx := contextvalues.SetAssistantPrincipal(t.Context(), contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	require.True(t, canTeeCompletionText(ctx, billing.ModelUsageSource("codex")))
}
