package risk_analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// evalScanBatchSize bounds how many messages each internal scan batch covers
// inside RunScan. The whole run executes within a single activity that loops
// over these batches, writing findings and heartbeating between them.
const evalScanBatchSize = 100

// evalRunErrorMaxLen caps the failure reason persisted on a run.
const evalRunErrorMaxLen = 2000

// PolicyEval implements the activities behind the policy-eval ("session replay")
// workflow: resolving a run's sample, scanning it non-enforcingly, rolling up
// its stats, and GC of expired runs. It shares the AnalyzeBatch scanner so eval
// findings match what enforcement would produce.
type PolicyEval struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	scanner *AnalyzeBatch
}

func NewPolicyEval(logger *slog.Logger, db *pgxpool.Pool, scanner *AnalyzeBatch) *PolicyEval {
	return &PolicyEval{
		logger:  logger.With(attr.SlogComponent("policy-eval")),
		db:      db,
		scanner: scanner,
	}
}

// PolicyEvalRunRef identifies a run for the per-run activities. ProjectID scopes
// every query to the tenant.
type PolicyEvalRunRef struct {
	RunID     uuid.UUID
	ProjectID uuid.UUID
}

// PolicyEvalScanResult is the rolled-up outcome of scanning a run's sample.
type PolicyEvalScanResult struct {
	MessagesScanned int
	FindingsCount   int
	Stats           PolicyEvalUsageStats
}

// SelectSample resolves the run's sample definition into a pinned list of
// chat_message_ids, persists that list back onto the run, and transitions the
// run to 'running'. Returns the number of pinned messages. Idempotent: a
// re-delivery re-pins the same ids and StartPolicyEvalRun only advances from
// 'pending'.
func (p *PolicyEval) SelectSample(ctx context.Context, ref PolicyEvalRunRef) (int, error) {
	q := repo.New(p.db)

	run, err := q.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: ref.RunID, ProjectID: ref.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("policy eval run %s not found", ref.RunID)
	}
	if err != nil {
		return 0, fmt.Errorf("get policy eval run: %w", err)
	}

	var spec EvalSampleSpec
	if err := json.Unmarshal(run.SampleDefinition, &spec); err != nil {
		return 0, fmt.Errorf("decode sample definition: %w", err)
	}

	ids, err := p.resolveSample(ctx, q, ref.ProjectID, spec)
	if err != nil {
		return 0, err
	}

	spec.MessageIDs = uuidsToStrings(ids)
	pinned, err := json.Marshal(spec)
	if err != nil {
		return 0, fmt.Errorf("encode pinned sample: %w", err)
	}
	if err := q.SetPolicyEvalRunSample(ctx, repo.SetPolicyEvalRunSampleParams{
		SampleDefinition: pinned,
		ID:               ref.RunID,
		ProjectID:        ref.ProjectID,
	}); err != nil {
		return 0, fmt.Errorf("persist pinned sample: %w", err)
	}
	if err := q.StartPolicyEvalRun(ctx, repo.StartPolicyEvalRunParams{ID: ref.RunID, ProjectID: ref.ProjectID}); err != nil {
		return 0, fmt.Errorf("start policy eval run: %w", err)
	}

	p.logger.InfoContext(ctx, "policy eval sample resolved", attr.SlogProjectID(ref.ProjectID.String()))
	return len(ids), nil
}

func (p *PolicyEval) resolveSample(ctx context.Context, q *repo.Queries, projectID uuid.UUID, spec EvalSampleSpec) ([]uuid.UUID, error) {
	if spec.Mode == "manual" {
		ids := make([]uuid.UUID, 0, len(spec.MessageIDs))
		for _, s := range spec.MessageIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				return nil, fmt.Errorf("invalid message id %q in sample: %w", s, err)
			}
			ids = append(ids, id)
		}
		if spec.MaxMessages > 0 && len(ids) > spec.MaxMessages {
			ids = ids[:spec.MaxMessages]
		}
		return ids, nil
	}

	// auto: most recent messages within the window, optionally bounded to the
	// most recent N sessions, capped at max_messages.
	params := repo.SelectPolicyEvalSampleMessagesParams{
		ProjectID:    uuid.NullUUID{UUID: projectID, Valid: true},
		FromTime:     pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		SessionLimit: pgtype.Int8{Int64: 0, Valid: false},
		MaxMessages:  conv.SafeInt32(spec.MaxMessages),
	}
	if spec.LookbackDays > 0 {
		from := time.Now().UTC().AddDate(0, 0, -spec.LookbackDays)
		params.FromTime = pgtype.Timestamptz{Time: from, InfinityModifier: pgtype.Finite, Valid: true}
	}
	if spec.LastNSessions > 0 {
		params.SessionLimit = pgtype.Int8{Int64: int64(spec.LastNSessions), Valid: true}
	}
	ids, err := q.SelectPolicyEvalSampleMessages(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("select eval sample messages: %w", err)
	}
	return ids, nil
}

// evalScanConfig is the resolved policy configuration a run scans against,
// derived from either a saved policy row or a frozen draft snapshot.
type evalScanConfig struct {
	policy           repo.RiskPolicy
	sources          []string
	presidioEntities []string
	customRuleIDs    []string
	messageTypes     []string
	policyID         uuid.UUID
	policyVersion    int64
}

func (p *PolicyEval) resolvePolicyConfig(ctx context.Context, q *repo.Queries, run repo.PolicyEvalRun) (evalScanConfig, error) {
	if run.RiskPolicyID.Valid {
		row, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: run.RiskPolicyID.UUID, ProjectID: run.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			return evalScanConfig{}, fmt.Errorf("policy %s under eval no longer exists", run.RiskPolicyID.UUID)
		}
		if err != nil {
			return evalScanConfig{}, fmt.Errorf("get policy under eval: %w", err)
		}
		return evalScanConfig{
			policy:           row,
			sources:          row.Sources,
			presidioEntities: row.PresidioEntities,
			customRuleIDs:    row.CustomRuleIds,
			messageTypes:     row.MessageTypes,
			policyID:         row.ID,
			policyVersion:    row.Version,
		}, nil
	}

	var snap EvalConfigSnapshot
	if err := json.Unmarshal(run.ConfigSnapshot, &snap); err != nil {
		return evalScanConfig{}, fmt.Errorf("decode candidate config snapshot: %w", err)
	}
	return evalScanConfig{
		policy:           SyntheticPolicyForSnapshot(run.ProjectID, snap),
		sources:          snap.Sources,
		presidioEntities: snap.PresidioEntities,
		customRuleIDs:    snap.CustomRuleIDs,
		messageTypes:     snap.MessageTypes,
		policyID:         uuid.Nil,
		policyVersion:    0,
	}, nil
}

// RunScan scans the run's pinned sample non-enforcingly: it writes findings to
// policy_eval_findings and accumulates judge cost/latency, but never touches
// risk_results, the outbox, or chat_messages.risk_analyzed_at. Returns the
// rolled-up stats for the run header.
func (p *PolicyEval) RunScan(ctx context.Context, ref PolicyEvalRunRef) (*PolicyEvalScanResult, error) {
	q := repo.New(p.db)

	run, err := q.GetPolicyEvalRun(ctx, repo.GetPolicyEvalRunParams{ID: ref.RunID, ProjectID: ref.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("policy eval run %s not found", ref.RunID)
	}
	if err != nil {
		return nil, fmt.Errorf("get policy eval run: %w", err)
	}

	cfg, err := p.resolvePolicyConfig(ctx, q, run)
	if err != nil {
		return nil, err
	}

	var spec EvalSampleSpec
	if err := json.Unmarshal(run.SampleDefinition, &spec); err != nil {
		return nil, fmt.Errorf("decode pinned sample: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(spec.MessageIDs))
	for _, s := range spec.MessageIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid pinned message id %q: %w", s, err)
		}
		ids = append(ids, id)
	}

	// var (not a composite literal) so exhaustruct doesn't require spelling out
	// the accumulator's unexported zero-value fields.
	var acc PolicyEvalUsageAccumulator
	var result PolicyEvalScanResult

	for start := 0; start < len(ids); start += evalScanBatchSize {
		end := min(start+evalScanBatchSize, len(ids))
		batch := ids[start:end]

		args := AnalyzeBatchArgs{
			ProjectID:        ref.ProjectID,
			OrganizationID:   run.OrganizationID,
			RiskPolicyID:     cfg.policyID,
			PolicyVersion:    cfg.policyVersion,
			MessageIDs:       batch,
			Sources:          cfg.sources,
			MessageTypes:     cfg.messageTypes,
			PresidioEntities: cfg.presidioEntities,
			CustomRuleIds:    cfg.customRuleIDs,
			JudgeObserve:     acc.Observe,
		}

		scanned, err := p.scanner.ScanBatchForEval(ctx, args, cfg.policy)
		if err != nil {
			return nil, fmt.Errorf("scan eval batch: %w", err)
		}

		var rows []repo.InsertPolicyEvalFindingsParams
		for _, mf := range scanned {
			result.MessagesScanned++
			batchRows, count := BuildPolicyEvalFindingRows(ref.RunID, ref.ProjectID, run.OrganizationID, mf.MessageID, mf.Findings)
			result.FindingsCount += count
			rows = append(rows, batchRows...)
		}
		if _, err := WriteEvalFindings(ctx, q, rows); err != nil {
			return nil, fmt.Errorf("write eval findings: %w", err)
		}

		activity.RecordHeartbeat(ctx, end)
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("eval scan canceled: %w", err)
		}
	}

	result.Stats = acc.Stats()
	return &result, nil
}

// CompleteRun writes a run's rolled-up statistics and marks it completed. Only
// advances from 'running', so it is a no-op if the run was cancelled meanwhile.
func (p *PolicyEval) CompleteRun(ctx context.Context, ref PolicyEvalRunRef, res PolicyEvalScanResult) error {
	if err := repo.New(p.db).CompletePolicyEvalRun(ctx, repo.CompletePolicyEvalRunParams{
		MessagesScanned:   conv.SafeInt32(res.MessagesScanned),
		FindingsCount:     conv.SafeInt32(res.FindingsCount),
		TotalCostUsd:      res.Stats.TotalCostUSD,
		InputTokens:       int64(res.Stats.InputTokens),
		OutputTokens:      int64(res.Stats.OutputTokens),
		JudgeLatencyP50Ms: pgtype.Int4{Int32: conv.SafeInt32(res.Stats.LatencyP50MS), Valid: true},
		JudgeLatencyP95Ms: pgtype.Int4{Int32: conv.SafeInt32(res.Stats.LatencyP95MS), Valid: true},
		ID:                ref.RunID,
		ProjectID:         ref.ProjectID,
	}); err != nil {
		return fmt.Errorf("complete policy eval run: %w", err)
	}
	return nil
}

// FailRun records a failure reason and marks the run failed. Only advances from
// pending/running, so a cancelled run is left as-is.
func (p *PolicyEval) FailRun(ctx context.Context, ref PolicyEvalRunRef, reason string) error {
	if len(reason) > evalRunErrorMaxLen {
		reason = reason[:evalRunErrorMaxLen]
	}
	if err := repo.New(p.db).FailPolicyEvalRun(ctx, repo.FailPolicyEvalRunParams{
		Error:     pgtype.Text{String: reason, Valid: reason != ""},
		ID:        ref.RunID,
		ProjectID: ref.ProjectID,
	}); err != nil {
		return fmt.Errorf("fail policy eval run: %w", err)
	}
	return nil
}

// DeleteExpiredRuns drops one batch of past-retention runs (cascading to their
// findings). Returns the number deleted so the GC workflow can loop until a
// batch comes back short.
func (p *PolicyEval) DeleteExpiredRuns(ctx context.Context, batchSize int32) (int64, error) {
	n, err := repo.New(p.db).DeleteExpiredPolicyEvalRuns(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete expired policy eval runs: %w", err)
	}
	return n, nil
}

func uuidsToStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
