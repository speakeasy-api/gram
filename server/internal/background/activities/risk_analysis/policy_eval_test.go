package risk_analysis

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBuildPolicyEvalFindingRows(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	projectID := uuid.New()
	orgID := "org-1"
	msgID := uuid.New()

	t.Run("empty findings produce no rows", func(t *testing.T) {
		t.Parallel()
		rows, count := BuildPolicyEvalFindingRows(runID, projectID, orgID, msgID, nil)
		require.Nil(t, rows)
		require.Zero(t, count)
	})

	t.Run("dead-letter findings are excluded", func(t *testing.T) {
		t.Parallel()
		rows, count := BuildPolicyEvalFindingRows(runID, projectID, orgID, msgID, []Finding{
			{Source: "gitleaks", RuleID: "secret.aws", DeadLetterReason: "scanner timeout"},
		})
		require.Nil(t, rows)
		require.Zero(t, count)
	})

	t.Run("distinct findings map to one row each", func(t *testing.T) {
		t.Parallel()
		rows, count := BuildPolicyEvalFindingRows(runID, projectID, orgID, msgID, []Finding{
			{Source: "gitleaks", RuleID: "secret.aws", Match: "AKIA…", StartPos: 0, EndPos: 5, Confidence: 0.9, Tags: []string{"secret"}},
			{Source: "presidio", RuleID: "pii.email_address", Match: "a@b.com", StartPos: 10, EndPos: 17, Confidence: 0.8},
		})
		require.Equal(t, 2, count)
		require.Len(t, rows, 2)

		// Every row carries the run/tenant/message identity and source fields.
		for _, r := range rows {
			require.NotEqual(t, uuid.Nil, r.ID)
			require.Equal(t, runID, r.PolicyEvalRunID)
			require.Equal(t, projectID, r.ProjectID)
			require.Equal(t, orgID, r.OrganizationID)
			require.Equal(t, msgID, r.ChatMessageID)
			require.NotEmpty(t, r.Source)
		}
		require.Equal(t, "secret.aws", rows[0].RuleID.String)
		require.Equal(t, "AKIA…", rows[0].Match.String)
		require.InDelta(t, 0.9, rows[0].Confidence.Float64, 0.0001)
		require.Equal(t, []string{"secret"}, rows[0].Tags)
	})

	t.Run("overlapping spans for one logical finding are grouped into a single row", func(t *testing.T) {
		t.Parallel()
		// Same (Source, RuleID, spanGroupKey) collapses to one row with all spans.
		rows, count := BuildPolicyEvalFindingRows(runID, projectID, orgID, msgID, []Finding{
			{Source: "presidio", RuleID: "pii.person", Match: "Jane", StartPos: 0, EndPos: 4, spanGroupKey: "g1"},
			{Source: "presidio", RuleID: "pii.person", Match: "Doe", StartPos: 5, EndPos: 8, spanGroupKey: "g1"},
		})
		require.Equal(t, 1, count)
		require.Len(t, rows, 1)

		var spans []FindingSpan
		require.NoError(t, json.Unmarshal(rows[0].Spans, &spans))
		require.Len(t, spans, 2)
	})

	t.Run("dead-letter mixed with real keeps only the real finding", func(t *testing.T) {
		t.Parallel()
		rows, count := BuildPolicyEvalFindingRows(runID, projectID, orgID, msgID, []Finding{
			{Source: "gitleaks", RuleID: "secret.aws", DeadLetterReason: "boom"},
			{Source: "gitleaks", RuleID: "secret.gcp", Match: "x"},
		})
		require.Equal(t, 1, count)
		require.Len(t, rows, 1)
		require.Equal(t, "secret.gcp", rows[0].RuleID.String)
	})
}

func TestPolicyEvalUsageAccumulator(t *testing.T) {
	t.Parallel()

	t.Run("zero observations yield zero stats", func(t *testing.T) {
		t.Parallel()
		var acc PolicyEvalUsageAccumulator
		stats := acc.Stats()
		require.Zero(t, stats.TotalCostUSD)
		require.Zero(t, stats.InputTokens)
		require.Zero(t, stats.OutputTokens)
		require.Zero(t, stats.LatencyP50MS)
		require.Zero(t, stats.LatencyP95MS)
	})

	t.Run("nil cost does not add to total but tokens accumulate", func(t *testing.T) {
		t.Parallel()
		var acc PolicyEvalUsageAccumulator
		acc.Observe(JudgeUsage{InputTokens: 100, OutputTokens: 20, CostUSD: nil, Latency: 10 * time.Millisecond})
		cost := 0.5
		acc.Observe(JudgeUsage{InputTokens: 50, OutputTokens: 10, CostUSD: &cost, Latency: 20 * time.Millisecond})

		stats := acc.Stats()
		require.InDelta(t, 0.5, stats.TotalCostUSD, 0.0001)
		require.Equal(t, 150, stats.InputTokens)
		require.Equal(t, 30, stats.OutputTokens)
	})

	t.Run("p50/p95 use nearest-rank over observed latencies", func(t *testing.T) {
		t.Parallel()
		var acc PolicyEvalUsageAccumulator
		for _, msVal := range []int{10, 20, 30, 40, 50} {
			acc.Observe(JudgeUsage{Latency: time.Duration(msVal) * time.Millisecond})
		}
		stats := acc.Stats()
		// nearest-rank: p50 -> index int(0.5*5)=2 -> 30ms; p95 -> index int(0.95*5)=4 -> 50ms.
		require.Equal(t, 30, stats.LatencyP50MS)
		require.Equal(t, 50, stats.LatencyP95MS)
	})

	t.Run("observe counts errored calls too", func(t *testing.T) {
		t.Parallel()
		var acc PolicyEvalUsageAccumulator
		cost := 0.25
		// A billable call that errored after the model responded still reports usage.
		acc.Observe(JudgeUsage{InputTokens: 10, OutputTokens: 0, CostUSD: &cost, Latency: 5 * time.Millisecond, Err: errJudge})
		stats := acc.Stats()
		require.InDelta(t, 0.25, stats.TotalCostUSD, 0.0001)
		require.Equal(t, 10, stats.InputTokens)
		require.Equal(t, 5, stats.LatencyP50MS)
	})
}

// errJudge is a sentinel error reused across cases to avoid allocating in the
// table; its value is irrelevant to the accumulator (it counts usage regardless).
var errJudge = sentinelError("judge error")

type sentinelError string

func (e sentinelError) Error() string { return string(e) }
