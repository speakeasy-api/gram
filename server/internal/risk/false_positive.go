package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// maxFalsePositiveBatch bounds a single mark/unmark request to what a UI
// multiselect can realistically produce (a page of results, generously). It
// is not a pattern-match sweep like exclusions, so there is no batch job —
// just a single UPDATE ... WHERE id = ANY(@ids).
const maxFalsePositiveBatch = 500

// parseResultIDs converts the payload's result_ids strings to uuid.UUID,
// rejecting the request if any entry is malformed or the batch exceeds
// maxFalsePositiveBatch. Duplicates are dropped so the returned slice is a
// set and every id counts once toward the UPDATE and the mirror.
func parseResultIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "result_ids must not be empty")
	}
	if len(raw) > maxFalsePositiveBatch {
		return nil, oops.E(oops.CodeInvalid, nil, "too many result_ids (max %d)", maxFalsePositiveBatch)
	}
	ids := make([]uuid.UUID, 0, len(raw))
	seen := make(map[uuid.UUID]struct{}, len(raw))
	for _, r := range raw {
		id, err := uuid.Parse(r)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid result id %q", r)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Service) MarkRiskResultsFalsePositive(ctx context.Context, payload *gen.MarkRiskResultsFalsePositivePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	ids, err := parseResultIDs(payload.ResultIds)
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	marked, err := repo.New(dbtx).MarkRiskResultsFalsePositive(ctx, repo.MarkRiskResultsFalsePositiveParams{
		ProjectID: *authCtx.ProjectID,
		Ids:       ids,
		Reason:    nullableText(payloadReason(payload.Reason)),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mark risk results false positive").LogError(ctx, s.logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	for _, row := range marked {
		if err := s.audit.LogRiskResultDismiss(ctx, dbtx, audit.LogRiskResultDismissEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			RiskResultID:     row.ID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log risk result dismiss").LogError(ctx, s.logger)
		}
	}

	if err := enqueueFalsePositiveMirror(ctx, dbtx, authCtx.ActiveOrganizationID, marked); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record dismissal in the findings store").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit mark risk results false positive").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) UnmarkRiskResultsFalsePositive(ctx context.Context, payload *gen.UnmarkRiskResultsFalsePositivePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	ids, err := parseResultIDs(payload.ResultIds)
	if err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	restored, err := repo.New(dbtx).UnmarkRiskResultsFalsePositive(ctx, repo.UnmarkRiskResultsFalsePositiveParams{
		ProjectID: *authCtx.ProjectID,
		Ids:       ids,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "unmark risk results false positive").LogError(ctx, s.logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	for _, row := range restored {
		if err := s.audit.LogRiskResultRestore(ctx, dbtx, audit.LogRiskResultRestoreEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			RiskResultID:     row.ID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log risk result restore").LogError(ctx, s.logger)
		}
	}

	if err := enqueueFalsePositiveMirror(ctx, dbtx, authCtx.ActiveOrganizationID, restored); err != nil {
		return oops.E(oops.CodeUnexpected, err, "record restore in the findings store").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit unmark risk results false positive").LogError(ctx, s.logger)
	}

	return nil
}

// enqueueFalsePositiveMirror appends each state change to the transactional
// outbox using the caller's transaction: the relay publishes the same
// riskv1.Finding message the scanners produce onto the shared findings topic,
// where FindingCHWriter appends a fresh risk_findings row recording the
// result's new suppression state — set for a mark, cleared for an unmark. The
// mark state is published twice over during the suppression convergence:
// excluded_at/excluded_reason=manual/excluded_detail are the converged fields,
// false_positive_at the legacy one the read paths still filter on.
// Enqueued-at-commit rather than published post-commit, so the mirror is
// atomic with the Postgres state change: no publish step remains that can fail
// after the caller has been told the change happened, and a retried RPC whose
// UPDATE matches nothing has nothing to repair — the original request either
// committed (mirror durably enqueued) or changed nothing at all.
func enqueueFalsePositiveMirror(ctx context.Context, dbtx pgx.Tx, orgID string, rows []repo.RiskResult) error {
	if len(rows) == 0 {
		return nil
	}

	msgs := make([]outbox.Message, 0, len(rows))
	for _, row := range rows {
		// A row carrying an exclusion stamp is suppressed by the exclusion
		// pipeline, which owns its ClickHouse identity: mirroring a manual
		// suppression (or an unsuppression) over it would overwrite the rule
		// suppression at read time, resurfacing or re-labeling a finding the
		// exclusion still covers. Postgres keeps the mark either way.
		if row.ExcludedAt.Valid {
			continue
		}
		msgs = append(msgs, outbox.Message{
			Proto:      fpMirrorMessage(row),
			PublicID:   uuid.Nil,
			Attributes: nil,
		})
	}
	if len(msgs) == 0 {
		return nil
	}
	if _, err := outbox.PublishBatch(ctx, dbtx, orgID, msgs); err != nil {
		return fmt.Errorf("enqueue suppression state change: %w", err)
	}
	return nil
}

// fpMirrorMessage renders one risk_results row as the finding message the
// mirror republishes. falsePositiveAt doubles as the converged
// excluded_at: same timestamp on a mark (with excluded_reason=manual and the
// user-supplied reason as excluded_detail), all empty on an unmark. An unmark
// republish deliberately carries no excluded state so the CH writer re-runs
// its exclusion check and re-stamps rule suppression when an active exclusion
// still matches, instead of resurfacing the finding. The event kind marks the
// message as a state change either way, so read-time dedup ranks it above the
// finding's scanner copies and a redelivered scanner row cannot undo it.
func fpMirrorMessage(row repo.RiskResult) *riskv1.Finding {
	id := row.ID.String()
	projectID := row.ProjectID.String()
	riskPolicyID := row.RiskPolicyID.String()
	createdAt := row.CreatedAt.Time.UTC().Format(time.RFC3339)
	var chatMessageID string
	if row.ChatMessageID.Valid {
		chatMessageID = row.ChatMessageID.UUID.String()
	}
	var falsePositiveAt string
	excludedReason := ""
	excludedDetail := ""
	eventKind := chrepo.EventKindUnsuppression
	if row.FalsePositiveAt.Valid {
		eventKind = chrepo.EventKindSuppression
		// RFC3339Nano, not RFC3339: the plain layout truncates
		// clock_timestamp()'s fractional seconds, and the DateTime64(9)
		// columns this lands in can hold the full precision. The writer's
		// time.Parse(time.RFC3339, ...) accepts fractional seconds as-is.
		falsePositiveAt = row.FalsePositiveAt.Time.UTC().Format(time.RFC3339Nano)
		excludedReason = chrepo.ExcludedReasonManual
		excludedDetail = row.FalsePositiveReason.String
	}

	surface := fpMirrorSurface(row.Source)

	return riskv1.Finding_builder{
		Id:                &id,
		RequestId:         conv.PtrEmpty(""),
		ChatMessageId:     &chatMessageID,
		ProjectId:         &projectID,
		OrganizationId:    &row.OrganizationID,
		RiskPolicyId:      &riskPolicyID,
		RiskPolicyVersion: &row.RiskPolicyVersion,
		CreatedAt:         &createdAt,
		RuleId:            &row.RuleID.String,
		Description:       &row.Description.String,
		Match:             &row.Match.String,
		StartPos:          &row.StartPos.Int32,
		EndPos:            &row.EndPos.Int32,
		Tags:              row.Tags,
		Source:            &row.Source,
		Confidence:        &row.Confidence.Float64,
		FalsePositiveAt:   &falsePositiveAt,
		ExcludedAt:        &falsePositiveAt,
		ExcludedReason:    &excludedReason,
		ExcludedDetail:    &excludedDetail,
		Surface:           &surface,
		EventKind:         &eventKind,
	}.Build()
}

// fpMirrorSurface maps a republished Postgres row's source to the text its
// offsets index. Every risk_results row is batch-scanned, so this mirrors the
// offline backfill's per-source mapping (riskfindings transform sourceSurface)
// rather than the live stream defaults in scanners.FindingSurface: the mirror
// row supersedes the backfilled row for the same id at read time and must not
// change its reveal semantics. Batch gitleaks offsets index the composed scan
// surface (content plus tool-call arguments) and batch presidio offsets index
// a YAML transform of the message — neither is the anchored content the live
// defaults describe. Custom rows fall to "" (no span context here), so reveal
// uses its verified candidate cascade — the accepted precision loss on
// FP-mirrored custom rows until the FP flow moves onto ClickHouse.
func fpMirrorSurface(source string) string {
	switch source {
	case "gitleaks":
		return "scan_surface"
	case "presidio":
		return "legacy_presidio"
	case "prompt_injection", "llm_judge":
		return scanners.SurfaceNone
	case "shadow_mcp", "account_identity", "destructive_tool", "cli_destructive":
		return scanners.SurfaceDerived
	default:
		return ""
	}
}

// ListDismissedRiskResults serves the Dismissed tab from ClickHouse: findings
// whose latest state is a manual or automated dismissal, newest dismissal
// first. Rows arrive store-side redacted like the Risk Events listing — the raw
// match never reaches ClickHouse — so results carry MatchRedacted and a nil
// Match where the Postgres-backed listing returned the raw value.
func (s *Service) ListDismissedRiskResults(ctx context.Context, payload *gen.ListDismissedRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if s.findingsCH == nil {
		return nil, oops.E(oops.CodeUnexpected, nil, "dismissed risk results are unavailable").LogError(ctx, s.logger)
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor")
	}
	pageSize := resolvePageSize(payload.Limit)
	projectID := *authCtx.ProjectID

	params := chrepo.ListDismissedRiskFindingsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID.String(),
		Reasons:        payload.Reasons,
		CursorTime:     nil,
		CursorID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		// resolvePageSize bounds pageSize to [1, 200], so the conversion
		// cannot wrap.
		Limit: uint64(conv.SafeInt32(pageSize)) + 1, // #nosec G115 -- non-negative by construction.
	}
	if cursor != nil {
		// The cursor's time component is the suppression time on this listing,
		// the same slot the Risk Events cursor fills with the message event
		// time.
		cursorTime := cursor.MessageCreatedAt
		params.CursorTime = &cursorTime
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}

	totalCount, err := s.findingsCH.CountDismissedRiskFindings(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count dismissed risk results").LogError(ctx, s.logger)
	}

	rows, err := s.findingsCH.ListDismissedRiskFindings(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list dismissed risk results").LogError(ctx, s.logger)
	}

	listRows := make([]chrepo.RiskFindingListRow, 0, len(rows))
	for _, row := range rows {
		listRows = append(listRows, row.RiskFindingListRow)
	}
	titles, blocks := s.listDisplayEnrichment(ctx, projectID, listRows)

	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		result := chListRowToResult(row.RiskFindingListRow, titles, blocks)
		suppressedAt := row.SuppressedAt.UTC().Format(time.RFC3339)
		result.SuppressedAt = &suppressedAt
		// FalsePositiveAt is the deprecated mirror of SuppressedAt, kept
		// populated while clients migrate to the suppressed_* fields.
		result.FalsePositiveAt = &suppressedAt
		// Legacy pre-convergence rows carry no excluded_reason, so derive it
		// the way chrepo's suppressedReasonExpr does (keep the two in
		// lockstep): an exclusion id marks a rule suppression, anything else
		// can only have come from a dismissal and maps to manual — keeping
		// the API enum closed until the TTL retires such rows.
		reason := row.SuppressedReason
		if reason == "" {
			if row.ExclusionID != nil {
				reason = chrepo.ExcludedReasonRule
			} else {
				reason = chrepo.ExcludedReasonManual
			}
		}
		result.SuppressedReason = &reason
		result.SuppressedDetail = conv.PtrEmpty(row.SuppressedDetail)
		if row.ExclusionID != nil {
			result.ExclusionID = conv.PtrEmpty(row.ExclusionID.String())
		}
		results = append(results, result)
		if i == pageSize {
			// Cursor from the LAST RETURNED row (not this extra row): the
			// next-page predicate is a strict (suppressed_at, id) <, so a
			// cursor pointing at the extra row would skip it entirely.
			last := rows[pageSize-1]
			nextCursor = &riskResultsCursor{MessageCreatedAt: last.SuppressedAt, ID: last.ID}
		}
	}

	return s.paginateResults(results, nextCursor, pageSize, safeCount(totalCount)), nil
}

// payloadReason returns the dismissal reason as a plain string, defaulting to
// empty when the caller didn't supply one.
func payloadReason(reason *string) string {
	if reason == nil {
		return ""
	}
	return *reason
}
