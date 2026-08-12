// Package risk_exclusion contains the Temporal activity that reconciles a risk
// exclusion against already-stored findings. It is the retroactive half of
// exclusions: the going-forward half lives in the analysis scanner
// (risk_analysis.ExclusionSet) and the ClickHouse ingest writer
// (risk.FindingCHWriter). The activity is idempotent — it reverses the flags
// the exclusion no longer justifies, then (when the exclusion is enabled and
// not deleted) applies the current predicate — so it is safe to retry and
// correctly handles create, update (predicate change), delete, enable, and
// disable. For an active exclusion the ClickHouse reversal is keep-guarded:
// it only un-flags rows that provably no longer match, so a row that was
// correctly hidden can never be exposed by a reconcile whose apply cannot
// evaluate it (missing pepper keyring, unreconstructable plaintext).
//
// Two stores are reconciled: Postgres risk_results (flag UPDATEs, the
// original mechanism, kept until that table is decommissioned) and ClickHouse
// risk_findings (append-only: flag changes are propagated by appending a copy
// of each affected row's latest version with the exclusion flags replaced —
// see chrepo/retro_exclusion.go).
package risk_exclusion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/assets/blobio"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	// reconcileBatchLimit bounds each UPDATE so locks/WAL stay small and the
	// activity can heartbeat between batches.
	reconcileBatchLimit int32 = 5000
	// perBatchTimeout bounds a single batch or ClickHouse statement — chiefly
	// the Postgres regex apply, whose `~` engine can be pathological on
	// crafted patterns. A timeout cancels the query; the activity fails and
	// Temporal retries from the current cursor.
	perBatchTimeout = 30 * time.Second
	// chRetentionDays is how far back the ClickHouse phases sweep — matching
	// the risk_findings table TTL, beyond which no live rows exist.
	chRetentionDays = 90
	// regexAppendChunk bounds one AppendRetroExclusion*ByIDs statement.
	regexAppendChunk = 1000
	// regexCandidatePage bounds one keyset page of regex candidates so a busy
	// day never materializes in worker memory at once.
	regexCandidatePage = 5000
)

// ReconcileArgs identifies the exclusion to reconcile.
type ReconcileArgs struct {
	// ProjectID scopes the exclusion lookup; the applies use each row's own
	// project so a bad argument can never touch another tenant's findings.
	ProjectID uuid.UUID

	// ExclusionID is the exclusion whose current state the stores are
	// reconciled against.
	ExclusionID uuid.UUID

	// WindowDays bounds the ClickHouse phases to that many recent partition
	// days (1 = today only); zero sweeps the full retention window. Used by
	// the workflow's delayed second sweep, whose racing rows are always
	// freshly ingested. A windowed run also skips the Postgres phases
	// entirely: the race the second sweep exists for is the ClickHouse ingest
	// writer's cached exclusion set, while the Postgres scanner loads
	// exclusions fresh per batch — re-walking risk_results would double the
	// UPDATE churn (and transiently un-hide findings between the reverse and
	// apply loops) for nothing.
	WindowDays int
}

// Reconcile flags/unflags stored findings to match an exclusion's current
// state, in Postgres and ClickHouse.
type Reconcile struct {
	logger *slog.Logger
	tracer trace.Tracer
	db     *pgxpool.Pool
	// ch is nil when the worker has no ClickHouse connection; the ClickHouse
	// phases are then skipped with a loud log (the convention for CH-less
	// workers — the Postgres phases still run).
	ch *chrepo.Queries
	// fingerprinter matches exact-value exclusions against the tenant
	// fingerprints stored on ClickHouse rows. A zero value disables the
	// ClickHouse exact-match apply (loud log) — Postgres still covers it.
	fingerprinter risk.Fingerprinter
	// assetStorage feeds regex plaintext reconstruction for findings anchored
	// to content-part assets; nil narrows regex coverage to message-anchored
	// findings.
	assetStorage blobio.Reader

	chRows    metric.Int64Counter
	chSkipped metric.Int64Counter
}

func NewReconcile(logger *slog.Logger, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, db *pgxpool.Pool, ch *chrepo.Queries, fingerprinter risk.Fingerprinter, assetStorage blobio.Reader) *Reconcile {
	logger = logger.With(attr.SlogComponent("risk-exclusion-reconcile"))
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion")

	chRows, err := meter.Int64Counter(
		"gram.risk_exclusions.reconcile_ch_rows",
		metric.WithDescription("Number of ClickHouse risk finding rows whose exclusion flags were rewritten by the retroactive reconcile"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName("gram.risk_exclusions.reconcile_ch_rows"), attr.SlogError(err))
	}
	chSkipped, err := meter.Int64Counter(
		"gram.risk_exclusions.reconcile_ch_skipped",
		metric.WithDescription("Number of retroactive reconciles that skipped a ClickHouse phase"),
		metric.WithUnit("{reconcile}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName("gram.risk_exclusions.reconcile_ch_skipped"), attr.SlogError(err))
	}

	return &Reconcile{
		logger:        logger,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion"),
		db:            db,
		ch:            ch,
		fingerprinter: fingerprinter,
		assetStorage:  assetStorage,
		chRows:        chRows,
		chSkipped:     chSkipped,
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
	// re-walk work it already completed. Phase distinguishes the loops (which
	// use independent cursors) so we never start a later loop partway through
	// and skip rows an earlier one must handle.
	var resume reconcileProgress
	if activity.HasHeartbeatDetails(ctx) {
		if err := activity.GetHeartbeatDetails(ctx, &resume); err != nil {
			a.logger.WarnContext(ctx, "failed to read reconcile heartbeat details", attr.SlogError(err))
			resume = reconcileProgress{Phase: "", Cursor: uuid.UUID{}, Day: ""}
		}
	}

	// A windowed run (the workflow's delayed second sweep) is ClickHouse-only
	// — see the WindowDays doc.
	pgPhases := args.WindowDays == 0

	// 1. Postgres reverse: clear any flags this exclusion previously set.
	// Skipped if a prior attempt already advanced past it.
	if pgPhases && !phaseDone(resume.Phase, phaseReverse) {
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

	// 2. Load current state. Scoped by the project from args; applies use the
	// row's own project_id so a bad project argument can never touch another
	// tenant's findings. A hard-deleted row (project cascade — the API only
	// soft-deletes) leaves no tenant scope for the ClickHouse reverse; those
	// findings age out with the project's data.
	ex, err := q.GetRiskExclusionForReconcile(ctx, repo.GetRiskExclusionForReconcileParams{
		ID:        args.ExclusionID,
		ProjectID: args.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		a.logger.WarnContext(ctx, "exclusion row gone; skipping clickhouse reverse", attr.SlogRiskExclusionID(args.ExclusionID.String()))
		return nil
	}
	if err != nil {
		return fmt.Errorf("load exclusion: %w", err)
	}
	active := !ex.Deleted && ex.Enabled

	// 3. Postgres apply: flag findings matching the current predicate.
	if pgPhases && active && !phaseDone(resume.Phase, phaseApply) {
		applyStart := uuid.UUID{}
		if resume.Phase == phaseApply {
			applyStart = resume.Cursor
		}
		if err := a.batchLoop(ctx, phaseApply, applyStart, a.pgApplyBatchFn(q, ex, exclusionID)); err != nil {
			return fmt.Errorf("apply exclusion (%s): %w", ex.MatchType, err)
		}
	}

	// 4 + 5. ClickHouse reverse, then apply. Append-only copies of each
	// affected row's latest version; statements are idempotent set operations
	// (the predicates select only rows whose latest state differs), so resume
	// is at day granularity with no intra-day cursor.
	if a.ch == nil {
		a.logger.WarnContext(ctx, "clickhouse unavailable; retroactive exclusion reconcile skipped for clickhouse",
			attr.SlogRiskExclusionID(args.ExclusionID.String()))
		a.addSkip(ctx, "no_clickhouse")
		return nil
	}

	run := chRun{
		exclusionID:    args.ExclusionID,
		organizationID: ex.OrganizationID,
		projectID:      ex.ProjectID.String(),
		excludedAt:     chrepo.FormatCHTime(time.Now().UTC()),
		lastInsertedAt: time.Time{},
		windowDays:     args.WindowDays,
	}

	// Decide the apply strategy BEFORE touching any row: an active exclusion
	// whose apply cannot run must not be reversed either — reversing first
	// would strip flags the apply can never restore, permanently un-hiding
	// findings that were correctly excluded at ingest or scan time. The
	// reversal of an active exclusion is likewise keep-guarded so it only
	// un-flags rows that provably no longer match (see RetroReversalKeep).
	var (
		applyPredicate chrepo.RetroExclusionPredicate
		applyRegex     *regexp.Regexp
	)
	if active {
		if ex.MatchType == "regex" {
			re, rerr := regexp.Compile(ex.MatchValue)
			if rerr != nil {
				// Create/update validate patterns, so this is legacy bad data:
				// skip both ClickHouse phases loudly, leaving flags as they
				// are — matching the scan-time ExclusionSet's skip contract.
				a.logger.WarnContext(ctx, "invalid regex exclusion pattern; not propagated to clickhouse", attr.SlogError(rerr), attr.SlogRiskExclusionID(ex.ID.String()))
				a.addSkip(ctx, "invalid_regex")
				return nil
			}
			applyRegex = re
		} else {
			p, ok := a.chPredicate(ctx, ex)
			if !ok {
				// Logged and metriced inside chPredicate. Skipping the
				// reversal too keeps every held row hidden.
				return nil
			}
			applyPredicate = p
		}
	}

	if !phaseDone(resume.Phase, phaseCHReverse) {
		startDay := ""
		if resume.Phase == phaseCHReverse {
			startDay = resume.Day
		}
		var rerr error
		switch {
		case !active:
			// Blanket reversal: a disabled or deleted exclusion holds nothing.
			rerr = a.chReverse(ctx, &run, chrepo.BlanketReversal(), startDay)
		case applyRegex != nil:
			rerr = a.chReverseRegex(ctx, &run, ex, applyRegex, startDay)
		default:
			keep, kerr := applyPredicate.KeepMatching()
			if kerr != nil {
				return fmt.Errorf("clickhouse reverse exclusion flags: %w", kerr)
			}
			rerr = a.chReverse(ctx, &run, keep, startDay)
		}
		if rerr != nil {
			return fmt.Errorf("clickhouse reverse exclusion flags: %w", rerr)
		}
	}

	if active {
		startDay := ""
		if resume.Phase == phaseCHApply {
			startDay = resume.Day
		}
		var aerr error
		if applyRegex != nil {
			aerr = a.chApplyRegex(ctx, &run, ex, applyRegex, startDay)
		} else {
			aerr = a.chApply(ctx, &run, ex, applyPredicate, startDay)
		}
		if aerr != nil {
			return fmt.Errorf("clickhouse apply exclusion (%s): %w", ex.MatchType, aerr)
		}
	}

	return nil
}

// pgApplyBatchFn dispatches the Postgres apply on the exclusion's match type.
func (a *Reconcile) pgApplyBatchFn(q *repo.Queries, ex repo.RiskExclusion, exclusionID uuid.NullUUID) func(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	projectID := ex.ProjectID
	policyID := ex.RiskPolicyID
	matchValue := pgtype.Text{String: ex.MatchValue, Valid: true}

	return func(bctx context.Context, cursor uuid.UUID) ([]uuid.UUID, error) {
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
}

// chRun carries the per-run constants of the ClickHouse phases.
type chRun struct {
	exclusionID    uuid.UUID
	organizationID string
	projectID      string
	excludedAt     string
	lastInsertedAt time.Time
	// windowDays bounds the swept partition days; zero means full retention.
	windowDays int
}

// nextInsertedAt stamps a statement's copies with the current wall clock,
// nudged to stay strictly increasing across the run's statements — so a
// reverse copy and a later re-flag copy of the same id order
// deterministically under the read paths' latest-by-inserted_at dedup, and so
// copies written late in a long multi-day sweep still beat rows a concurrent
// writer (a Pub/Sub redelivery, another exclusion's reconcile) inserted after
// the sweep started.
func (r *chRun) nextInsertedAt() string {
	now := time.Now().UTC()
	if !now.After(r.lastInsertedAt) {
		now = r.lastInsertedAt.Add(time.Microsecond)
	}
	r.lastInsertedAt = now
	return chrepo.FormatCHTime(now)
}

// days yields the partition days to sweep, newest first, capped by the run's
// window (zero = the table's whole retention window). startDay ("" = from the
// top) resumes a partially completed sweep at that day. A resume day that
// fell out of the recomputed window — the attempt crossed UTC midnight, or
// the window shrank — re-runs the whole window: statements are idempotent,
// and completing the phase over an empty slice would silently reconcile
// nothing. (A resume also skips any NEW today partition that appeared after
// the heartbeat; rows there were annotated at ingest and the workflow's
// windowed second sweep re-covers the most recent days.)
func (r *chRun) days(startDay string) []time.Time {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	oldest := chRetentionDays
	if r.windowDays > 0 {
		oldest = min(r.windowDays-1, chRetentionDays)
	}
	days := make([]time.Time, 0, oldest+1)
	for i := 0; i <= oldest; i++ {
		days = append(days, today.AddDate(0, 0, -i))
	}
	if startDay == "" {
		return days
	}
	for i, day := range days {
		if day.Format(time.DateOnly) == startDay {
			return days[i:]
		}
	}
	return days
}

func (r *chRun) scope(day time.Time) chrepo.RetroExclusionScope {
	return chrepo.RetroExclusionScope{
		OrganizationID: r.organizationID,
		ProjectID:      r.projectID,
		DayStart:       day,
		DayEnd:         day.AddDate(0, 0, 1),
	}
}

// chReverse appends un-flag copies for every latest row held by the
// exclusion and not retained by the keep guard, one partition day at a time.
func (a *Reconcile) chReverse(ctx context.Context, run *chRun, keep chrepo.RetroReversalKeep, startDay string) error {
	var total uint64
	for _, day := range run.days(startDay) {
		bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
		count, err := a.ch.CountRetroExclusionReversal(bctx, run.scope(day), run.exclusionID, keep)
		if err == nil && count > 0 {
			err = a.ch.AppendRetroExclusionReversal(bctx, run.scope(day), run.exclusionID, run.nextInsertedAt(), keep)
		}
		cancel()
		if err != nil {
			return fmt.Errorf("day %s: %w", day.Format(time.DateOnly), err)
		}
		total += count
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHReverse, Cursor: uuid.UUID{}, Day: day.Format(time.DateOnly)})
	}
	if total > 0 {
		a.chRows.Add(ctx, int64(total), metric.WithAttributes(attribute.String("phase", "reverse")))
	}
	a.logger.InfoContext(ctx, "clickhouse exclusion reverse complete",
		attr.SlogRiskExclusionID(run.exclusionID.String()),
		attr.SlogRiskReconcileRowCount(int(total)),
	)
	return nil
}

// chApply flags every latest row matching the exclusion's predicate.
// rule_id/entity_type/source evaluate natively in ClickHouse; exact matches
// on recomputed tenant fingerprints.
func (a *Reconcile) chApply(ctx context.Context, run *chRun, ex repo.RiskExclusion, predicate chrepo.RetroExclusionPredicate, startDay string) error {
	var total uint64
	for _, day := range run.days(startDay) {
		bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
		count, err := a.ch.CountRetroExclusionApply(bctx, run.scope(day), predicate)
		if err == nil && count > 0 {
			err = a.ch.AppendRetroExclusionApply(bctx, run.scope(day), run.exclusionID, run.excludedAt, run.nextInsertedAt(), predicate)
		}
		cancel()
		if err != nil {
			return fmt.Errorf("day %s: %w", day.Format(time.DateOnly), err)
		}
		total += count
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHApply, Cursor: uuid.UUID{}, Day: day.Format(time.DateOnly)})
	}
	if total > 0 {
		a.chRows.Add(ctx, int64(total), metric.WithAttributes(attribute.String("phase", "apply")))
	}
	a.logger.InfoContext(ctx, "clickhouse exclusion apply complete",
		attr.SlogRiskExclusionID(run.exclusionID.String()),
		attr.SlogRiskExclusionMatchType(ex.MatchType),
		attr.SlogRiskReconcileRowCount(int(total)),
	)
	return nil
}

// chPredicate translates the exclusion into the ClickHouse-evaluable
// predicate. The second return is false when the translation is impossible
// (exact matching without a pepper keyring) — a loud, metriced skip rather
// than an error, since the Postgres phases still cover the store of record.
func (a *Reconcile) chPredicate(ctx context.Context, ex repo.RiskExclusion) (chrepo.RetroExclusionPredicate, bool) {
	p := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       ex.RuleIDFilter.String,
		SourceFilter:       ex.SourceFilter.String,
	}
	if ex.RiskPolicyID.Valid {
		p.PolicyID = ex.RiskPolicyID.UUID.String()
	}

	switch ex.MatchType {
	case "rule_id":
		p.RuleID = ex.MatchValue
	case "entity_type":
		p.RuleID = "pii." + strings.ToLower(ex.MatchValue)
	case "source":
		p.Source = ex.MatchValue
	case "exact":
		versions := a.fingerprinter.Versions()
		if len(versions) == 0 {
			a.logger.WarnContext(ctx, "fingerprint pepper keyring not configured; exact-match exclusion not propagated to clickhouse",
				attr.SlogRiskExclusionID(ex.ID.String()))
			a.addSkip(ctx, "no_fingerprinter")
			return p, false
		}
		for _, version := range versions {
			fp, err := a.fingerprinter.TenantedHS256WithVersion(version, ex.OrganizationID, []byte(ex.MatchValue))
			if err != nil {
				a.logger.WarnContext(ctx, "compute tenant fingerprint for exclusion", attr.SlogError(err), attr.SlogRiskExclusionID(ex.ID.String()))
				continue
			}
			p.TenantFingerprints = append(p.TenantFingerprints, risk.EncodeFingerprint(fp))
		}
		if len(p.TenantFingerprints) == 0 {
			a.addSkip(ctx, "no_fingerprinter")
			return p, false
		}
	default:
		a.logger.WarnContext(ctx, "unknown match_type for clickhouse exclusion apply", attr.SlogRiskExclusionMatchType(ex.MatchType))
		return p, false
	}

	return p, true
}

// regexPredicate is the SQL-provable scope of a regex exclusion — policy
// binding and filters; the matcher itself only evaluates in Go.
func regexPredicate(ex repo.RiskExclusion) chrepo.RetroExclusionPredicate {
	p := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       ex.RuleIDFilter.String,
		SourceFilter:       ex.SourceFilter.String,
	}
	if ex.RiskPolicyID.Valid {
		p.PolicyID = ex.RiskPolicyID.UUID.String()
	}
	return p
}

// forEachRegexCandidate pages through one day's regex candidates, reconstructs
// each row's plaintext from the chat store, and reports every candidate to fn
// with its reconstruction (ok=false when the plaintext could not be rebuilt or
// the row's chat attribution diverges). The listing is keyset-paginated so a
// busy day never materializes in memory at once.
func (a *Reconcile) forEachRegexCandidate(
	ctx context.Context,
	run *chRun,
	reveal *risk.RevealMatcher,
	projectID uuid.UUID,
	phase, dayKey string,
	list func(ctx context.Context, afterID uuid.UUID, limit int) ([]chrepo.RetroRegexCandidate, error),
	fn func(c chrepo.RetroRegexCandidate, match string, ok bool),
) error {
	// Anchors repeat across findings (several findings per message), so
	// cache the loaded-and-hydrated anchor per anchor id. The cache is
	// per-day: one scan's findings all share its created_at day, so
	// carrying hydrated content — up to 20 MiB per content-part asset —
	// across a 90-day sweep would grow without bound to buy back almost
	// no hits.
	anchors := make(map[string]risk.RevealAnchor)

	afterID := uuid.Nil
	for {
		bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
		candidates, err := list(bctx, afterID, regexCandidatePage)
		cancel()
		if err != nil {
			return fmt.Errorf("list candidates: %w", err)
		}
		if len(candidates) == 0 {
			return nil
		}
		afterID = candidates[len(candidates)-1].ID

		for _, c := range candidates {
			// Each candidate can cost a Postgres anchor load and an asset read,
			// so a day's worth of them easily outlives the activity's heartbeat
			// timeout. The SDK throttles the actual heartbeat RPCs, so recording
			// one per candidate is cheap; resume stays at day granularity
			// because re-running the day being heartbeated is idempotent.
			activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phase, Cursor: uuid.UUID{}, Day: dayKey})

			row := &chrepo.RiskFindingUnmaskRow{
				ID:             c.ID,
				CreatedAt:      time.Time{},
				ChatMessageID:  c.ChatMessageID,
				ContentPartID:  c.ContentPartID,
				ChatID:         c.ChatID,
				Source:         c.Source,
				RuleID:         c.RuleID,
				StartPos:       c.StartPos,
				EndPos:         c.EndPos,
				MatchLen:       c.MatchLen,
				MatchRedacted:  c.MatchRedacted,
				Surface:        c.Surface,
				Field:          c.Field,
				Path:           c.Path,
				ToolCallID:     c.ToolCallID,
				OrganizationID: run.organizationID,
			}

			anchorKey := c.ChatMessageID + "\x00" + c.ContentPartID
			anchor, ok := anchors[anchorKey]
			if !ok {
				anchor = reveal.LoadAnchor(ctx, projectID, row)
				reveal.HydratePartContent(ctx, &anchor)
				anchors[anchorKey] = anchor
			}

			// A stamped chat id that disagrees with the anchor's chat means the
			// message was re-parented, so the anchor's content is not this
			// finding's to evaluate — the same refusal the unmask endpoint makes.
			chatID, attributed := risk.ResolveChatID(row, anchor)
			if !attributed {
				a.logger.DebugContext(ctx, "risk finding chat id diverges from its anchor; cannot evaluate regex candidate",
					attr.SlogChatID(c.ChatID),
					attr.SlogValueString(c.ID.String()),
				)
				fn(c, "", false)
				continue
			}

			match, ok := risk.MatchingReconstruction(row.MatchLen, reveal.Candidates(ctx, chatID, row, anchor))
			fn(c, match, ok)
		}

		if len(candidates) < regexCandidatePage {
			return nil
		}
	}
}

// chApplyRegex evaluates a regex exclusion against ClickHouse rows by
// reconstructing each candidate's plaintext from the chat store — the same
// reconstruction the audited unmask endpoint uses — and matching in Go (RE2,
// the same engine as scan-time). The plaintext never leaves the process and
// no unmask audit entry is written, mirroring scan-time matching.
func (a *Reconcile) chApplyRegex(ctx context.Context, run *chRun, ex repo.RiskExclusion, re *regexp.Regexp, startDay string) error {
	predicate := regexPredicate(ex)
	reveal := risk.NewRevealMatcher(a.logger, repo.New(a.db), a.assetStorage)

	var total int
	for _, day := range run.days(startDay) {
		dayKey := day.Format(time.DateOnly)

		var matched []uuid.UUID
		err := a.forEachRegexCandidate(ctx, run, reveal, ex.ProjectID, phaseCHApply, dayKey,
			func(lctx context.Context, afterID uuid.UUID, limit int) ([]chrepo.RetroRegexCandidate, error) {
				return a.ch.ListRetroRegexCandidates(lctx, run.scope(day), predicate, afterID, limit)
			},
			func(c chrepo.RetroRegexCandidate, match string, ok bool) {
				if ok && re.MatchString(match) {
					matched = append(matched, c.ID)
				}
			},
		)
		if err != nil {
			return fmt.Errorf("day %s: %w", dayKey, err)
		}

		for chunk := range slices.Chunk(matched, regexAppendChunk) {
			bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
			err := a.ch.AppendRetroExclusionApplyByIDs(bctx, run.scope(day), run.exclusionID, run.excludedAt, run.nextInsertedAt(), chunk)
			cancel()
			if err != nil {
				return fmt.Errorf("day %s: append regex matches: %w", dayKey, err)
			}
			activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHApply, Cursor: uuid.UUID{}, Day: dayKey})
		}
		total += len(matched)
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHApply, Cursor: uuid.UUID{}, Day: dayKey})
	}

	if total > 0 {
		a.chRows.Add(ctx, int64(total), metric.WithAttributes(attribute.String("phase", "apply")))
	}
	a.logger.InfoContext(ctx, "clickhouse exclusion apply complete",
		attr.SlogRiskExclusionID(run.exclusionID.String()),
		attr.SlogRiskExclusionMatchType(ex.MatchType),
		attr.SlogRiskReconcileRowCount(total),
	)
	return nil
}

// chReverseRegex reverses an ACTIVE regex exclusion's held rows without ever
// blanket-un-flagging: held rows provably outside the exclusion's SQL scope
// (policy binding, filters) reverse in SQL — including rows whose plaintext
// can no longer be reconstructed — and in-scope rows reverse only when their
// reconstructed plaintext provably no longer matches the pattern. Held rows
// that cannot be reconstructed (chat retention, asset loss, re-parented
// anchors) stay hidden: they were flagged with the plaintext in hand, and
// un-hiding a correctly excluded finding is the one mistake this phase must
// never make.
func (a *Reconcile) chReverseRegex(ctx context.Context, run *chRun, ex repo.RiskExclusion, re *regexp.Regexp, startDay string) error {
	scopeKeep := regexPredicate(ex).KeepScope()
	reveal := risk.NewRevealMatcher(a.logger, repo.New(a.db), a.assetStorage)

	var total uint64
	var kept int
	for _, day := range run.days(startDay) {
		dayKey := day.Format(time.DateOnly)

		// Out-of-scope rows reverse first, in SQL, so the candidate pass below
		// only lists rows still held (a reversed row's latest copy no longer
		// points at the exclusion).
		if !scopeKeep.Empty() {
			bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
			count, err := a.ch.CountRetroExclusionReversal(bctx, run.scope(day), run.exclusionID, scopeKeep)
			if err == nil && count > 0 {
				err = a.ch.AppendRetroExclusionReversal(bctx, run.scope(day), run.exclusionID, run.nextInsertedAt(), scopeKeep)
			}
			cancel()
			if err != nil {
				return fmt.Errorf("day %s: %w", dayKey, err)
			}
			total += count
			activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHReverse, Cursor: uuid.UUID{}, Day: dayKey})
		}

		var reversed []uuid.UUID
		err := a.forEachRegexCandidate(ctx, run, reveal, ex.ProjectID, phaseCHReverse, dayKey,
			func(lctx context.Context, afterID uuid.UUID, limit int) ([]chrepo.RetroRegexCandidate, error) {
				return a.ch.ListRetroRegexReversalCandidates(lctx, run.scope(day), run.exclusionID, afterID, limit)
			},
			func(c chrepo.RetroRegexCandidate, match string, ok bool) {
				switch {
				case !ok:
					kept++
				case !re.MatchString(match):
					reversed = append(reversed, c.ID)
				}
			},
		)
		if err != nil {
			return fmt.Errorf("day %s: %w", dayKey, err)
		}

		for chunk := range slices.Chunk(reversed, regexAppendChunk) {
			bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
			err := a.ch.AppendRetroExclusionReversalByIDs(bctx, run.scope(day), run.exclusionID, run.nextInsertedAt(), chunk)
			cancel()
			if err != nil {
				return fmt.Errorf("day %s: reverse regex non-matches: %w", dayKey, err)
			}
			activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHReverse, Cursor: uuid.UUID{}, Day: dayKey})
		}
		total += uint64(len(reversed))
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHReverse, Cursor: uuid.UUID{}, Day: dayKey})
	}

	if total > 0 {
		a.chRows.Add(ctx, int64(total), metric.WithAttributes(attribute.String("phase", "reverse")))
	}
	a.logger.InfoContext(ctx, "clickhouse exclusion reverse complete",
		attr.SlogRiskExclusionID(run.exclusionID.String()),
		attr.SlogRiskExclusionMatchType(ex.MatchType),
		attr.SlogRiskReconcileRowCount(int(total)),
		attr.SlogRiskReconcileRowsKept(kept),
	)
	return nil
}

func (a *Reconcile) addSkip(ctx context.Context, reason string) {
	a.chSkipped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// reconcile phases, recorded in heartbeat details so a retry can resume the
// correct loop at the correct cursor. Ordered: Postgres reverse, Postgres
// apply, ClickHouse reverse, ClickHouse apply.
const (
	phaseReverse   = "reverse"
	phaseApply     = "apply"
	phaseCHReverse = "ch_reverse"
	phaseCHApply   = "ch_apply"
)

var phaseOrder = map[string]int{
	phaseReverse:   0,
	phaseApply:     1,
	phaseCHReverse: 2,
	phaseCHApply:   3,
}

// phaseDone reports whether the resumed run had already advanced past the
// given phase (strictly later phase recorded in the heartbeat).
func phaseDone(resumed, phase string) bool {
	r, ok := phaseOrder[resumed]
	if !ok {
		return false
	}
	return r > phaseOrder[phase]
}

// reconcileProgress is the heartbeat payload: which phase was in flight and
// the cursor reached within it — a keyset uuid for the Postgres loops, a
// partition day (time.DateOnly) for the ClickHouse loops.
type reconcileProgress struct {
	Phase  string
	Cursor uuid.UUID
	Day    string
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
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phase, Cursor: cursor, Day: ""})
		if len(ids) < int(reconcileBatchLimit) {
			return nil
		}
	}
}
