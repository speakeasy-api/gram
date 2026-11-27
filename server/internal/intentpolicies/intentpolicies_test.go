package intentpolicies_test

import (
	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/intent_policies"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const transcript = `{"messages":[
	{"role":"user","content":"push it"},
	{"role":"assistant","content":[
		{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}},
		{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"git push --force origin main"}}
	]},
	{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a"},{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}
]}`

func TestEndpointsRequireRolloutFlag(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestServiceWithFlags(t, classifier.Noop{}, new(feature.InMemory))

	_, err := svc.GetGuardrail(ctx, &gen.GetGuardrailPayload{SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: uuid.NewString(), Evaluator: "jev", Generation: nil, Offset: 0, Limit: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "x", Content: transcript})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRolloutTargetsPeopleByEmail(t *testing.T) {
	t.Parallel()

	member := new(feature.InMemory)
	member.SetFlag(feature.FlagIntentPolicies, mockidp.MockUserEmail, true)
	ctx, svc := newTestServiceWithFlags(t, classifier.Noop{}, member)
	_, err := svc.GetGuardrail(ctx, &gen.GetGuardrailPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	// Rolled out to someone else in the same organization.
	other := new(feature.InMemory)
	other.SetFlag(feature.FlagIntentPolicies, "someone-else@example.com", true)
	ctx, svc = newTestServiceWithFlags(t, classifier.Noop{}, other)
	_, err = svc.GetGuardrail(ctx, &gen.GetGuardrailPayload{SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestEndpointsRequireChatPermissions(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: uuid.NewString(), Evaluator: "jev", Generation: nil, Offset: 0, Limit: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "x", Content: transcript})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetGuardrail(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	g, err := svc.GetGuardrail(ctx, &gen.GetGuardrailPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, intentpolicy.GuardrailVersion, g.Version)
	require.Len(t, g.Statements, 10)
	require.Len(t, g.Questions, 6)
	require.Len(t, g.Contracts, 10)
}

func TestUploadAndAnalyzeWithGemini(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "upload.json", Content: transcript})
	require.NoError(t, err)
	require.Equal(t, "messages", up.Format)
	require.Equal(t, 4, up.MessageCount)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil, Offset: 0, Limit: nil})
	require.NoError(t, err)
	require.Len(t, res.Assessments, 2)
	require.Equal(t, "gemini", res.Evaluator.Evaluator)
	require.Equal(t, "t1", res.Assessments[0].ToolCallID)
	require.Equal(t, "permit", res.Assessments[0].Decision)
	require.JSONEq(t, `{"command":"ls"}`, res.Assessments[0].Arguments)
	require.Equal(t, "block", res.Assessments[1].Decision)
	require.Equal(t, []string{"no_remote_destruction"}, res.Assessments[1].DecidedBy)
	require.Equal(t, 2, res.Totals.Judged)
	require.Equal(t, 1, res.Totals.Blocked)
	require.InDelta(t, 0.004, res.Totals.CostUsd, 1e-9)
	require.False(t, res.LimitHit)
}

func TestAnalyzePublishesAssessments(t *testing.T) {
	t.Parallel()
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagIntentPolicies, feature.AnyDistinctID, true)
	ctx, svc, results := newTestServiceWithResults(t, classifier.Noop{}, flags)

	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "upload.json", Content: transcript})
	require.NoError(t, err)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil, Offset: 0, Limit: nil})
	require.NoError(t, err)

	published := results.published()
	require.Len(t, published, 2)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	for i, msg := range published {
		require.Equal(t, published[0].GetAnalysisId(), msg.GetAnalysisId(), "one analysis id per request")
		require.Equal(t, authCtx.ActiveOrganizationID, msg.GetOrganizationId())
		require.Equal(t, authCtx.ProjectID.String(), msg.GetProjectId())
		require.Equal(t, authCtx.UserID, msg.GetActor().GetUserId())
		require.NotNil(t, authCtx.Email, "the test session carries an email")
		require.Equal(t, *authCtx.Email, msg.GetActor().GetUserEmail())
		require.Equal(t, up.ChatID, msg.GetToolCall().GetChatId())
		require.Equal(t, res.Assessments[i].ToolCallID, msg.GetToolCall().GetToolCallId())
		require.Equal(t, res.Assessments[i].Decision, msg.GetDecision())
		require.Equal(t, "gemini", msg.GetEvaluator().GetKind())
		require.Equal(t, intentpolicy.DefaultGuardrail().Version, msg.GetGuardrail().GetVersion())
		require.Equal(t, intentpolicy.TriggerDashboard, msg.GetTriggerKind())
		require.Len(t, msg.GetLabels(), 6)
		require.Len(t, msg.GetContracts(), len(intentpolicy.DefaultGuardrail().Contracts))
		require.Positive(t, msg.GetToolCall().GetArgumentsBytes())
	}
	require.Equal(t, []string{"no_remote_destruction"}, published[1].GetDecidedBy())

	// A second run of the same calls shares logical ids but not the analysis id.
	_, err = svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil, Offset: 0, Limit: nil})
	require.NoError(t, err)
	again := results.published()[2:]
	require.Len(t, again, 2)
	require.Equal(t, published[0].GetId(), again[0].GetId())
	require.NotEqual(t, published[0].GetAnalysisId(), again[0].GetAnalysisId())
}

func TestAnalyzeInBatches(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "upload.json", Content: transcript})
	require.NoError(t, err)

	limit := int32(1)
	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil, Offset: 1, Limit: &limit})
	require.NoError(t, err)
	require.Len(t, res.Assessments, 1)
	require.Equal(t, "t2", res.Assessments[0].ToolCallID)
	require.Equal(t, 2, res.Totals.Calls, "totals count the whole session")
	require.Equal(t, 1, res.Totals.Judged)
}

func TestAnalyzeWithDisabledJevFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "upload.json", Content: transcript})
	require.NoError(t, err)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "jev", Generation: nil, Offset: 0, Limit: nil})
	require.NoError(t, err)
	for _, a := range res.Assessments {
		require.Equal(t, "block", a.Decision)
		require.NotNil(t, a.EvaluatorError)
		require.Empty(t, a.Labels)
	}
}

func TestAnalyzeUnknownChat(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	_, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: uuid.NewString(), Evaluator: "jev", Generation: nil, Offset: 0, Limit: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUploadRejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	_, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "x", Content: "hello"})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUploadStripsNULCharacters(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	content := `{"messages":[` +
		`{"role":"user","content":"read the log\u0000"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"cat a\u0000b"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"bin\u0000ary"}]}]}`
	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "nul.json", Content: content})
	require.NoError(t, err)
	require.Equal(t, 3, up.MessageCount)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil, Offset: 0, Limit: nil})
	require.NoError(t, err)
	require.Len(t, res.Assessments, 1)
	require.JSONEq(t, `{"command":"cat ab"}`, res.Assessments[0].Arguments)
}
