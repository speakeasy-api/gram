package risk_analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// This file holds the NON-ENFORCING eval ("session replay") write path. It
// deliberately reuses the package's detection scanners (scanPromptPolicy,
// scanStandardPolicyBatch) and finding-grouping (groupFindings) but writes to
// the segregated policy_eval_findings table — never risk_results — and never
// appends to the outbox or marks messages analyzed. That isolation is what
// keeps an eval run from touching enforcement state or the realtime coordinator.
//
// Cost capture: the eval activity sets JudgeInput.Observe to a
// PolicyEvalUsageAccumulator so per-call cost/tokens/latency roll up into the
// run header. The realtime path leaves Observe nil and is unaffected.

// PolicyEvalUsageAccumulator collects judge cost/latency across an eval run. It
// is the sink wired into JudgeInput.Observe: assign acc.Observe directly. Safe
// for concurrent use, since the batch judge fans out up to judgeConcurrency
// calls in parallel.
type PolicyEvalUsageAccumulator struct {
	mu           sync.Mutex
	costUSD      float64
	inputTokens  int
	outputTokens int
	latencies    []time.Duration
	calls        int
	errors       int
	firstErr     error
}

// Observe implements the JudgeInput.Observe callback signature.
func (a *PolicyEvalUsageAccumulator) Observe(u JudgeUsage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if u.Err != nil {
		a.errors++
		if a.firstErr == nil {
			a.firstErr = u.Err
		}
	}
	if u.CostUSD != nil {
		a.costUSD += *u.CostUSD
	}
	a.inputTokens += u.InputTokens
	a.outputTokens += u.OutputTokens
	a.latencies = append(a.latencies, u.Latency)
}

// JudgeHealth reports attempted judge calls, how many errored, and the first
// error seen. Used to fail an eval fast when the judge provider is
// systematically unavailable instead of grinding the whole sample (which leaves
// the run "stuck" in progress until the activity timeout).
func (a *PolicyEvalUsageAccumulator) JudgeHealth() (calls int, errs int, firstErr error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls, a.errors, a.firstErr
}

// PolicyEvalUsageStats is the rolled-up cost/latency for a run, ready to write
// onto the policy_eval_runs header.
type PolicyEvalUsageStats struct {
	TotalCostUSD float64
	InputTokens  int
	OutputTokens int
	LatencyP50MS int
	LatencyP95MS int
}

// Stats summarizes everything observed so far.
func (a *PolicyEvalUsageAccumulator) Stats() PolicyEvalUsageStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return PolicyEvalUsageStats{
		TotalCostUSD: a.costUSD,
		InputTokens:  a.inputTokens,
		OutputTokens: a.outputTokens,
		LatencyP50MS: percentileMS(a.latencies, 0.50),
		LatencyP95MS: percentileMS(a.latencies, 0.95),
	}
}

// percentileMS returns the p-th percentile of the durations in milliseconds
// (nearest-rank), or 0 when there is no data.
func percentileMS(durations []time.Duration, p float64) int {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	slices.Sort(sorted)
	rank := int(p * float64(len(sorted)))
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return int(sorted[rank].Milliseconds())
}

// BuildPolicyEvalFindingRows maps a message's findings to eval-finding insert
// rows. Unlike the enforcement writer (buildRows) it records only real findings
// — no empty-result sentinels and no dead-letter rows — since an eval run does
// not track per-message analyzed state. Overlapping spans for one logical
// finding are grouped via the shared groupFindings so the stored shape matches
// risk_results.
func BuildPolicyEvalFindingRows(runID, projectID uuid.UUID, orgID string, messageID uuid.UUID, findings []Finding) ([]repo.InsertPolicyEvalFindingsParams, int) {
	realFindings := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if f.DeadLetterReason != "" {
			continue
		}
		realFindings = append(realFindings, f)
	}
	if len(realFindings) == 0 {
		return nil, 0
	}

	var rows []repo.InsertPolicyEvalFindingsParams
	count := 0
	for _, grp := range groupFindings(realFindings) {
		f := grp.primary
		count++
		id, _ := uuid.NewV7()
		spansJSON, err := json.Marshal(grp.spans)
		if err != nil {
			spansJSON = nil
		}
		rows = append(rows, repo.InsertPolicyEvalFindingsParams{
			ID:              id,
			PolicyEvalRunID: runID,
			ProjectID:       projectID,
			OrganizationID:  orgID,
			ChatMessageID:   messageID,
			Source:          f.Source,
			RuleID:          pgtype.Text{String: f.RuleID, Valid: f.RuleID != ""},
			Description:     pgtype.Text{String: f.Description, Valid: f.Description != ""},
			Match:           pgtype.Text{String: f.Match, Valid: f.Match != ""},
			StartPos:        pgtype.Int4{Int32: conv.SafeInt32(f.StartPos), Valid: true},
			EndPos:          pgtype.Int4{Int32: conv.SafeInt32(f.EndPos), Valid: true},
			Confidence:      pgtype.Float8{Float64: f.Confidence, Valid: true},
			Tags:            f.Tags,
			Spans:           spansJSON,
		})
	}
	return rows, count
}

// WriteEvalFindings inserts eval findings. It is intentionally a plain batch
// insert: no transaction-coupled outbox append (unlike writeResults) and no
// MarkMessagesAnalyzed, so an eval run produces zero enforcement side effects.
func WriteEvalFindings(ctx context.Context, q *repo.Queries, rows []repo.InsertPolicyEvalFindingsParams) (int64, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	n, err := q.InsertPolicyEvalFindings(ctx, rows)
	if err != nil {
		return 0, fmt.Errorf("insert policy eval findings: %w", err)
	}
	return n, nil
}
