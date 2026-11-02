//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

var errMemoryEvalsUnavailable = errors.New("team memory eval runs are unavailable")

// MemoryEvalReader exposes the dashboard's team memory eval run reads. Runs
// score models against the synthetic corpus in the demo organization; they
// hold no customer data. Starting a run spends credits and stays in the
// dashboard.
type MemoryEvalReader interface {
	ListMemoryEvalRuns(context.Context, *gen.ListMemoryEvalRunsPayload) (*gen.ListMemoryEvalRunsResult, error)
	GetMemoryEvalRun(context.Context, *gen.GetMemoryEvalRunPayload) (*gen.AdminMemoryEvalRun, error)
}

type ListMemoryEvalRunsInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"ID of the last run on the previous page"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Runs per page, 1 to 50; default 20"`
}

type GetMemoryEvalRunInput struct {
	RunID string `json:"run_id" jsonschema:"Exact run ID from list_memory_eval_runs"`
}

// MemoryEvalRunConfig is what a run compared.
type MemoryEvalRunConfig struct {
	AnswerModels []string `json:"answer_models"`
	JudgeModel   string   `json:"judge_model"`
	SummaryModel string   `json:"summary_model"`
	DigestModel  string   `json:"digest_model"`
	Runs         int      `json:"runs"`
}

// MemoryEvalRunSummary is one run without its scores.
type MemoryEvalRunSummary struct {
	ID          string              `json:"id"`
	Status      string              `json:"status"`
	Config      MemoryEvalRunConfig `json:"config"`
	CostUSD     *float64            `json:"cost_usd,omitempty"`
	Error       *string             `json:"error,omitempty"`
	RequestedBy string              `json:"requested_by"`
	CreatedAt   string              `json:"created_at"`
	FinishedAt  *string             `json:"finished_at,omitempty"`
}

type MemoryEvalRunPage struct {
	Runs       []MemoryEvalRunSummary `json:"runs"`
	NextCursor *string                `json:"next_cursor,omitempty"`
}

// MemoryEvalScore is one answer model in one arm.
type MemoryEvalScore struct {
	Model               string  `json:"model"`
	Arm                 string  `json:"arm"`
	Accuracy            float64 `json:"accuracy"`
	ValidCitationRate   float64 `json:"valid_citation_rate,omitempty"`
	Leaks               int     `json:"leaks"`
	IsolationViolations int     `json:"isolation_violations"`
	Errors              int     `json:"errors"`
	CostUSD             float64 `json:"cost_usd"`
	LatencyP95MS        int64   `json:"latency_p95_ms"`
}

// MemoryEvalRunDetail is one run with its per-arm scores and what writing
// team memory cost.
type MemoryEvalRunDetail struct {
	MemoryEvalRunSummary
	Scores             []MemoryEvalScore `json:"scores"`
	Summaries          int               `json:"summaries,omitempty"`
	FailedSummaries    int               `json:"failed_summaries,omitempty"`
	Digests            int               `json:"digests,omitempty"`
	MemoryWriteCostUSD float64           `json:"memory_write_cost_usd,omitempty"`
	JudgeCostUSD       float64           `json:"judge_cost_usd,omitempty"`
}

// memoryEvalReport is the subset of bench.Report the tool projects.
type memoryEvalReport struct {
	JudgeCostUSD float64 `json:"judge_cost_usd"`
	Scores       []struct {
		Model               string   `json:"model"`
		Arm                 string   `json:"arm"`
		HasTools            bool     `json:"has_tools"`
		Accuracy            float64  `json:"accuracy"`
		Answerable          int      `json:"answerable"`
		ValidCited          int      `json:"valid_cited"`
		Leaks               []string `json:"leaks"`
		IsolationViolations []string `json:"isolation_violations"`
		Errors              int      `json:"errors"`
		CostUSD             float64  `json:"cost_usd"`
		LatencyP95MS        int64    `json:"latency_p95_ms"`
	} `json:"scores"`
	Preparation *struct {
		Summaries    int `json:"summaries"`
		Failed       int `json:"failed_summaries"`
		Digests      int `json:"digests"`
		SummarySpend struct {
			CostUSD float64 `json:"cost_usd"`
		} `json:"summary_spend"`
		DigestSpend struct {
			CostUSD float64 `json:"cost_usd"`
		} `json:"digest_spend"`
	} `json:"preparation"`
}

func registerMemoryEvalTools(server *mcp.Server, reader MemoryEvalReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_memory_eval_runs",
		Title:       "List Team Memory Eval Runs",
		Description: "List team memory eval runs, newest first: status, the answer, judge, summary and digest models compared, cost and who started them. Runs score models against the synthetic corpus in the demo organization, never customer data. Use get_memory_eval_run for scores.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListMemoryEvalRunsInput) (*mcp.CallToolResult, MemoryEvalRunPage, error) {
		if reader == nil {
			return nil, MemoryEvalRunPage{}, errMemoryEvalsUnavailable
		}
		limit := input.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > 50 {
			return nil, MemoryEvalRunPage{}, errors.New("limit must be at most 50")
		}
		payload := &gen.ListMemoryEvalRunsPayload{Limit: limit}
		if input.Cursor != "" {
			payload.Cursor = &input.Cursor
		}
		result, err := reader.ListMemoryEvalRuns(ctx, payload)
		if err != nil || result == nil {
			return nil, MemoryEvalRunPage{}, errMemoryEvalsUnavailable
		}
		runs := make([]MemoryEvalRunSummary, 0, len(result.Runs))
		for _, run := range result.Runs {
			runs = append(runs, memoryEvalSummary(run))
		}
		return nil, MemoryEvalRunPage{Runs: runs, NextCursor: result.NextCursor}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_memory_eval_run",
		Title:       "Get Team Memory Eval Scores",
		Description: "Get one team memory eval run's scores per answer model and arm (no memory, search_chats, search_team_memory): accuracy, valid citation rate, leaked planted secrets, isolation violations, errors, cost and p95 latency, plus what writing summaries and digests cost. Any leak or isolation violation disqualifies a model whatever its accuracy.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMemoryEvalRunInput) (*mcp.CallToolResult, MemoryEvalRunDetail, error) {
		if reader == nil {
			return nil, MemoryEvalRunDetail{}, errMemoryEvalsUnavailable
		}
		if input.RunID == "" {
			return nil, MemoryEvalRunDetail{}, errors.New("run_id is required")
		}
		run, err := reader.GetMemoryEvalRun(ctx, &gen.GetMemoryEvalRunPayload{ID: input.RunID})
		if err != nil || run == nil {
			return nil, MemoryEvalRunDetail{}, errors.New("team memory eval run not found or unavailable")
		}
		detail := MemoryEvalRunDetail{MemoryEvalRunSummary: memoryEvalSummary(run), Scores: []MemoryEvalScore{}}
		if run.ReportJSON == nil {
			return nil, detail, nil
		}
		var report memoryEvalReport
		if err := json.Unmarshal([]byte(*run.ReportJSON), &report); err != nil {
			return nil, MemoryEvalRunDetail{}, errors.New("team memory eval report is unreadable")
		}
		for _, s := range report.Scores {
			score := MemoryEvalScore{
				Model: s.Model, Arm: s.Arm, Accuracy: s.Accuracy,
				Leaks: len(s.Leaks), IsolationViolations: len(s.IsolationViolations),
				Errors: s.Errors, CostUSD: s.CostUSD, LatencyP95MS: s.LatencyP95MS,
			}
			if s.HasTools && s.Answerable > 0 {
				score.ValidCitationRate = float64(s.ValidCited) / float64(s.Answerable)
			}
			detail.Scores = append(detail.Scores, score)
		}
		detail.JudgeCostUSD = report.JudgeCostUSD
		if p := report.Preparation; p != nil {
			detail.Summaries = p.Summaries
			detail.FailedSummaries = p.Failed
			detail.Digests = p.Digests
			detail.MemoryWriteCostUSD = p.SummarySpend.CostUSD + p.DigestSpend.CostUSD
		}
		return nil, detail, nil
	})
}

func memoryEvalSummary(run *gen.AdminMemoryEvalRun) MemoryEvalRunSummary {
	summary := MemoryEvalRunSummary{
		ID:          run.ID,
		Status:      run.Status,
		CostUSD:     run.CostUsd,
		Error:       run.Error,
		RequestedBy: run.RequestedBy,
		CreatedAt:   run.CreatedAt,
		FinishedAt:  run.FinishedAt,
	}
	if run.Config != nil {
		summary.Config = MemoryEvalRunConfig{
			AnswerModels: run.Config.AnswerModels,
			JudgeModel:   run.Config.JudgeModel,
			SummaryModel: run.Config.SummaryModel,
			DigestModel:  run.Config.DigestModel,
			Runs:         run.Config.Runs,
		}
	}
	return summary
}
