package risk

import (
	"context"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
// maxFalsePositiveBatch.
func parseResultIDs(raw []string) ([]uuid.UUID, error) {
	if len(raw) == 0 {
		return nil, oops.E(oops.CodeInvalid, nil, "result_ids must not be empty")
	}
	if len(raw) > maxFalsePositiveBatch {
		return nil, oops.E(oops.CodeInvalid, nil, "too many result_ids (max %d)", maxFalsePositiveBatch)
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		id, err := uuid.Parse(r)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid result id %q", r)
		}
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

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit mark risk results false positive").LogError(ctx, s.logger)
	}

	s.mirrorFalsePositiveToClickHouse(ctx, marked)

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

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit unmark risk results false positive").LogError(ctx, s.logger)
	}

	s.mirrorFalsePositiveToClickHouse(ctx, restored)

	return nil
}

// mirrorFalsePositiveToClickHouse republishes each affected result onto the
// shared findings topic (the same one scanners publish to) so
// FindingCHWriter appends a fresh risk_findings row recording the result's
// new suppression state — set for a mark, cleared for an unmark, read
// straight off the row the UPDATE ... RETURNING already gave us. The mark
// state is published twice over during the suppression convergence:
// excluded_at/excluded_reason=manual/excluded_detail are the converged
// fields, false_positive_at the legacy one the read paths still filter on.
// Best-effort and detached from the request: Postgres already committed and
// remains the source of truth, so a publish failure here only delays when
// the change is reflected in ClickHouse-backed reads, never the RPC.
func (s *Service) mirrorFalsePositiveToClickHouse(ctx context.Context, rows []repo.RiskResult) {
	if s.findingsPub == nil || len(rows) == 0 {
		return
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		results := make([]gcp.PublishResult, 0, len(rows))
		for _, row := range rows {
			results = append(results, s.findingsPub.Publish(detached, fpMirrorMessage(row)))
		}

		for _, res := range results {
			waitCtx, cancel := context.WithTimeout(detached, 10*time.Second)
			_, err := res.Get(waitCtx)
			cancel()
			if err != nil {
				s.logger.ErrorContext(detached, "failed to mirror false positive state to clickhouse", attr.SlogError(err))
			}
		}
	}()
}

// fpMirrorMessage renders one UPDATE ... RETURNING row as the finding message
// the mirror republishes. falsePositiveAt doubles as the converged
// excluded_at: same timestamp on a mark (with excluded_reason=manual and the
// user-supplied reason as excluded_detail), all empty on an unmark. An unmark
// republish deliberately carries no excluded state so the CH writer re-runs
// its exclusion check and re-stamps rule suppression when an active exclusion
// still matches, instead of resurfacing the finding.
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
	if row.FalsePositiveAt.Valid {
		falsePositiveAt = row.FalsePositiveAt.Time.UTC().Format(time.RFC3339)
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

func (s *Service) ListDismissedRiskResults(ctx context.Context, payload *gen.ListDismissedRiskResultsPayload) (*gen.ListRiskResultsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cursor, err := parseRiskResultsCursor(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor")
	}
	pageSize := resolvePageSize(payload.Limit)
	cursorFalsePositiveAt, cursorID := cursorToParams(cursor)

	totalCount, err := s.repo.CountFalsePositiveRiskResults(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count dismissed risk results").LogError(ctx, s.logger)
	}

	rows, err := s.repo.ListFalsePositiveRiskResults(ctx, repo.ListFalsePositiveRiskResultsParams{
		ProjectID:             *authCtx.ProjectID,
		CursorFalsePositiveAt: cursorFalsePositiveAt,
		CursorID:              cursorID,
		PageLimit:             conv.SafeInt32(pageSize + 1),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list dismissed risk results").LogError(ctx, s.logger)
	}

	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		chatID := row.ChatID.String()
		// The Dismissed tab doesn't need durable content-part links, so this is
		// always the zero NullUUID (see foundRowToResult's block_id comment for
		// the same convention on tool-call blocks).
		noContentPartID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		result := foundRowToResult(row.ID, row.RiskPolicyID, row.RiskPolicyVersion, uuid.Nil, row.ChatMessageID, noContentPartID, &chatID, row.ChatTitle, row.ChatUserID, row.Source, row.RuleID, row.Description, row.Match, row.StartPos, row.EndPos, row.Confidence, row.Tags, row.Spans, row.CreatedAt)
		falsePositiveAt := row.FalsePositiveAt.Time.Format(time.RFC3339)
		result.FalsePositiveAt = &falsePositiveAt
		results = append(results, result)
		if i == pageSize {
			nextCursor = &riskResultsCursor{MessageCreatedAt: row.FalsePositiveAt.Time, ID: row.ID}
		}
	}

	return s.paginateResults(results, nextCursor, pageSize, totalCount), nil
}

// payloadReason returns the dismissal reason as a plain string, defaulting to
// empty when the caller didn't supply one.
func payloadReason(reason *string) string {
	if reason == nil {
		return ""
	}
	return *reason
}
