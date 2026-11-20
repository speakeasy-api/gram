package intentpolicy_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/classifiertest"
	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

func labelInput(t *testing.T) intentpolicy.LabelInput {
	t.Helper()
	g := intentpolicy.DefaultGuardrail()
	return intentpolicy.LabelInput{Action: bashCall("rm -rf build"), Grounding: g.Statements, Questions: g.Questions}
}

func noulAnswer(key string, p float64) classifier.QuestionOutcome {
	return classifier.QuestionOutcome{Key: classifier.QuestionKey(key), Answer: &classifier.Answer{Noul: &classifier.NoulAnswer{Probability: p}, Choice: nil, Score: nil}, Failure: nil}
}

func TestRenderStateIsSharedAndUntrustedShaped(t *testing.T) {
	t.Parallel()

	in := labelInput(t)
	in.Action.Cwd = "/work"
	state, err := intentpolicy.RenderState(in)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(state, &decoded))
	require.Len(t, decoded["grounding"], 8)
	require.Equal(t, "/work", decoded["working_directory"])
	action, ok := decoded["action"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ai_assistant_tool_call", action["produced_by"])
	require.Equal(t, "arguments", action["body_kind"])
	body, ok := action["body"].(string)
	require.True(t, ok)
	require.JSONEq(t, `{"command":"rm -rf build"}`, body)

	req, err := intentpolicy.CompileJevRequest(in)
	require.NoError(t, err)
	encoded, err := json.Marshal(req.Input)
	require.NoError(t, err)
	require.JSONEq(t, string(state), string(encoded))
	// Five effect classes plus six other labels.
	require.Len(t, req.Questions, 11)
}

func TestJevEvaluatorFoldsAnswers(t *testing.T) {
	t.Parallel()

	in := labelInput(t)
	outcomes := []classifier.QuestionOutcome{
		noulAnswer("effect_class.read", 0.1),
		noulAnswer("effect_class.write_local", 0.2),
		noulAnswer("effect_class.outbound", 0.05),
		noulAnswer("effect_class.destructive", 0.95),
		noulAnswer("effect_class.exec", 0.3),
		noulAnswer(intentpolicy.LabelCarriesSecret, 0.02),
		noulAnswer(intentpolicy.LabelExecutesRemoteCodeInline, 0.01),
		noulAnswer(intentpolicy.LabelTouchesCredentialStore, 0.03),
		noulAnswer(intentpolicy.LabelTouchesPersistence, 0.04),
		noulAnswer(intentpolicy.LabelHistoryRewrite, 0.01),
		{Key: intentpolicy.LabelEffectCoverage, Answer: &classifier.Answer{Noul: nil, Score: nil, Choice: &classifier.ChoiceAnswer{
			Selected:     intentpolicy.CoverageUnderstood,
			Distribution: []classifier.Probability{{Option: intentpolicy.CoverageUnderstood, Value: 0.8}, {Option: intentpolicy.CoveragePartiallyUnderstood, Value: 0.15}, {Option: intentpolicy.CoverageUnknown, Value: 0.05}},
			Confidence:   nil,
		}}, Failure: nil},
	}
	result := classifier.NewResult(outcomes, nil)
	result.Usage = classifier.Usage{InputTokens: 1000, OutputTokens: 20, Complete: true}

	mockClassifier := classifiertest.NewMock(t)
	mockClassifier.On("Classify", mock.Anything, mock.Anything).Return(result).Once()

	labels, usage, err := intentpolicy.NewJevEvaluator(mockClassifier).Label(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, int64(1000), usage.InputTokens)

	effects := labels[intentpolicy.LabelEffectClass]
	require.Equal(t, []string{intentpolicy.EffectDestructive}, effects.Value)
	require.InDelta(t, 0.7, effects.Probability, 1e-9)
	require.InDelta(t, 0.95, effects.Distribution[intentpolicy.EffectDestructive], 1e-9)

	secret := labels[intentpolicy.LabelCarriesSecret]
	require.Equal(t, false, secret.Value)
	require.InDelta(t, 0.98, secret.Probability, 1e-9)
	require.Equal(t, -1, secret.Rank)

	coverage := labels[intentpolicy.LabelEffectCoverage]
	require.Equal(t, intentpolicy.CoverageUnderstood, coverage.Value)
	require.Equal(t, 2, coverage.Rank)

	decision, decidedBy, _ := newEngine(t).Evaluate(in.Action, labels)
	require.Equal(t, intentpolicy.DecisionClarify, decision)
	require.Equal(t, []string{"review_destructive"}, decidedBy)
}

func TestJevEvaluatorOmitsFailedLabels(t *testing.T) {
	t.Parallel()

	in := labelInput(t)
	outcomes := []classifier.QuestionOutcome{
		noulAnswer("effect_class.read", 0.9),
		{Key: "effect_class.write_local", Answer: nil, Failure: &classifier.QuestionFailure{Code: classifier.FailureProviderUnavailable, Message: "down", Retryable: true, RetryAfter: nil}},
		noulAnswer(intentpolicy.LabelCarriesSecret, 0.1),
	}
	mockClassifier := classifiertest.NewMock(t)
	mockClassifier.On("Classify", mock.Anything, mock.Anything).Return(classifier.NewResult(outcomes, nil)).Once()

	labels, _, err := intentpolicy.NewJevEvaluator(mockClassifier).Label(t.Context(), in)
	require.Error(t, err)
	require.NotContains(t, labels, intentpolicy.LabelEffectClass)
	require.Contains(t, labels, intentpolicy.LabelCarriesSecret)
}

func TestParseGeminiLabels(t *testing.T) {
	t.Parallel()

	raw := `{
		"effect_class": {"value": ["exec", "read"], "confidence": 0.8, "rationale": "Runs a script."},
		"carries_secret": {"value": false, "confidence": 0.9, "rationale": "No credential."},
		"executes_remote_code_inline": {"value": false, "confidence": 0.9, "rationale": "Local."},
		"touches_credential_store": {"value": false, "confidence": 0.9, "rationale": "No."},
		"touches_persistence": {"value": false, "confidence": 0.9, "rationale": "No."},
		"history_rewrite": {"value": false, "confidence": 0.9, "rationale": "No."},
		"effect_coverage": {"value": "unknown", "confidence": 1.4, "rationale": "Opaque script."}
	}`
	labels, err := intentpolicy.ParseGeminiLabels(intentpolicy.DefaultGuardrail().Questions, []byte(raw))
	require.NoError(t, err)
	require.Equal(t, []string{"read", "exec"}, labels[intentpolicy.LabelEffectClass].Value)
	require.Equal(t, "Runs a script.", labels[intentpolicy.LabelEffectClass].Rationale)
	require.Equal(t, -1, labels[intentpolicy.LabelEffectCoverage].Rank)
	require.InDelta(t, 1.0, labels[intentpolicy.LabelEffectCoverage].Probability, 1e-9)
}

func TestParseGeminiLabelsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	raw := `{"effect_class": {"value": ["teleport"], "confidence": 0.5, "rationale": ""}, "carries_secret": {"value": "yes", "confidence": 0.5, "rationale": ""}}`
	labels, err := intentpolicy.ParseGeminiLabels(intentpolicy.DefaultGuardrail().Questions, []byte(raw))
	require.Error(t, err)
	require.Empty(t, labels)
}

func TestGeminiSchemaCoversEveryLabel(t *testing.T) {
	t.Parallel()

	qs := intentpolicy.DefaultGuardrail().Questions
	schema := intentpolicy.GeminiSchema(qs)
	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	require.Len(t, props, len(qs))
	require.Len(t, schema["required"], len(qs))
}

func TestExtractActions(t *testing.T) {
	t.Parallel()

	chatID := uuid.New()
	msgID := uuid.New()
	rows := []intentpolicy.MessageRow{
		{ID: uuid.New(), Seq: 1, Generation: 0, Role: "user", ToolCalls: nil},
		{ID: msgID, Seq: 2, Generation: 0, Role: "assistant", ToolCalls: []byte(`[{"id":"c1","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"ls\"}"}},{"id":"c2","type":"function","function":{"name":"mcp__github__create_issue","arguments":"not json"}}]`)},
		{ID: uuid.New(), Seq: 3, Generation: 0, Role: "tool", ToolCalls: nil},
	}
	actions, err := intentpolicy.ExtractActions(chatID, "/work", rows)
	require.NoError(t, err)
	require.Len(t, actions, 2)
	require.Equal(t, msgID, actions[0].MessageID)
	require.Equal(t, "c1", actions[0].ToolCallID)
	require.JSONEq(t, `{"command":"ls"}`, string(actions[0].Arguments))
	require.Equal(t, "github", actions[1].Tool.MCPServer)
	require.Equal(t, "create_issue", actions[1].Tool.MCPFunction)
	require.JSONEq(t, `"not json"`, string(actions[1].Arguments))
	require.Equal(t, "/work", actions[1].Cwd)
}

func TestAnalyzeCapsAndCounts(t *testing.T) {
	t.Parallel()

	actions := make([]intentpolicy.Action, intentpolicy.MaxActions+5)
	for i := range actions {
		actions[i] = mcpCall()
	}
	g := intentpolicy.DefaultGuardrail()
	analysis, err := intentpolicy.Analyze(t.Context(), g, newEngine(t), fixedEvaluator{labels: benignLabels()}, actions)
	require.NoError(t, err)
	require.True(t, analysis.LimitHit)
	require.Equal(t, intentpolicy.MaxActions+5, analysis.Totals.Calls)
	require.Equal(t, intentpolicy.MaxActions, analysis.Totals.Judged)
	require.Equal(t, intentpolicy.MaxActions, analysis.Totals.Permitted)
}
