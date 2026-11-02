package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingMemoryEvalReader struct {
	*recordingOrganizationReader
	listInput *gen.ListMemoryEvalRunsPayload
	getInput  *gen.GetMemoryEvalRunPayload
	run       *gen.AdminMemoryEvalRun
	err       error
}

func (r *recordingMemoryEvalReader) ListMemoryEvalRuns(_ context.Context, input *gen.ListMemoryEvalRunsPayload) (*gen.ListMemoryEvalRunsResult, error) {
	r.listInput = input
	if r.err != nil {
		return nil, r.err
	}
	next := "run-0"
	return &gen.ListMemoryEvalRunsResult{Runs: []*gen.AdminMemoryEvalRun{r.run}, NextCursor: &next}, nil
}

func (r *recordingMemoryEvalReader) GetMemoryEvalRun(_ context.Context, input *gen.GetMemoryEvalRunPayload) (*gen.AdminMemoryEvalRun, error) {
	r.getInput = input
	return r.run, r.err
}

func testMemoryEvalReads() *recordingMemoryEvalReader {
	cost := 3.5
	report := `{"judge_cost_usd":0.4,"scores":[
		{"model":"a/one","arm":"A: no memory","has_tools":false,"accuracy":0.1,"answerable":30,"valid_cited":0,"leaks":null,"isolation_violations":null,"errors":0,"cost_usd":0.2,"latency_p95_ms":900},
		{"model":"a/one","arm":"C: search_team_memory","has_tools":true,"accuracy":0.8,"answerable":30,"valid_cited":24,"leaks":["planted-1"],"isolation_violations":[],"errors":1,"cost_usd":0.7,"latency_p95_ms":5000}],
		"preparation":{"summaries":159,"failed_summaries":1,"digests":89,"summary_spend":{"cost_usd":0.3},"digest_spend":{"cost_usd":0.5}}}`
	return &recordingMemoryEvalReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		run: &gen.AdminMemoryEvalRun{
			ID:          "run-1",
			Status:      "succeeded",
			Config:      &gen.AdminMemoryEvalRunConfig{AnswerModels: []string{"a/one"}, JudgeModel: "j/judge", SummaryModel: "s/sum", DigestModel: "d/dig", Runs: 1},
			CostUsd:     &cost,
			RequestedBy: "staff@example.com",
			ReportJSON:  &report,
			CreatedAt:   "2026-11-02T10:00:00Z",
		},
	}
}

func TestListMemoryEvalRunsProjectsSummaries(t *testing.T) {
	t.Parallel()
	reads := testMemoryEvalReads()

	status, body, data := callStaffReadTool(t, reads, "list_memory_eval_runs", `{"limit":5,"cursor":"run-9"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, 5, reads.listInput.Limit)
	require.Equal(t, "run-9", *reads.listInput.Cursor)
	require.Nil(t, reads.listInput.AdminSessionToken)

	var out MemoryEvalRunPage
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.Runs, 1)
	require.Equal(t, "s/sum", out.Runs[0].Config.SummaryModel)
	require.Equal(t, "run-0", *out.NextCursor)
	require.NotContains(t, string(data), "planted-1", "list leaves reports out")
}

func TestListMemoryEvalRunsBoundsLimit(t *testing.T) {
	t.Parallel()
	reads := testMemoryEvalReads()

	_, body, _ := callStaffReadTool(t, reads, "list_memory_eval_runs", `{"limit":500}`)
	require.Contains(t, body, "limit must be at most 50")
	require.Nil(t, reads.listInput)
}

func TestGetMemoryEvalRunProjectsScores(t *testing.T) {
	t.Parallel()
	reads := testMemoryEvalReads()

	status, body, data := callStaffReadTool(t, reads, "get_memory_eval_run", `{"run_id":"run-1"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "run-1", reads.getInput.ID)

	var out MemoryEvalRunDetail
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.Scores, 2)
	memory := out.Scores[1]
	require.InDelta(t, 0.8, memory.Accuracy, 1e-9)
	require.InDelta(t, 0.8, memory.ValidCitationRate, 1e-9)
	require.Equal(t, 1, memory.Leaks)
	// Leaked secret names stay out of the tool result.
	require.NotContains(t, string(data), "planted-1")
	require.Zero(t, out.Scores[0].ValidCitationRate)
	require.InDelta(t, 0.8, out.MemoryWriteCostUSD, 1e-9)
	require.Equal(t, 159, out.Summaries)
}

func TestMemoryEvalToolsReportUnavailableReads(t *testing.T) {
	t.Parallel()
	reads := testMemoryEvalReads()
	reads.err = errors.New("database down")

	_, body, _ := callStaffReadTool(t, reads, "get_memory_eval_run", `{"run_id":"run-1"}`)
	require.Contains(t, body, "not found or unavailable")
	require.NotContains(t, body, "database down")
}
