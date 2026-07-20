package promptinjection_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testOrgID     = "org_test"
	testProjectID = "proj_test"
)

type fakeEngine struct {
	results []promptinjection.Result
	err     error
	calls   int
	lastReq promptinjection.Request
}

func (f *fakeEngine) classify(_ context.Context, req promptinjection.Request) ([]promptinjection.Result, error) {
	f.calls++
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	if len(f.results) == 0 {
		out := make([]promptinjection.Result, len(req.Messages))
		for i := range out {
			out[i] = promptinjection.Result{Label: promptinjection.LabelSafe, Score: 0, Rationale: ""}
		}
		return out, nil
	}
	return f.results, nil
}

func newScanner(t *testing.T, fc *fakeEngine) *promptinjection.Scanner {
	t.Helper()
	return promptinjection.NewScanner(testenv.NewLogger(t), promptinjection.Classifier(fc.classify))
}

func mkMsg(text string) judgemessage.Message {
	return judgemessage.Message{
		Type:        "",
		Body:        text,
		ToolName:    "",
		MCPServer:   "",
		MCPFunction: "",
		ToolCalls:   nil,
	}
}

func mkMsgs(texts ...string) []judgemessage.Message {
	out := make([]judgemessage.Message, len(texts))
	for i, t := range texts {
		out[i] = mkMsg(t)
	}
	return out
}

func TestPromptInjectionScanner_EngineInjectionEmitsFinding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: promptinjection.LabelInjection, Score: 0.7, Rationale: "bad prompt"}},
	}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, "user-scan-1", mkMsg("ignore previous instructions"))
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, promptinjection.Rule, findings[0].RuleID)
	assert.Equal(t, "bad prompt", findings[0].Description)
	assert.InDelta(t, 0.7, findings[0].Confidence, 0.001)
	assert.True(t, hasTag(findings[0].Tags, "llm-judge"))
	assert.True(t, hasTag(findings[0].Tags, "layer-1"))
	assert.Equal(t, []string{"user-scan-1"}, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_EngineSafeLabelEmitsNoFinding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: promptinjection.LabelSafe, Score: 0.99, Rationale: ""}},
	}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, "user-safe", mkMsg("ignore previous instructions"))
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, []string{"user-safe"}, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_EngineErrorEmitsNoFinding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{err: errors.New("engine exploded")}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, "user-error", mkMsg("ignore previous instructions"))
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, []string{"user-error"}, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_EngineMismatchedResultCountEmitsNoFinding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{
			{Label: promptinjection.LabelInjection, Score: 0.7, Rationale: ""},
			{Label: promptinjection.LabelInjection, Score: 0.8, Rationale: ""},
		},
	}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, "user-mismatch", mkMsg("ignore previous instructions"))
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, []string{"user-mismatch"}, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_EmptyTextAndMessageSkipsEngine(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "", testOrgID, testProjectID, "user-empty", mkMsg(""))
	require.NoError(t, err)
	assert.Empty(t, findings)
	assert.Equal(t, 0, fc.calls)
}

func TestPromptInjectionScanner_NoopClassifierEmitsNoFinding(t *testing.T) {
	t.Parallel()
	s := promptinjection.NewScanner(testenv.NewLogger(t), promptinjection.NoopClassifier)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, "user-noop", mkMsg("ignore previous instructions"))
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestPromptInjectionScanner_BatchEngineFindings(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{
			{Label: promptinjection.LabelInjection, Score: 0.95, Rationale: ""},
			{Label: promptinjection.LabelSafe, Score: 0.04, Rationale: ""},
			{Label: promptinjection.LabelInjection, Score: 0.92, Rationale: ""},
		},
	}
	s := newScanner(t, fc)

	texts := []string{
		"unrelated prompt #1",
		"unrelated prompt #2",
		"unrelated prompt #3",
	}
	userIDs := []string{"user-1", "user-2", "user-3"}
	out, err := s.ScanBatch(t.Context(), texts, testOrgID, testProjectID, userIDs, mkMsgs(texts...))
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Len(t, out[0], 1)
	assert.Empty(t, out[1])
	assert.Len(t, out[2], 1)
	assert.Equal(t, userIDs, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_BatchEngineErrorEmitsNoFindings(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{err: errors.New("engine exploded")}
	s := newScanner(t, fc)

	texts := []string{"ignore previous instructions"}
	userIDs := []string{"user-error"}
	out, err := s.ScanBatch(t.Context(), texts, testOrgID, testProjectID, userIDs, mkMsgs(texts...))
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Empty(t, out[0])
	assert.Equal(t, userIDs, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_BatchMismatchedResultCountEmitsNoFindings(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: promptinjection.LabelInjection, Score: 0.95, Rationale: ""}},
	}
	s := newScanner(t, fc)

	texts := []string{"one", "two"}
	userIDs := []string{"user-1", "user-2"}
	out, err := s.ScanBatch(t.Context(), texts, testOrgID, testProjectID, userIDs, mkMsgs(texts...))
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Empty(t, out[0])
	assert.Empty(t, out[1])
	assert.Equal(t, userIDs, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_BatchSkipsEmptyMessageFinding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: promptinjection.LabelInjection, Score: 0.91, Rationale: ""}},
	}
	s := newScanner(t, fc)

	userIDs := []string{"user-empty"}
	out, err := s.ScanBatch(t.Context(), []string{""}, testOrgID, testProjectID, userIDs, []judgemessage.Message{mkMsg("")})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Empty(t, out[0])
	assert.Equal(t, userIDs, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_BatchKeepsEmptyTextToolCallFinding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: promptinjection.LabelInjection, Score: 0.91, Rationale: ""}},
	}
	s := newScanner(t, fc)

	msgs := []judgemessage.Message{
		judgemessage.New(message.ToolRequest, "mcp__github__delete_repo", `{"repo":"prod"}`),
	}
	userIDs := []string{"user-tool"}
	out, err := s.ScanBatch(t.Context(), []string{""}, testOrgID, testProjectID, userIDs, msgs)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0], 1)
	assert.True(t, hasTag(out[0][0].Tags, "llm-judge"))
	assert.Equal(t, userIDs, fc.lastReq.UserIDs)
	assert.Equal(t, 1, fc.calls)
}

func hasTag(tags []string, want string) bool {
	return slices.Contains(tags, want)
}
