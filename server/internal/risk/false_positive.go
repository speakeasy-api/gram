package risk

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
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

// FalsePositiveMutation names one manual dismissal or restore batch inside a
// project. IDs must already be parsed and deduplicated. Ids that do not exist
// in the project, or that are already in the requested state, match no row
// and are skipped; the rows returned by the core are exactly the ones that
// changed.
type FalsePositiveMutation struct {
	// OrganizationID scopes the audit entries and the ClickHouse mirror.
	OrganizationID string

	// ProjectID bounds the UPDATE: ids from another project never match.
	ProjectID uuid.UUID

	// IDs are the risk_results row ids to dismiss or restore.
	IDs []uuid.UUID

	// Reason is the optional free-text dismissal reason. Restores ignore it.
	Reason *string

	// Actor is the user attributed on every audit entry.
	Actor urn.Principal

	// ActorDisplayName is shown beside the actor in the audit log when known.
	ActorDisplayName *string
}

// FalsePositiveFindingsStore appends dismissal state copies to the findings
// store. Copies are built from each finding's latest copy, so findings that
// exist only in ClickHouse are handled too.
type FalsePositiveFindingsStore interface {
	AppendFalsePositiveSuppression(ctx context.Context, organizationID, projectID, suppressedAt, insertedAt, reason string, ids []uuid.UUID) error
	AppendFalsePositiveReversal(ctx context.Context, organizationID, projectID, insertedAt string, ids []uuid.UUID) error
}

// FalsePositiveCore applies manual false-positive marks and restores inside a
// caller-owned transaction: per-finding locks, the UPDATE, one audit entry per
// changed row, and the findings-store append. The append runs before the
// caller commits, so a failed append rolls the Postgres change back, and a
// retry repairs an append followed by a failed commit. The risk management
// API and the Platform MCP share it so both surfaces dismiss and restore
// findings identically.
type FalsePositiveCore struct {
	audit    *audit.Logger
	findings FalsePositiveFindingsStore
}

// NewFalsePositiveCore builds the shared dismissal core. A nil findings store
// makes every mark and restore fail rather than silently skip ClickHouse.
func NewFalsePositiveCore(auditLogger *audit.Logger, findings FalsePositiveFindingsStore) *FalsePositiveCore {
	return &FalsePositiveCore{audit: auditLogger, findings: findings}
}

// MarkInTransaction dismisses the still-active rows among mutation.IDs and
// returns the rows it changed.
func (c *FalsePositiveCore) MarkInTransaction(ctx context.Context, dbtx pgx.Tx, mutation FalsePositiveMutation) ([]repo.RiskResult, error) {
	if c.findings == nil {
		return nil, errFindingsStoreUnavailable
	}
	queries := repo.New(dbtx)
	if err := lockFalsePositiveTransitions(ctx, queries, mutation.ProjectID, mutation.IDs); err != nil {
		return nil, err
	}

	marked, err := queries.MarkRiskResultsFalsePositive(ctx, repo.MarkRiskResultsFalsePositiveParams{
		ProjectID: mutation.ProjectID,
		Ids:       mutation.IDs,
		Reason:    nullableText(payloadReason(mutation.Reason)),
	})
	if err != nil {
		return nil, fmt.Errorf("mark risk results false positive: %w", err)
	}

	for _, row := range marked {
		if err := c.audit.LogRiskResultDismiss(ctx, dbtx, audit.LogRiskResultDismissEvent{
			OrganizationID:   mutation.OrganizationID,
			ProjectID:        mutation.ProjectID,
			Actor:            mutation.Actor,
			ActorDisplayName: mutation.ActorDisplayName,
			ActorSlug:        nil,
			RiskResultID:     row.ID,
		}); err != nil {
			return nil, fmt.Errorf("log risk result dismiss: %w", err)
		}
	}

	now := chrepo.FormatCHTime(time.Now().UTC())
	if err := c.findings.AppendFalsePositiveSuppression(ctx, mutation.OrganizationID, mutation.ProjectID.String(), now, now, payloadReason(mutation.Reason), mutation.IDs); err != nil {
		return nil, fmt.Errorf("record dismissal in the findings store: %w", err)
	}

	return marked, nil
}

// UnmarkInTransaction restores the dismissed rows among mutation.IDs and
// returns the rows it changed.
func (c *FalsePositiveCore) UnmarkInTransaction(ctx context.Context, dbtx pgx.Tx, mutation FalsePositiveMutation) ([]repo.RiskResult, error) {
	if c.findings == nil {
		return nil, errFindingsStoreUnavailable
	}
	queries := repo.New(dbtx)
	if err := lockFalsePositiveTransitions(ctx, queries, mutation.ProjectID, mutation.IDs); err != nil {
		return nil, err
	}

	restored, err := queries.UnmarkRiskResultsFalsePositive(ctx, repo.UnmarkRiskResultsFalsePositiveParams{
		ProjectID: mutation.ProjectID,
		Ids:       mutation.IDs,
	})
	if err != nil {
		return nil, fmt.Errorf("unmark risk results false positive: %w", err)
	}

	for _, row := range restored {
		if err := c.audit.LogRiskResultRestore(ctx, dbtx, audit.LogRiskResultRestoreEvent{
			OrganizationID:   mutation.OrganizationID,
			ProjectID:        mutation.ProjectID,
			Actor:            mutation.Actor,
			ActorDisplayName: mutation.ActorDisplayName,
			ActorSlug:        nil,
			RiskResultID:     row.ID,
		}); err != nil {
			return nil, fmt.Errorf("log risk result restore: %w", err)
		}
	}

	if err := c.findings.AppendFalsePositiveReversal(ctx, mutation.OrganizationID, mutation.ProjectID.String(), chrepo.FormatCHTime(time.Now().UTC()), mutation.IDs); err != nil {
		return nil, fmt.Errorf("record restore in the findings store: %w", err)
	}

	return restored, nil
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

	if _, err := s.falsePositiveCore().MarkInTransaction(ctx, dbtx, FalsePositiveMutation{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		IDs:              ids,
		Reason:           payload.Reason,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "mark risk results false positive").LogError(ctx, s.logger)
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

	if _, err := s.falsePositiveCore().UnmarkInTransaction(ctx, dbtx, FalsePositiveMutation{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		IDs:              ids,
		Reason:           nil,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "unmark risk results false positive").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit unmark risk results false positive").LogError(ctx, s.logger)
	}

	return nil
}

var errFindingsStoreUnavailable = errors.New("risk findings store is unavailable")

// falsePositiveCore passes a nil store through as an untyped nil, so a missing
// ClickHouse connection fails dismissals instead of panicking.
func (s *Service) falsePositiveCore() *FalsePositiveCore {
	var store FalsePositiveFindingsStore
	if s.findingsCH != nil {
		store = s.findingsCH
	}
	return NewFalsePositiveCore(s.audit, store)
}

// lockFalsePositiveTransitions serializes marks and restores per finding, in a
// stable order so concurrent batches cannot deadlock.
func lockFalsePositiveTransitions(ctx context.Context, queries *repo.Queries, projectID uuid.UUID, ids []uuid.UUID) error {
	lockIDs := make([]string, len(ids))
	for i, id := range ids {
		lockIDs[i] = id.String()
	}
	sort.Strings(lockIDs)
	for _, id := range lockIDs {
		if err := queries.LockRiskResultFalsePositiveTransition(ctx, repo.LockRiskResultFalsePositiveTransitionParams{ProjectID: projectID.String(), ID: id}); err != nil {
			return fmt.Errorf("lock false positive transition for result %s: %w", id, err)
		}
	}
	return nil
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
	titles, blocks, userEmails := s.listDisplayEnrichment(ctx, authCtx.ActiveOrganizationID, projectID, listRows)

	results := make([]*types.RiskResult, 0, len(rows))
	var nextCursor *riskResultsCursor
	for i, row := range rows {
		result := chListRowToResult(row.RiskFindingListRow, titles, blocks, userEmails)
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
