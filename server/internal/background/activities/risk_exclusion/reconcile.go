// Package risk_exclusion contains the Temporal activity that reconciles a risk
// exclusion against already-stored findings. It is the retroactive half of
// exclusions: the going-forward half lives in the analysis scanner
// (risk_analysis.ExclusionSet) and the ClickHouse ingest writer
// (risk.FindingCHWriter). The activity is idempotent — it always reverses the
// exclusion's prior flags, then (when the exclusion is enabled and not
// deleted) re-applies them — so it is safe to retry and correctly handles
// create, update (predicate change), delete, enable, and disable.
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
	// regexAppendChunk bounds one AppendRetroExclusionApplyByIDs statement.
	regexAppendChunk = 1000
)

// ReconcileArgs identifies the exclusion to reconcile.
type ReconcileArgs struct {
	ProjectID   uuid.UUID
	ExclusionID uuid.UUID
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

	// 1. Postgres reverse: clear any flags this exclusion previously set.
	// Skipped if a prior attempt already advanced past it.
	if !phaseDone(resume.Phase, phaseReverse) {
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
	if active && !phaseDone(resume.Phase, phaseApply) {
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
		insertedAtBase: time.Now().UTC(),
		statements:     0,
	}

	if !phaseDone(resume.Phase, phaseCHReverse) {
		startDay := ""
		if resume.Phase == phaseCHReverse {
			startDay = resume.Day
		}
		if err := a.chReverse(ctx, &run, startDay); err != nil {
			return fmt.Errorf("clickhouse reverse exclusion flags: %w", err)
		}
	}

	if active {
		startDay := ""
		if resume.Phase == phaseCHApply {
			startDay = resume.Day
		}
		if err := a.chApply(ctx, &run, ex, startDay); err != nil {
			return fmt.Errorf("clickhouse apply exclusion (%s): %w", ex.MatchType, err)
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

// chRun carries the per-run constants of the ClickHouse phases. insertedAt is
// strictly increasing across every statement in the run (reverse before
// apply) so an update's un-flag copy and re-flag copy of the same id order
// deterministically under the read paths' latest-by-inserted_at dedup.
type chRun struct {
	exclusionID    uuid.UUID
	organizationID string
	projectID      string
	excludedAt     string
	insertedAtBase time.Time
	statements     int
}

func (r *chRun) nextInsertedAt() string {
	s := chrepo.FormatCHTime(r.insertedAtBase.Add(time.Duration(r.statements) * time.Microsecond))
	r.statements++
	return s
}

// chDays yields the partition days to sweep, newest first, over the table's
// retention window. startDay ("" = from the top) resumes a partially
// completed sweep.
func chDays(startDay string) []time.Time {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	days := make([]time.Time, 0, chRetentionDays+1)
	started := startDay == ""
	for i := 0; i <= chRetentionDays; i++ {
		day := today.AddDate(0, 0, -i)
		if !started {
			if day.Format(time.DateOnly) != startDay {
				continue
			}
			started = true
		}
		days = append(days, day)
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
// exclusion, one partition day at a time.
func (a *Reconcile) chReverse(ctx context.Context, run *chRun, startDay string) error {
	var total uint64
	for _, day := range chDays(startDay) {
		bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
		count, err := a.ch.CountRetroExclusionReversal(bctx, run.scope(day), run.exclusionID)
		if err == nil && count > 0 {
			err = a.ch.AppendRetroExclusionReversal(bctx, run.scope(day), run.exclusionID, run.nextInsertedAt())
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
// on recomputed tenant fingerprints; regex reconstructs each candidate's
// plaintext from the chat store and evaluates in Go.
func (a *Reconcile) chApply(ctx context.Context, run *chRun, ex repo.RiskExclusion, startDay string) error {
	if ex.MatchType == "regex" {
		return a.chApplyRegex(ctx, run, ex, startDay)
	}

	predicate, ok := a.chPredicate(ctx, ex)
	if !ok {
		return nil
	}

	var total uint64
	for _, day := range chDays(startDay) {
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

// chApplyRegex evaluates a regex exclusion against ClickHouse rows by
// reconstructing each candidate's plaintext from the chat store — the same
// reconstruction the audited unmask endpoint uses — and matching in Go (RE2,
// the same engine as scan-time). The plaintext never leaves the process and
// no unmask audit entry is written, mirroring scan-time matching.
func (a *Reconcile) chApplyRegex(ctx context.Context, run *chRun, ex repo.RiskExclusion, startDay string) error {
	re, err := regexp.Compile(ex.MatchValue)
	if err != nil {
		// Create/update validate patterns, so this is legacy bad data: skip
		// loudly, matching the scan-time ExclusionSet's silent-skip contract.
		a.logger.WarnContext(ctx, "invalid regex exclusion pattern; not propagated to clickhouse", attr.SlogError(err), attr.SlogRiskExclusionID(ex.ID.String()))
		a.addSkip(ctx, "invalid_regex")
		return nil
	}

	predicate := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       ex.RuleIDFilter.String,
		SourceFilter:       ex.SourceFilter.String,
	}
	if ex.RiskPolicyID.Valid {
		predicate.PolicyID = ex.RiskPolicyID.UUID.String()
	}

	reveal := risk.NewRevealMatcher(a.logger, repo.New(a.db), a.assetStorage)
	projectID := ex.ProjectID
	// Anchors repeat across findings (several findings per message), so cache
	// the loaded-and-hydrated anchor per anchor id for the whole run.
	anchors := make(map[string]risk.RevealAnchor)

	var total int
	for _, day := range chDays(startDay) {
		bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
		candidates, err := a.ch.ListRetroRegexCandidates(bctx, run.scope(day), predicate)
		cancel()
		if err != nil {
			return fmt.Errorf("day %s: list candidates: %w", day.Format(time.DateOnly), err)
		}

		var matched []uuid.UUID
		for _, c := range candidates {
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

			chatID := uuid.Nil
			if parsed, err := uuid.Parse(row.ChatID); err == nil {
				chatID = parsed
			} else if anchor.ChatID.Valid {
				chatID = anchor.ChatID.UUID
			}

			match, ok := risk.MatchingReconstruction(row.MatchLen, reveal.Candidates(ctx, chatID, row, anchor))
			if !ok || !re.MatchString(match) {
				continue
			}
			matched = append(matched, c.ID)
		}

		for chunk := range slicesChunk(matched, regexAppendChunk) {
			bctx, cancel := context.WithTimeout(ctx, perBatchTimeout)
			err := a.ch.AppendRetroExclusionApplyByIDs(bctx, run.scope(day), run.exclusionID, run.excludedAt, run.nextInsertedAt(), chunk)
			cancel()
			if err != nil {
				return fmt.Errorf("day %s: append regex matches: %w", day.Format(time.DateOnly), err)
			}
		}
		total += len(matched)
		activity.RecordHeartbeat(ctx, reconcileProgress{Phase: phaseCHApply, Cursor: uuid.UUID{}, Day: day.Format(time.DateOnly)})
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

func (a *Reconcile) addSkip(ctx context.Context, reason string) {
	a.chSkipped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// slicesChunk yields fixed-size chunks of s.
func slicesChunk[T any](s []T, size int) func(func([]T) bool) {
	return func(yield func([]T) bool) {
		for start := 0; start < len(s); start += size {
			end := min(start+size, len(s))
			if !yield(s[start:end]) {
				return
			}
		}
	}
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
