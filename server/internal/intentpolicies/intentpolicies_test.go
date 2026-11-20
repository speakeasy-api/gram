package intentpolicies_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/intent_policies"
	"github.com/speakeasy-api/gram/server/internal/classifier"
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

func TestGetGuardrailRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	_, err := svc.GetGuardrail(ctx, &gen.GetGuardrailPayload{SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: uuid.NewString(), Evaluator: "jev", Generation: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "x", Content: transcript})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetGuardrail(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	g, err := svc.GetGuardrail(withAdmin(t, ctx), &gen.GetGuardrailPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, intentpolicy.GuardrailVersion, g.Version)
	require.Len(t, g.Statements, 8)
	require.Len(t, g.Questions, 7)
	require.Len(t, g.Contracts, 8)
}

func TestUploadAndAnalyzeWithGemini(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})
	ctx = withAdmin(t, ctx)

	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "upload.json", Content: transcript})
	require.NoError(t, err)
	require.Equal(t, "messages", up.Format)
	require.Equal(t, 4, up.MessageCount)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil})
	require.NoError(t, err)
	require.Len(t, res.Assessments, 2)
	require.Equal(t, "gemini", res.Evaluator.Evaluator)
	require.Equal(t, "t1", res.Assessments[0].ToolCallID)
	require.Equal(t, "permit", res.Assessments[0].Decision)
	require.JSONEq(t, `{"command":"ls"}`, res.Assessments[0].Arguments)
	require.Equal(t, "block", res.Assessments[1].Decision)
	require.Equal(t, []string{"no_force_push"}, res.Assessments[1].DecidedBy)
	require.Equal(t, 2, res.Totals.Judged)
	require.Equal(t, 1, res.Totals.Blocked)
	require.InDelta(t, 0.004, res.Totals.CostUsd, 1e-9)
	require.False(t, res.LimitHit)
}

func TestAnalyzeWithDisabledJevFailsClosed(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})
	ctx = withAdmin(t, ctx)

	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "upload.json", Content: transcript})
	require.NoError(t, err)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "jev", Generation: nil})
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

	_, err := svc.AnalyzeSession(withAdmin(t, ctx), &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: uuid.NewString(), Evaluator: "jev", Generation: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUploadRejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})

	_, err := svc.UploadTranscript(withAdmin(t, ctx), &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "x", Content: "hello"})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUploadStripsNULCharacters(t *testing.T) {
	t.Parallel()
	ctx, svc := newTestService(t, classifier.Noop{})
	ctx = withAdmin(t, ctx)

	content := `{"messages":[` +
		`{"role":"user","content":"read the log\u0000"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"cat a\u0000b"}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"bin\u0000ary"}]}]}`
	up, err := svc.UploadTranscript(ctx, &gen.UploadTranscriptPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "nul.json", Content: content})
	require.NoError(t, err)
	require.Equal(t, 3, up.MessageCount)

	res, err := svc.AnalyzeSession(ctx, &gen.AnalyzeSessionPayload{SessionToken: nil, ProjectSlugInput: nil, ChatID: up.ChatID, Evaluator: "gemini", Generation: nil})
	require.NoError(t, err)
	require.Len(t, res.Assessments, 1)
	require.JSONEq(t, `{"command":"cat ab"}`, res.Assessments[0].Arguments)
}
