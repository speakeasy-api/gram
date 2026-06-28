// Package risk_exclusion contains the Temporal activity that reconciles a risk
// exclusion against already-stored findings (risk_results). It is the
// retroactive half of exclusions: the going-forward half lives in the analysis
// scanner (risk_analysis.ExclusionSet). The activity is idempotent — it always
// reverses the exclusion's prior flags, then (when the exclusion is enabled and
// not deleted) re-applies them — so it is safe to retry and correctly handles
// create, update (predicate change), delete, enable, and disable.
package risk_exclusion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	// reconcileBatchLimit bounds each UPDATE so locks/WAL stay small and the
	// activity can heartbeat between batches.
	reconcileBatchLimit int32 = 5000
	// perBatchTimeout bounds a single batch — chiefly the regex apply, whose
	// Postgres `~` engine can be pathological on crafted patterns. A timeout
	// cancels the query (pgx propagates cancellation); the activity fails and
	// Temporal retries from the current keyset cursor.
	perBatchTimeout = 30 * time.Second
)

// ReconcileArgs identifies the exclusion to reconcile.
type ReconcileArgs struct {
	ProjectID   uuid.UUID
	ExclusionID uuid.UUID
}

// Reconcile flags/unflags risk_results rows to match an exclusion's current state.
type Reconcile struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
}

func NewReconcile(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *Reconcile {
	return &Reconcile{
		logger: logger.With(attr.SlogComponent("risk-exclusion-reconcile")),
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion"),
		db:     db,
	}
}

func (a *Reconcile) Do(ctx context.Context, args ReconcileArgs) (err error) {
	ctx, span := a.tracer.Start(ctx, "risk.reconcileExclusion")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	q := repo.New(a.db)
	exclusionID := uuid.NullUUID{UUID: args.ExclusionID, Valid: true}

	// Resume progress from the last heartbeat so a retried attempt does not
	// re-walk batches it already completed. Phase distinguishes the two keyset
	// loops (which use independent cursors) so we never start the reverse loop
	// partway through and skip rows it must clear.
	var resume reconcileProgress
	if activity.HasHeartbeatDetails(ctx) {
		if err := activity.GetHeartbeatDetails(ctx, &resume); err != nil {
			a.logger.WarnContext(ctx, "failed to read reconcile heartbeat details", attr.SlogError(err))
			resume = reconcileProgress{Phase: "", Cursor: uuid.UUID{}}
		}
	}

	// 1. Reverse: clear any flags this exclusion previously set. Skipped if a
	// prior attempt already advanced into the apply phase.
	if resume.Phase != phaseApply {
		if err := a.batchLoop(ctx, phaseReverse, resume.Cursor, func(bctx context.Context, cursor uuid.UUID) ([]uuid.UUID, error) {
			return q.ReverseExclusionFlagsBatch(bctx, repo.ReverseExclusionFlagsBatchParams{
				ExclusionID: exclusionID,
				Cursor:      cursor,
				BatchLimit:  reconcileBatchLimit,
			})
		}); err != nil {
			return fmt.Errorf("reverse exclusion flags: %w", err)
		}
	}

	// 2. Load current state. If gone, deleted, or disabled, reversal was enough.
	// Scoped by the project from args; applies use the row's own project_id so a
	// bad project argument can never touch another tenant's findings.
	ex, err := q.GetRiskExclusionForReconcile(ctx, repo.GetRiskExclusionForReconcileParams{
		ID:        args.ExclusionID,
		ProjectID: args.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load exclusion: %w", err)
	}
	if ex.Deleted || !ex.Enabled {
		return nil
	}

	// 3. Apply: flag findings matching the current predicate.
	projectID := ex.ProjectID
	policyID := ex.RiskPolicyID
	matchValue := pgtype.Text{String: ex.MatchValue, Valid: true}

	apply := func(bctx context.Context, cursor uuid.UUID) ([]uuid.UUID, error) {
		switch ex.MatchType {
		case "exact":
			return q.ApplyExactExclusionBatch(bctx, repo.ApplyExactExclusionBatchParams{
				ExclusionID: exclusionID, ProjectID: projectID, PolicyID: policyID,
				MatchValue: matchValue, RuleIDFilter: ex.RuleIDFilter, SourceFilter: ex.SourceFilter,
				Cursor: cursor, BatchLimit: reconcileBatchLimit,
			})
		case "regex":
			return q.ApplyRegexExclusionBatch(bctx, repo.ApplyRegexExclusionBatchParams{
				ExclusionID: exclusionID, ProjectID: projectID, PolicyID: policyID,
				Pattern: matchValue, RuleIDFilter: ex.RuleIDFilter, SourceFilter: ex.SourceFilter,
				Cursor: cursor, BatchLimit: reconcileBatchLimit,
			})
		case "rule_id":
			return q.ApplyRuleIDExclusionBatch(bctx, repo.ApplyRuleIDExclusionBatchParams{
				ExclusionID: exclusionID, ProjectID: projectID, PolicyID: policyID,
				MatchValue: matchValue, SourceFilter: ex.SourceFilter,
				Cursor: cursor, BatchLimit: reconcileBatchLimit,
			})
		case "source":
			return q.ApplySourceExclusionBatch(bctx, repo.ApplySourceExclusionBatchParams{
				ExclusionID: exclusionID, ProjectID: projectID, PolicyID: policyID,
				MatchValue: ex.MatchValue, RuleIDFilter: ex.RuleIDFilter,
				Cursor: cursor, BatchLimit: reconcileBatchLimit,
			})
		case "entity_type":
			// Presidio entities map to rule_id "pii.<entity>".
			return q.ApplyRuleIDExclusionBatch(bctx, repo.ApplyRuleIDExclusionBatchParams{
				ExclusionID: exclusionID, ProjectID: projectID, PolicyID: policyID,
				MatchValue:   pgtype.Text{String: "pii." + strings.ToLower(ex.MatchValue), Valid: true},
				SourceFilter: ex.SourceFilter,
				Cursor:       cursor, BatchLimit: reconcileBatchLimit,
			})
		default:
			return nil, fmt.Errorf("unknown match_type %q", ex.MatchType)
		}
	}
	applyStart := uuid.UUID{}
	if resume.Phase == phaseApply {
		applyStart = resume.Cursor
	}
	if err := a.batchLoop(ctx, phaseApply, applyStart, apply); err != nil {
		return fmt.Errorf("apply exclusion (%s): %w", ex.MatchType, err)
	}

	return nil
}

// reconcile phases, recorded in heartbeat details so a retry can resume the
// correct keyset loop at the correct cursor.
const (
	phaseReverse = "reverse"
	phaseApply   = "apply"
)

// reconcileProgress is the heartbeat payload: which phase was in flight and the
// keyset cursor reached within it.
type reconcileProgress struct {
	Phase  string
	Cursor uuid.UUID
}

// batchLoop runs a keyset-paginated batch fn until it returns a short batch,
// advancing the cursor to the max id seen and heartbeating (phase + cursor)
// between batches so a retried attempt can resume from the last cursor.
func (a *Reconcile) batchLoop(ctx context.Context, phase string, cursor uuid.UUID, fn func(ctx context.Context, cursor uuid.UUID) ([]uuid.UUID, error)) error {
	for {
		batchCtx, cancel := context.WithTimeout(ctx, perBatchTimeout)
		ids, err := fn(batchCtx, cursor)
		cancel()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if id.String() > cursor.String() {
				cursor = id
			}
		}
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phase, Cursor: cursor})
		if len(ids) < int(reconcileBatchLimit) {
			return nil
		}
	}
}
