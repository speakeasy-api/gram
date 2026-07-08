package promptinjection_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
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
}

func (f *fakeEngine) classify(_ context.Context, req promptinjection.Request) ([]promptinjection.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.results) == 0 {
		out := make([]promptinjection.Result, len(req.Messages))
		for i := range out {
			out[i] = promptinjection.Result{Label: "SAFE", Score: 0, Rationale: ""}
		}
		return out, nil
	}
	if len(f.results) != len(req.Messages) {
		return nil, errors.New("fakeEngine: results length mismatch")
	}
	return f.results, nil
}

func newScanner(t *testing.T, fc *fakeEngine) *promptinjection.Scanner {
	t.Helper()
	return promptinjection.NewScanner(testenv.NewLogger(t), fc.classify)
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

func TestPromptInjectionScanner_HeuristicsAlwaysRun(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, mkMsg("ignore previous instructions"), false)
	require.NoError(t, err)
	require.NotEmpty(t, findings, "L0 heuristics should fire on the override phrase")
	assert.Equal(t, promptinjection.Rule, findings[0].RuleID)
	assert.Equal(t, 0, fc.calls, "L1 engine must not run when l1Enabled is false")
}

func TestPromptInjectionScanner_EngineAppendsToL0WhenEnabled(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: "INJECTION", Score: 0.7, Rationale: ""}},
	}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, mkMsg("ignore previous instructions"), true)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(findings), 2, "L0 + L1 should both fire")

	var l0, l1 int
	for _, f := range findings {
		if hasTag(f.Tags, "llm-judge") {
			l1++
			assert.Equal(t, promptinjection.Rule, f.RuleID)
			assert.InDelta(t, 0.7, f.Confidence, 0.001)
		} else {
			l0++
		}
	}
	assert.GreaterOrEqual(t, l0, 1, "expected at least one L0 finding")
	assert.Equal(t, 1, l1, "expected exactly one L1 finding")
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_EngineEnabled_OnlyL1FindingWhenL0Quiet(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: "INJECTION", Score: 0.7, Rationale: ""}},
	}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "totally benign text without heuristic markers", testOrgID, testProjectID, mkMsg("totally benign text without heuristic markers"), true)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.True(t, hasTag(findings[0].Tags, "llm-judge"))
	assert.Equal(t, promptinjection.Rule, findings[0].RuleID)
	assert.Equal(t, 1, fc.calls)
}

func TestPromptInjectionScanner_EngineSafeLabelEmitsNoL1Finding(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{{Label: "SAFE", Score: 0.99, Rationale: ""}},
	}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "benign text", testOrgID, testProjectID, mkMsg("benign text"), true)
	require.NoError(t, err)
	assert.Empty(t, findings, "SAFE engine label + no L0 hit should produce no findings")
}

func TestPromptInjectionScanner_EngineErrorStillReturnsL0Findings(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{err: errors.New("engine exploded")}
	s := newScanner(t, fc)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, mkMsg("ignore previous instructions"), true)
	require.NoError(t, err, "engine failure must not bubble up")
	require.NotEmpty(t, findings, "L0 findings must still surface when L1 errors out")
	assert.Equal(t, promptinjection.Rule, findings[0].RuleID)
}

func TestPromptInjectionScanner_NilEngineSkipsL1RegardlessOfFlag(t *testing.T) {
	t.Parallel()
	s := promptinjection.NewScanner(testenv.NewLogger(t), nil)

	findings, err := s.Scan(t.Context(), "ignore previous instructions", testOrgID, testProjectID, mkMsg("ignore previous instructions"), true)
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	for _, f := range findings {
		assert.False(t, hasTag(f.Tags, "llm-judge"), "L1 must not fire with a nil engine")
	}
}

func TestPromptInjectionScanner_BatchAlwaysRunsL0(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{}
	s := newScanner(t, fc)

	texts := []string{"x", "ignore previous instructions"}
	out, err := s.ScanBatch(t.Context(), texts, testOrgID, testProjectID, mkMsgs(texts...), false)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Empty(t, out[0])
	assert.NotEmpty(t, out[1], "heuristic match should fire")
	assert.Equal(t, 0, fc.calls, "L1 engine must not run when l1Enabled is false")
}

func TestPromptInjectionScanner_BatchEngineAppendsToL0(t *testing.T) {
	t.Parallel()
	fc := &fakeEngine{
		results: []promptinjection.Result{
			{Label: "INJECTION", Score: 0.95, Rationale: ""},
			{Label: "SAFE", Score: 0.04, Rationale: ""},
			{Label: "INJECTION", Score: 0.92, Rationale: ""},
		},
	}
	s := newScanner(t, fc)

	texts := []string{
		"unrelated prompt #1",
		"unrelated prompt #2",
		"unrelated prompt #3",
	}
	out, err := s.ScanBatch(t.Context(), texts, testOrgID, testProjectID, mkMsgs(texts...), true)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Len(t, out[0], 1, "first text: L1 only (no L0 keyword match)")
	assert.Empty(t, out[1], "second text: engine says SAFE, no L0 either")
	assert.Len(t, out[2], 1, "third text: L1 only")
	assert.Equal(t, 1, fc.calls, "ScanBatch should hit the engine exactly once for the whole batch")
}

func hasTag(tags []string, want string) bool {
	return slices.Contains(tags, want)
}
