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
	require.Len(t, decoded["grounding"], 10)
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
	// 5 effect classes + 6 target locations + 2 payload data + 3 choices.
	require.Len(t, req.Questions, 16)
}

func TestJevEvaluatorFoldsAnswers(t *testing.T) {
	t.Parallel()

	in := labelInput(t)
	outcomes := []classifier.QuestionOutcome{
		noulAnswer("effect_class.read", 0.1),
		noulAnswer("effect_class.write", 0.2),
		noulAnswer("effect_class.outbound", 0.05),
		noulAnswer("effect_class.destructive", 0.95),
		noulAnswer("effect_class.exec", 0.3),
		noulAnswer("target_locations.scratch", 0.05),
		noulAnswer("target_locations.workspace", 0.85),
		noulAnswer("target_locations.system", 0.02),
		noulAnswer("target_locations.credential_store", 0.03),
		noulAnswer("target_locations.persistence", 0.04),
		noulAnswer("target_locations.remote", 0.01),
		noulAnswer("payload_data.credential", 0.02),
		noulAnswer("payload_data.personal", 0.01),
		{Key: intentpolicy.LabelExecCodeOrigin, Answer: &classifier.Answer{Noul: nil, Score: nil, Choice: &classifier.ChoiceAnswer{
			Selected:     intentpolicy.CodeOriginNone,
			Distribution: []classifier.Probability{{Option: intentpolicy.CodeOriginNone, Value: 0.9}, {Option: intentpolicy.CodeOriginProjectTask, Value: 0.02}, {Option: intentpolicy.CodeOriginLocalScript, Value: 0.02}, {Option: intentpolicy.CodeOriginInline, Value: 0.02}, {Option: intentpolicy.CodeOriginNetwork, Value: 0.02}, {Option: intentpolicy.CodeOriginUnknown, Value: 0.02}},
			Confidence:   nil,
		}}, Failure: nil},
		{Key: intentpolicy.LabelAudience, Answer: &classifier.Answer{Noul: nil, Score: nil, Choice: &classifier.ChoiceAnswer{
			Selected:     intentpolicy.AudienceNone,
			Distribution: []classifier.Probability{{Option: intentpolicy.AudienceNone, Value: 0.9}, {Option: intentpolicy.AudienceDirect, Value: 0.02}, {Option: intentpolicy.AudienceGroup, Value: 0.02}, {Option: intentpolicy.AudienceBroad, Value: 0.02}, {Option: intentpolicy.AudiencePublic, Value: 0.02}, {Option: intentpolicy.AudienceUnknown, Value: 0.02}},
			Confidence:   nil,
		}}, Failure: nil},
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

	locations := labels[intentpolicy.LabelTargetLocations]
	require.Equal(t, []string{intentpolicy.LocationWorkspace}, locations.Value)
	require.InDelta(t, 0.85, locations.Probability, 1e-9)
	require.Equal(t, -1, locations.Rank)

	payload := labels[intentpolicy.LabelPayloadData]
	require.Equal(t, []string{}, payload.Value)
	require.InDelta(t, 0.98, payload.Probability, 1e-9)
	require.Equal(t, -1, payload.Rank)

	coverage := labels[intentpolicy.LabelEffectCoverage]
	require.Equal(t, intentpolicy.CoverageUnderstood, coverage.Value)
	require.Equal(t, 2, coverage.Rank)

	decision, decidedBy, _ := newEngine(t).Evaluate(in.Action, labels)
	require.Equal(t, intentpolicy.DecisionClarify, decision)
	require.Equal(t, []string{"review_destruction"}, decidedBy)
}

func TestJevEvaluatorOmitsFailedLabels(t *testing.T) {
	t.Parallel()

	in := labelInput(t)
	outcomes := []classifier.QuestionOutcome{
		noulAnswer("effect_class.read", 0.9),
		{Key: "effect_class.write", Answer: nil, Failure: &classifier.QuestionFailure{Code: classifier.FailureProviderUnavailable, Message: "down", Retryable: true, RetryAfter: nil}},
		noulAnswer("payload_data.credential", 0.1),
		noulAnswer("payload_data.personal", 0.05),
	}
	mockClassifier := classifiertest.NewMock(t)
	mockClassifier.On("Classify", mock.Anything, mock.Anything).Return(classifier.NewResult(outcomes, nil)).Once()

	labels, _, err := intentpolicy.NewJevEvaluator(mockClassifier).Label(t.Context(), in)
	require.Error(t, err)
	require.NotContains(t, labels, intentpolicy.LabelEffectClass)
	require.Contains(t, labels, intentpolicy.LabelPayloadData)
}

func TestParseGeminiLabels(t *testing.T) {
	t.Parallel()

	raw := `{
		"effect_class": {"value": ["exec", "read"], "confidence": 0.8, "rationale": "Runs a script."},
		"target_locations": {"value": ["workspace"], "confidence": 0.9, "rationale": "Local file."},
		"payload_data": {"value": [], "confidence": 0.9, "rationale": "No sensitive data."},
		"exec_code_origin": {"value": "local_script", "confidence": 0.85, "rationale": "Workspace script."},
		"audience": {"value": "none", "confidence": 0.95, "rationale": "Not outbound."},
		"effect_coverage": {"value": "unknown", "confidence": 1.4, "rationale": "Opaque script."}
	}`
	labels, err := intentpolicy.ParseGeminiLabels(intentpolicy.DefaultGuardrail().Questions, []byte(raw))
	require.NoError(t, err)
	require.Equal(t, []string{"read", "exec"}, labels[intentpolicy.LabelEffectClass].Value)
	require.Equal(t, "Runs a script.", labels[intentpolicy.LabelEffectClass].Rationale)
	require.Equal(t, []string{"workspace"}, labels[intentpolicy.LabelTargetLocations].Value)
	require.Equal(t, []string{}, labels[intentpolicy.LabelPayloadData].Value)
	require.Equal(t, intentpolicy.CodeOriginLocalScript, labels[intentpolicy.LabelExecCodeOrigin].Value)
	require.Equal(t, intentpolicy.AudienceNone, labels[intentpolicy.LabelAudience].Value)
	require.Equal(t, -1, labels[intentpolicy.LabelEffectCoverage].Rank)
	require.InDelta(t, 1.0, labels[intentpolicy.LabelEffectCoverage].Probability, 1e-9)
}

func TestParseGeminiLabelsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	raw := `{"effect_class": {"value": ["teleport"], "confidence": 0.5, "rationale": ""}, "payload_data": {"value": "credential", "confidence": 0.5, "rationale": ""}, "audience": {"value": null, "confidence": 0.5, "rationale": ""}}`
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
	analysis, err := intentpolicy.Analyze(t.Context(), g, newEngine(t), fixedEvaluator{labels: benignLabels()}, actions, 0, 0)
	require.NoError(t, err)
	require.True(t, analysis.LimitHit)
	require.Equal(t, intentpolicy.MaxActions+5, analysis.Totals.Calls)
	require.Equal(t, intentpolicy.MaxActions, analysis.Totals.Judged)
	require.Equal(t, intentpolicy.MaxActions, analysis.Totals.Permitted)
}
