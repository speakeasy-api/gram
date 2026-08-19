package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// chDismissalCopy builds the ClickHouse row mirrorFalsePositiveToClickHouse
// appends when a result is dismissed: a fresh copy of the finding carrying the
// suppression on excluded_at/excluded_reason plus the legacy false_positive_at.
// A zero suppressedAt builds the undo copy instead — same id, no suppression at
// all — which is what an unmark republish produces.
func chDismissalCopy(t *testing.T, projectID uuid.UUID, orgID, policyID string, id, chatID, msgID uuid.UUID, suppressedAt time.Time) chrepo.RiskFindingRow {
	t.Helper()

	created := time.Now().UTC().AddDate(0, 0, -1)
	row := chListFinding(t, projectID, orgID, chatID, msgID, policyID, created, created, "gitleaks", "aws-access-key-id", "alice@example.com", "AKIA**************LE", "fp-dismissal", "")
	row.ID = id
	if !suppressedAt.IsZero() {
		at := suppressedAt
		row.ExcludedAt = &at
		row.ExcludedReason = chrepo.ExcludedReasonManual
		row.ExcludedDetail = "noise"
		row.FalsePositiveAt = &at
	}
	return row
}

func TestMarkUnmarkRiskResultsFalsePositive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("False Positive Test")})
	require.NoError(t, err)
	policyID, err2 := uuid.Parse(policy.ID)
	require.NoError(t, err2)

	chatID, msgID := seedChatMessage(t, ti, projectID, orgID)
	seedRiskResult(t, ti, projectID, orgID, policyID, 1, msgID, true)

	before, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Len(t, before.Results, 1)
	resultID := before.Results[0].ID
	resultUUID, err := uuid.Parse(resultID)
	require.NoError(t, err)

	// The dismissed listing reads ClickHouse while the mark/unmark RPCs write
	// Postgres and republish onto the findings topic. The harness wires
	// findingsPub as nil, so the mirror never runs here and each state change's
	// ClickHouse copy is appended by hand — the same rows
	// mirrorFalsePositiveToClickHouse would produce.
	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		chDismissalCopy(t, projectID, orgID, policy.ID, resultUUID, chatID, msgID, time.Time{}),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	dismissCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)

	// Mark as false positive: it must disappear from listRiskResults and show
	// up in listDismissedResults, and it must be atomic with one audit entry.
	err = ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID},
		Reason:    new("noise"),
	})
	require.NoError(t, err)

	dismissCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, dismissCountBefore+1, dismissCountAfter)

	afterMark, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Empty(t, afterMark.Results, "dismissed result must not appear in listRiskResults")

	dismissedAt := time.Now().UTC()
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		chDismissalCopy(t, projectID, orgID, policy.ID, resultUUID, chatID, msgID, dismissedAt),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, dismissed.Results, 1)
	require.Equal(t, resultID, dismissed.Results[0].ID)
	require.NotNil(t, dismissed.Results[0].FalsePositiveAt)
	require.Equal(t, int64(1), dismissed.TotalCount)

	// The reason isn't surfaced on the API type today (no UI sends one yet),
	// but it must still land in Postgres from the RPC payload.
	dismissedRows, err := riskrepo.New(ti.conn).ListFalsePositiveRiskResults(ctx, riskrepo.ListFalsePositiveRiskResultsParams{
		ProjectID: projectID,
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, dismissedRows, 1)
	require.Equal(t, resultUUID, dismissedRows[0].ID)
	require.True(t, dismissedRows[0].FalsePositiveReason.Valid)
	require.Equal(t, "noise", dismissedRows[0].FalsePositiveReason.String)

	restoreCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)

	// Undo: the result must come back in listRiskResults and disappear from
	// listDismissedResults.
	err = ti.service.UnmarkRiskResultsFalsePositive(ctx, &gen.UnmarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID},
	})
	require.NoError(t, err)

	restoreCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)
	require.Equal(t, restoreCountBefore+1, restoreCountAfter)

	afterUnmark, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Len(t, afterUnmark.Results, 1)

	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		chDismissalCopy(t, projectID, orgID, policy.ID, resultUUID, chatID, msgID, time.Time{}),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	dismissedAfterUnmark, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, dismissedAfterUnmark.Results, "the undo's ClickHouse copy supersedes the dismissal")
	require.Equal(t, int64(0), dismissedAfterUnmark.TotalCount)
}

func TestMarkRiskResultsFalsePositive_RejectsEmptyIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	err := ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{ResultIds: nil})
	require.Error(t, err)
}

func TestMarkRiskResultsFalsePositive_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn) // no org:admin grant

	err := ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{
		ResultIds: []string{uuid.NewString()},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestUnmarkRiskResultsFalsePositive_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn) // no org:admin grant

	err := ti.service.UnmarkRiskResultsFalsePositive(ctx, &gen.UnmarkRiskResultsFalsePositivePayload{
		ResultIds: []string{uuid.NewString()},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListDismissedRiskResults_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn) // no org:admin grant

	_, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestMarkRiskResultsFalsePositive_RejectsBatchTooLarge exercises the
// maxFalsePositiveBatch guard (matching the "no batch job" scoping decision
// in the plan: multiselect is bounded, so a page-sized cap is enough).
func TestMarkRiskResultsFalsePositive_RejectsBatchTooLarge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	ids := make([]string, 501)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	err := ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{ResultIds: ids})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

// TestMarkRiskResultsFalsePositive_ContentPartAnchoredFindingIsListable keeps
// a past regression pinned on the ClickHouse listing. A finding anchored to a
// content part rather than a chat message (per risk_results_anchor_check a
// result carries exactly one of the two) was once dropped by the Postgres
// listing's anchor join, leaving it dismissible but never listable and so
// permanently unrestorable through the UI. ClickHouse carries the anchor as a
// plain content_part_id column with no join to drop it, and this pins that the
// result reaches the tab with the anchor intact.
func TestMarkRiskResultsFalsePositive_ContentPartAnchoredFindingIsListable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Content Part False Positive Test")})
	require.NoError(t, err)
	policyID, err := uuid.Parse(policy.ID)
	require.NoError(t, err)

	chatID, msgID := seedChatMessage(t, ti, projectID, orgID)

	repo := riskrepo.New(ti.conn)
	partID, err := repo.CreateChatContentPartForTest(ctx, riskrepo.CreateChatContentPartForTestParams{
		ChatID:              chatID,
		ProjectID:           uuid.NullUUID{UUID: projectID, Valid: true},
		Kind:                "prompt_attachment",
		ContentAssetUrl:     "gs://test-bucket/content-part.txt",
		ParentChatMessageID: uuid.NullUUID{UUID: msgID, Valid: true},
	})
	require.NoError(t, err)

	resultID, err := uuid.NewV7()
	require.NoError(t, err)
	match := "SECRET_ATTACHMENT_TOKEN"
	_, err = repo.InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    orgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     uuid.NullUUID{},
		ChatContentPartID: uuid.NullUUID{UUID: partID, Valid: true},
		Source:            "gitleaks",
		Found:             true,
		RuleID:            pgtype.Text{String: "generic-api-key", Valid: true},
		Description:       pgtype.Text{String: "Generic API key", Valid: true},
		Match:             pgtype.Text{String: match, Valid: true},
		StartPos:          pgtype.Int4{Int32: 0, Valid: true},
		EndPos:            pgtype.Int4{Int32: int32(len(match)), Valid: true},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: true},
	}})
	require.NoError(t, err)

	err = ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID.String()},
	})
	require.NoError(t, err)

	// The harness has no findings publisher, so the mirror the mark would
	// normally trigger is appended by hand. A content-part-anchored finding
	// carries its anchor in content_part_id with chat_message_id empty.
	dismissedAt := time.Now().UTC()
	dismissal := chDismissalCopy(t, projectID, orgID, policy.ID, resultID, chatID, msgID, dismissedAt)
	dismissal.ChatMessageID = ""
	dismissal.ContentPartID = partID.String()

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{dismissal}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, dismissed.Results, 1, "a content-part-anchored dismissal must still be listable")
	require.Equal(t, resultID.String(), dismissed.Results[0].ID)
	require.NotNil(t, dismissed.Results[0].FalsePositiveAt)
	require.NotNil(t, dismissed.Results[0].ChatContentPartID)
	require.Equal(t, partID.String(), *dismissed.Results[0].ChatContentPartID)
	require.Nil(t, dismissed.Results[0].ChatMessageID)

	err = ti.service.UnmarkRiskResultsFalsePositive(ctx, &gen.UnmarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID.String()},
	})
	require.NoError(t, err)

	restored := chDismissalCopy(t, projectID, orgID, policy.ID, resultID, chatID, msgID, time.Time{})
	restored.ChatMessageID = ""
	restored.ContentPartID = partID.String()
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{restored}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	afterUndo, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, afterUndo.Results)
}

// TestListDismissedRiskResults_ClickHousePageOrderingAndRedaction drives the
// ClickHouse-backed Dismissed tab end to end: suppression-time ordering, cursor
// pagination, the total count, the store-side redaction the tab now inherits
// from the Risk Events listing, and the predicate's edges — automated sweep
// rows and legacy false-positive-only rows are listed, while rule suppression,
// open findings, dead-letter sentinels and other tenants are not.
func TestListDismissedRiskResults_ClickHousePageOrderingAndRedaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Dismissed CH Page")})
	require.NoError(t, err)

	// Relative times: the table's 90-day created_at TTL expires hardcoded
	// dates at insert once the calendar catches up.
	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	createdAt := base.Add(4 * time.Hour)

	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	finding := func(ruleID, matchRedacted string, messageCreatedAt time.Time) chrepo.RiskFindingRow {
		return chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, messageCreatedAt, "gitleaks", ruleID, "alice@example.com", matchRedacted, "", "")
	}

	// The newest dismissal, and the only one carrying a user-supplied reason.
	manualAt := base.Add(9 * time.Hour)
	manual := finding("secret.github_pat", "AKIA**************LE", base.Add(time.Hour))
	manual.ExcludedAt = &manualAt
	manual.ExcludedReason = chrepo.ExcludedReasonManual
	manual.ExcludedDetail = "noise"
	manual.FalsePositiveAt = &manualAt

	// Two dismissals sharing one suppression timestamp, straddling the page
	// boundary below. Ties are the common case, not an exotic one:
	// InsertRiskFindings binds excluded_at as a time.Time, which the driver
	// stores at whole-second precision, so a bulk dismiss collapses onto a
	// single second. Only the cursor's id tiebreak keeps such a pair from
	// repeating or vanishing across pages.
	tiedAt := base.Add(8 * time.Hour)
	tiedA := finding("secret.slack_token", "xoxb****************ab", base.Add(2*time.Hour))
	tiedA.ExcludedAt = &tiedAt
	tiedA.ExcludedReason = chrepo.ExcludedReasonAutomated
	tiedA.ExcludedDetail = "known test fixture"

	tiedB := finding("secret.gcp_api_key", "AIza********************ZY", base.Add(150*time.Minute))
	tiedB.ExcludedAt = &tiedAt
	tiedB.ExcludedReason = chrepo.ExcludedReasonManual

	// The oldest: a pre-convergence dismissal that only ever got
	// false_positive_at, which the predicate's fallback disjunct picks up.
	legacyAt := base.Add(7 * time.Hour)
	legacy := finding("secret.aws_secret_key", "wJal********************EY", base.Add(3*time.Hour))
	legacy.FalsePositiveAt = &legacyAt

	// Rule-suppressed findings are part of the suppressed listing too — the
	// newest suppression in this fixture set.
	ruleAt := base.Add(10 * time.Hour)
	exclusionID := uuid.Must(uuid.NewV7())
	ruleSuppressed := finding("secret.db_password", "hunt****ter", base.Add(4*time.Hour))
	ruleSuppressed.ExcludedAt = &ruleAt
	ruleSuppressed.ExclusionID = &exclusionID
	ruleSuppressed.ExcludedReason = chrepo.ExcludedReasonRule

	// Not listed: an open finding was never suppressed, and a dead-letter
	// sentinel is not a finding.
	open := finding("secret.stripe_api_key", "sk_l**********ve", base.Add(5*time.Hour))

	deadLetter := finding("", "", base.Add(6*time.Hour))
	deadLetter.DeadLetterReason = "could-not-analyze"
	deadLetter.Category = ""
	deadLetter.ExcludedAt = &manualAt
	deadLetter.ExcludedReason = chrepo.ExcludedReasonManual

	foreign := chListFinding(t, uuid.New(), "org_"+uuid.NewString(), chatID, msgID, policy.ID, createdAt, base.Add(time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "AKIA**************LE", "", "")
	foreign.ExcludedAt = &manualAt
	foreign.ExcludedReason = chrepo.ExcludedReasonManual

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		manual, tiedA, tiedB, legacy, ruleSuppressed, open, deadLetter, foreign,
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	ids := func(result *gen.ListRiskResultsResult) []string {
		out := make([]string, 0, len(result.Results))
		for _, r := range result.Results {
			out = append(out, r.ID)
		}
		return out
	}

	pageSize := 3
	page1, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: &pageSize, Reasons: nil})
	require.NoError(t, err)
	require.Equal(t, int64(5), page1.TotalCount)
	require.Len(t, page1.Results, 3)
	require.Equal(t, ruleSuppressed.ID.String(), page1.Results[0].ID, "newest suppression first — the rule-suppressed row")
	require.Equal(t, manual.ID.String(), page1.Results[1].ID)
	require.Contains(t, []string{tiedA.ID.String(), tiedB.ID.String()}, page1.Results[2].ID)
	require.NotNil(t, page1.NextCursor)

	// The rule-suppressed row carries its provenance: reason rule, the
	// exclusion id, no detail.
	ruleRow := page1.Results[0]
	require.NotNil(t, ruleRow.SuppressedReason)
	require.Equal(t, chrepo.ExcludedReasonRule, *ruleRow.SuppressedReason)
	require.NotNil(t, ruleRow.ExclusionID)
	require.Equal(t, exclusionID.String(), *ruleRow.ExclusionID)
	require.Nil(t, ruleRow.SuppressedDetail)
	require.NotNil(t, ruleRow.SuppressedAt)
	require.Equal(t, ruleAt.Format(time.RFC3339), *ruleRow.SuppressedAt)

	// Store-side redaction: the suppressed listing serves match_redacted like
	// the Risk Events listing, never the raw match.
	first := page1.Results[1]
	require.Nil(t, first.Match)
	require.Nil(t, first.Spans)
	require.NotNil(t, first.MatchRedacted)
	require.Equal(t, "AKIA**************LE", *first.MatchRedacted)

	require.NotNil(t, first.FalsePositiveAt)
	require.Equal(t, manualAt.Format(time.RFC3339), *first.FalsePositiveAt)

	// The suppressed_* fields carry the converged suppression state;
	// false_positive_at above is their deprecated mirror.
	require.NotNil(t, first.SuppressedAt)
	require.Equal(t, *first.FalsePositiveAt, *first.SuppressedAt)
	require.NotNil(t, first.SuppressedReason)
	require.Equal(t, chrepo.ExcludedReasonManual, *first.SuppressedReason)
	require.NotNil(t, first.SuppressedDetail)
	require.Equal(t, "noise", *first.SuppressedDetail)
	require.Nil(t, first.ExclusionID, "manual suppression carries no exclusion id")

	// Postgres display enrichment, same as the Risk Events listing.
	require.NotNil(t, first.ChatTitle)
	require.Equal(t, "test chat", *first.ChatTitle)
	require.NotNil(t, first.UserID)
	require.Equal(t, "alice@example.com", *first.UserID)

	page2, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: &pageSize, Cursor: page1.NextCursor, Reasons: nil})
	require.NoError(t, err)
	require.Len(t, page2.Results, 2)
	require.Equal(t, legacy.ID.String(), page2.Results[1].ID, "the legacy false-positive-only row is the fallback disjunct's job")
	require.Nil(t, page2.NextCursor)
	require.Equal(t, int64(5), page2.TotalCount)
	require.NotNil(t, page2.Results[1].FalsePositiveAt)
	require.Equal(t, legacyAt.Format(time.RFC3339), *page2.Results[1].FalsePositiveAt)
	// A legacy row carries no excluded_reason; the API maps it to manual so
	// the reason enum stays closed while the TTL retires such rows.
	require.NotNil(t, page2.Results[1].SuppressedReason)
	require.Equal(t, chrepo.ExcludedReasonManual, *page2.Results[1].SuppressedReason)
	require.NotNil(t, page2.Results[1].SuppressedAt)
	require.Equal(t, legacyAt.Format(time.RFC3339), *page2.Results[1].SuppressedAt)
	require.Nil(t, page2.Results[1].SuppressedDetail)

	// The automated dismissal keeps its reason and catalog detail wherever the
	// tied pair landed.
	var automated *types.RiskResult
	for _, r := range append(page1.Results, page2.Results...) {
		if r.ID == tiedA.ID.String() {
			automated = r
		}
	}
	require.NotNil(t, automated)
	require.NotNil(t, automated.SuppressedReason)
	require.Equal(t, chrepo.ExcludedReasonAutomated, *automated.SuppressedReason)
	require.NotNil(t, automated.SuppressedDetail)
	require.Equal(t, "known test fixture", *automated.SuppressedDetail)

	// The tied pair is split across the boundary exactly once: no repeat, no
	// skip. Which of the two lands on which page is ClickHouse's UUID ordering
	// to decide; the cursor only has to agree with it.
	require.ElementsMatch(t,
		[]string{ruleSuppressed.ID.String(), manual.ID.String(), tiedA.ID.String(), tiedB.ID.String(), legacy.ID.String()},
		append(ids(page1), ids(page2)...),
	)

	// The reasons filter narrows on the derived reason, and its counts follow.
	ruleOnly, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: nil, Cursor: nil, Reasons: []string{chrepo.ExcludedReasonRule}})
	require.NoError(t, err)
	require.Equal(t, int64(1), ruleOnly.TotalCount)
	require.Len(t, ruleOnly.Results, 1)
	require.Equal(t, ruleSuppressed.ID.String(), ruleOnly.Results[0].ID)

	// The legacy false-positive-only row derives manual, so it joins the
	// manual dismissals under the filter.
	manualOnly, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: nil, Cursor: nil, Reasons: []string{chrepo.ExcludedReasonManual}})
	require.NoError(t, err)
	require.Equal(t, int64(3), manualOnly.TotalCount)
	require.ElementsMatch(t,
		[]string{manual.ID.String(), tiedB.ID.String(), legacy.ID.String()},
		ids(manualOnly),
	)
}

// TestListDismissedRiskResults_ClickHouseLegacyRuleRowDerivesRule pins the
// derived reason on pre-convergence rule exclusions: a row annotated at ingest
// or reconcile time before the suppression convergence carries excluded_at and
// exclusion_id but no excluded_reason, and both the listing's reason field and
// the reasons filter must classify it as rule, not manual.
func TestListDismissedRiskResults_ClickHouseLegacyRuleRowDerivesRule(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Legacy Rule Row")})
	require.NoError(t, err)

	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	excludedAt := base.Add(time.Hour)
	exclusionID := uuid.Must(uuid.NewV7())
	legacyRule := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base, base, "gitleaks", "secret.github_pat", "alice@example.com", "AKIA**************LE", "", "")
	legacyRule.ExcludedAt = &excludedAt
	legacyRule.ExclusionID = &exclusionID
	// Deliberately no ExcludedReason: this is the pre-convergence shape.

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{legacyRule}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	listed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: nil, Cursor: nil, Reasons: nil})
	require.NoError(t, err)
	require.Len(t, listed.Results, 1)
	row := listed.Results[0]
	require.NotNil(t, row.SuppressedReason)
	require.Equal(t, chrepo.ExcludedReasonRule, *row.SuppressedReason)
	require.NotNil(t, row.ExclusionID)
	require.Equal(t, exclusionID.String(), *row.ExclusionID)

	// The SQL-side derived reason agrees with the Go-side mapping.
	ruleOnly, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: nil, Cursor: nil, Reasons: []string{chrepo.ExcludedReasonRule}})
	require.NoError(t, err)
	require.Len(t, ruleOnly.Results, 1)

	manualOnly, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{Limit: nil, Cursor: nil, Reasons: []string{chrepo.ExcludedReasonManual}})
	require.NoError(t, err)
	require.Empty(t, manualOnly.Results)
	require.Zero(t, manualOnly.TotalCount)
}

// TestListDismissedRiskResults_ClickHouseResolvesLatestCopy pins the
// dedup-before-predicate order: suppression state changes by appending a newer
// copy of a finding, so a restore must drop the id from the listing even though
// its older, dismissed copy still matches the predicate on its own.
func TestListDismissedRiskResults_ClickHouseResolvesLatestCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Dismissed CH Latest Copy")})
	require.NoError(t, err)

	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	chQueries := chrepo.New(ti.chConn)

	restoredID := uuid.Must(uuid.NewV7())
	stillDismissedID := uuid.Must(uuid.NewV7())
	dismissedAt := time.Now().UTC().Add(-time.Hour)

	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		chDismissalCopy(t, projectID, orgID, policy.ID, restoredID, chatID, msgID, dismissedAt),
		chDismissalCopy(t, projectID, orgID, policy.ID, stillDismissedID, chatID, msgID, dismissedAt),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// The undo's copy: same id, no suppression, inserted later.
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		chDismissalCopy(t, projectID, orgID, policy.ID, restoredID, chatID, msgID, time.Time{}),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, dismissed.Results, 1)
	require.Equal(t, stillDismissedID.String(), dismissed.Results[0].ID)
	require.Equal(t, int64(1), dismissed.TotalCount)
}
