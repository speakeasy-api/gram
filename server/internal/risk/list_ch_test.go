package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// chListFinding builds one ClickHouse listing row: an overview-shaped finding
// with the list-relevant extras (event time, redaction, fingerprint, assistant)
// set explicitly.
func chListFinding(t *testing.T, projectID uuid.UUID, orgID string, chatID, msgID uuid.UUID, policyID string, createdAt, messageCreatedAt time.Time, source, ruleID, externalUserID, matchRedacted, tenantFingerprint, assistantID string) chrepo.RiskFindingRow {
	t.Helper()

	row := chOverviewFinding(t, projectID, orgID, chatID, msgID, createdAt, source, ruleID, externalUserID)
	row.RiskPolicyID = policyID
	row.MessageCreatedAt = messageCreatedAt
	row.MatchRedacted = matchRedacted
	row.FingerprintTenantHS256 = tenantFingerprint
	row.AssistantID = assistantID
	return row
}

// TestListRiskResults_ClickHousePageOrderingAndRedaction drives the flagged
// ClickHouse listing end to end: event-time ordering, cursor pagination,
// store-side redaction passthrough (no re-derivation from the nil match),
// Postgres display enrichment, and the read filters that hide redelivered
// duplicates, excluded rows, dead-letter sentinels, post-hoc false positives
// and foreign tenants.
func TestListRiskResults_ClickHousePageOrderingAndRedaction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Page")})
	require.NoError(t, err)

	// Relative times: the table's 90-day created_at TTL expires hardcoded
	// dates at insert once the calendar catches up.
	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)

	oldChat, oldMsg := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	midChat, midMsg := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	newChat, newMsg := seedChatWithUser(t, ti, projectID, orgID, "bob@example.com")

	oldest := chListFinding(t, projectID, orgID, oldChat, oldMsg, policy.ID, base.Add(4*time.Hour), base.Add(1*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=aaaaaaaa>", "fp-oldest", "")
	middle := chListFinding(t, projectID, orgID, midChat, midMsg, policy.ID, base.Add(4*time.Hour), base.Add(2*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=bbbbbbbb>", "fp-middle", "")
	newest := chListFinding(t, projectID, orgID, newChat, newMsg, policy.ID, base.Add(4*time.Hour), base.Add(3*time.Hour), "presidio", "pii.email_address", "bob@example.com", "<redacted len=9 sha=cccccccc>", "fp-newest", "")

	rows := []chrepo.RiskFindingRow{oldest, middle, newest}
	// Redelivered duplicate id: the id dedup must keep one copy.
	rows = append(rows, newest)

	// Filtered out at read time: excluded, dead-letter, foreign tenant.
	excludedAt := base.Add(3 * time.Hour)
	excluded := chListFinding(t, projectID, orgID, oldChat, oldMsg, policy.ID, base.Add(4*time.Hour), base.Add(90*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=dddddddd>", "fp-excluded", "")
	excluded.ExcludedAt = &excludedAt
	exclusionID := uuid.Must(uuid.NewV7())
	excluded.ExclusionID = &exclusionID
	rows = append(rows, excluded)

	deadLetter := chListFinding(t, projectID, orgID, oldChat, oldMsg, policy.ID, base.Add(4*time.Hour), base.Add(95*time.Minute), "gitleaks", "", "alice@example.com", "", "", "")
	deadLetter.DeadLetterReason = "could-not-analyze"
	deadLetter.Category = ""
	rows = append(rows, deadLetter)

	foreign := chListFinding(t, uuid.New(), "org_"+uuid.NewString(), oldChat, oldMsg, policy.ID, base.Add(4*time.Hour), base.Add(100*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=eeeeeeee>", "fp-foreign", "")
	rows = append(rows, foreign)

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, rows))

	// Post-hoc false positive: inserted directly with false_positive_at set.
	require.NoError(t, ti.chConn.Exec(ctx, `
		INSERT INTO risk_findings (id, created_at, message_created_at, organization_id, project_id, risk_policy_id, rule_id, source, category, chat_id, false_positive_at)
		VALUES (?, ?, ?, ?, ?, ?, 'secret.github_pat', 'gitleaks', 'secrets', ?, ?)
	`, uuid.Must(uuid.NewV7()), base.Add(4*time.Hour), base.Add(110*time.Minute), orgID, projectID.String(), policy.ID, oldChat.String(), base.Add(5*time.Hour)))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	pageSize := 2
	page1, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{Limit: &pageSize})
	require.NoError(t, err)
	require.Equal(t, int64(3), page1.TotalCount)
	require.Len(t, page1.Results, 2)
	require.NotNil(t, page1.NextCursor)

	first, second := page1.Results[0], page1.Results[1]
	require.Equal(t, newest.ID.String(), first.ID)
	require.Equal(t, middle.ID.String(), second.ID)

	// Store-side redaction passes through untouched; raw content never
	// materializes on this path.
	require.Nil(t, first.Match)
	require.Nil(t, first.Spans)
	require.NotNil(t, first.MatchRedacted)
	require.Equal(t, "<redacted len=9 sha=cccccccc>", *first.MatchRedacted)
	require.Equal(t, "<redacted len=7 sha=bbbbbbbb>", *second.MatchRedacted)

	// CreatedAt carries the message event time (the sort key), matching the
	// Postgres listing, not the ClickHouse scan time.
	require.Equal(t, newest.MessageCreatedAt.UTC().Format(time.RFC3339), first.CreatedAt)

	// Postgres display enrichment.
	require.NotNil(t, first.ChatTitle)
	require.Equal(t, "test chat", *first.ChatTitle)
	require.NotNil(t, first.UserID)
	require.Equal(t, "bob@example.com", *first.UserID)
	require.Equal(t, policy.ID, first.PolicyID)

	page2, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{Limit: &pageSize, Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)
	require.Equal(t, oldest.ID.String(), page2.Results[0].ID)
	require.Nil(t, page2.NextCursor)
	require.Equal(t, int64(3), page2.TotalCount)
}

// TestListRiskResults_ClickHouseLegacyFalsePositiveOnlyRowIsHidden pins the
// live listing's transitional suppression semantics. A dismissal written before
// the suppression convergence carries only the legacy false_positive_at, and no
// backfill is coming to give it an excluded_at copy — such rows simply stop
// being written once the converged writers deploy, then expire under the
// table's 90-day TTL. Until that window closes the listing must keep honoring
// the legacy column, so a false-positive-only row stays hidden here exactly
// like a converged one.
func TestListRiskResults_ClickHouseLegacyFalsePositiveOnlyRowIsHidden(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Legacy FP")})
	require.NoError(t, err)

	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	dismissedAt := base.Add(2 * time.Hour)

	legacyFP := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(4*time.Hour), base.Add(time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=aaaaaaaa>", "fp-legacy", "")
	legacyFP.FalsePositiveAt = &dismissedAt

	// The converged shape of the same dismissal, hidden by excluded_at.
	converged := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(4*time.Hour), base.Add(90*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=bbbbbbbb>", "fp-converged", "")
	converged.ExcludedAt = &dismissedAt
	converged.ExcludedReason = chrepo.ExcludedReasonManual
	converged.FalsePositiveAt = &dismissedAt

	// An untouched finding, so the assertion distinguishes "both suppressed
	// rows are hidden" from "the listing returned nothing at all".
	open := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(4*time.Hour), base.Add(30*time.Minute), "gitleaks", "secret.slack_token", "alice@example.com", "<redacted len=6 sha=cccccccc>", "fp-open", "")

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{legacyFP, converged, open}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	page, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	ids := make([]string, 0, len(page.Results))
	for _, r := range page.Results {
		ids = append(ids, r.ID)
	}
	require.Equal(t, []string{open.ID.String()}, ids, "both the legacy and the converged dismissal stay hidden")
	require.Equal(t, int64(1), page.TotalCount)
}

// TestListRiskResults_ClickHouseFilters exercises the pushed-down filters:
// category, rule and user substrings, assistant scoping, the enabled-policy
// pushdown (disabled policies hidden by default, surfaced by an explicit
// filter), and unique-match dedup on the tenant fingerprint with the
// empty-fingerprint fallback.
func TestListRiskResults_ClickHouseFilters(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Filters")})
	require.NoError(t, err)
	disabledPolicy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Filters Disabled"), Enabled: new(false)})
	require.NoError(t, err)

	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)

	aliceChat, aliceMsg := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	bobChat, bobMsg := seedChatWithUser(t, ti, projectID, orgID, "bob@example.com")

	assistantID := uuid.Must(uuid.NewV7()).String()

	secretAssistant := chListFinding(t, projectID, orgID, aliceChat, aliceMsg, policy.ID, base.Add(4*time.Hour), base.Add(1*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=11111111>", "fp-secret", assistantID)
	piiNoAssistant := chListFinding(t, projectID, orgID, bobChat, bobMsg, policy.ID, base.Add(4*time.Hour), base.Add(2*time.Hour), "presidio", "pii.email_address", "bob@example.com", "<redacted len=9 sha=22222222>", "fp-pii", "")
	disabledPolicyRow := chListFinding(t, projectID, orgID, aliceChat, aliceMsg, disabledPolicy.ID, base.Add(4*time.Hour), base.Add(3*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=33333333>", "fp-disabled", "")

	// Unique-match trio on the enabled policy: two occurrences of the same
	// fingerprint (newest wins) plus two fingerprint-less rows that must both
	// survive the dedup individually.
	dupOld := chListFinding(t, projectID, orgID, aliceChat, aliceMsg, policy.ID, base.Add(4*time.Hour), base.Add(30*time.Minute), "gitleaks", "secret.aws_secret_key", "alice@example.com", "<redacted len=5 sha=44444444>", "fp-dup", "")
	dupNew := chListFinding(t, projectID, orgID, aliceChat, aliceMsg, policy.ID, base.Add(4*time.Hour), base.Add(40*time.Minute), "gitleaks", "secret.aws_secret_key", "alice@example.com", "<redacted len=5 sha=55555555>", "fp-dup", "")
	noFp1 := chListFinding(t, projectID, orgID, aliceChat, aliceMsg, policy.ID, base.Add(4*time.Hour), base.Add(50*time.Minute), "gitleaks", "secret.slack_token", "alice@example.com", "<redacted len=6 sha=66666666>", "", "")
	noFp2 := chListFinding(t, projectID, orgID, aliceChat, aliceMsg, policy.ID, base.Add(4*time.Hour), base.Add(55*time.Minute), "gitleaks", "secret.slack_token", "alice@example.com", "<redacted len=6 sha=77777777>", "", "")

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{secretAssistant, piiNoAssistant, disabledPolicyRow, dupOld, dupNew, noFp1, noFp2}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	ids := func(result *gen.ListRiskResultsResult) []string {
		out := make([]string, 0, len(result.Results))
		for _, r := range result.Results {
			out = append(out, r.ID)
		}
		return out
	}

	// Default view: disabled policy's row hidden by the policy pushdown.
	all, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.NotContains(t, ids(all), disabledPolicyRow.ID.String())
	require.Len(t, all.Results, 6)

	// Explicit policy filter surfaces the disabled policy's history.
	byDisabled, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &disabledPolicy.ID})
	require.NoError(t, err)
	require.Equal(t, []string{disabledPolicyRow.ID.String()}, ids(byDisabled))

	// Category filter uses the ingest-stamped category column.
	pii, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{Category: new("pii")})
	require.NoError(t, err)
	require.Equal(t, []string{piiNoAssistant.ID.String()}, ids(pii))

	// Rule substring, case-insensitive.
	slack, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{RuleID: new("SLACK")})
	require.NoError(t, err)
	require.Len(t, slack.Results, 2)

	// User substring against the denormalized external user id.
	bob, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{UserID: new("BOB@")})
	require.NoError(t, err)
	require.Equal(t, []string{piiNoAssistant.ID.String()}, ids(bob))

	// Assistant scoping, both directions.
	byAssistant, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{AssistantID: &assistantID})
	require.NoError(t, err)
	require.Equal(t, []string{secretAssistant.ID.String()}, ids(byAssistant))

	nonAssistant, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{NonAssistant: new(true)})
	require.NoError(t, err)
	require.NotContains(t, ids(nonAssistant), secretAssistant.ID.String())
	require.Len(t, nonAssistant.Results, 5)

	// Unique match: fp-dup collapses to its newest occurrence; the two
	// fingerprint-less rows stay individually visible.
	unique, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{UniqueMatch: new(true)})
	require.NoError(t, err)
	uniqueIDs := ids(unique)
	require.Contains(t, uniqueIDs, dupNew.ID.String())
	require.NotContains(t, uniqueIDs, dupOld.ID.String())
	require.Contains(t, uniqueIDs, noFp1.ID.String())
	require.Contains(t, uniqueIDs, noFp2.ID.String())
	require.Len(t, uniqueIDs, 5)
}

// TestListRiskResults_ClickHouseUniqueMatchPagination guards the
// dedup-then-paginate order: the unique-match dedup must run over the full
// filtered set before the cursor applies, so a fingerprint group whose newest
// occurrence fell behind the cursor cannot reappear on a later page via an
// older occurrence winning the dedup.
func TestListRiskResults_ClickHouseUniqueMatchPagination(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Unique Pages")})
	require.NoError(t, err)

	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	// Same fingerprint straddling the page boundary: the newest occurrence
	// ends page one, so page two's cursor lands between the two occurrences
	// and only the dedup-before-cursor order keeps the older one hidden.
	dupNew := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(4*time.Hour), base.Add(3*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=aaaaaaaa>", "fp-page", "")
	other := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(4*time.Hour), base.Add(2*time.Hour), "gitleaks", "secret.slack_token", "alice@example.com", "<redacted len=6 sha=bbbbbbbb>", "fp-other", "")
	dupOld := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, base.Add(4*time.Hour), base.Add(1*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=cccccccc>", "fp-page", "")

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{dupNew, other, dupOld}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	pageSize := 1
	var cursor *string
	var seen []string
	for range 5 {
		page, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{Limit: &pageSize, Cursor: cursor, UniqueMatch: new(true)})
		require.NoError(t, err)
		for _, r := range page.Results {
			seen = append(seen, r.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	require.Equal(t, []string{dupNew.ID.String(), other.ID.String()}, seen)
}

// TestListRiskResults_ClickHouseRetroFlagCopies guards the dedup-before-state
// order of the listing: exclusion and false-positive flags change by
// appending a NEWER copy of a row (the retroactive reconcile, the
// false-positive mirror), so the listing must resolve each id to its latest
// copy before gating on the flags. Filtering first would drop the flagged
// copy, let the stale live copy win the dedup, and retro-hidden findings
// would keep showing on the list while overview and signals already hide
// them.
// TestListRiskResults_ClickHouseRedeliveryCannotUndoDismissal pins the
// event-kind ranking end to end: after a manual dismissal appends its
// suppression copy, a redelivered scanner copy of the same finding lands with
// a fresher inserted_at but must not win the per-id dedup — the finding stays
// off the live listing and on the Dismissed tab.
func TestListRiskResults_ClickHouseRedeliveryCannotUndoDismissal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Redelivery")})
	require.NoError(t, err)

	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	createdAt := base.Add(4 * time.Hour)
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	finding := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=aaaaaaaa>", "fp-1", "")
	finding.EventKind = chrepo.EventKindFinding
	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))

	// The manual dismissal appends its suppression copy.
	suppressed := finding
	dismissedAt := createdAt.Add(time.Minute)
	suppressed.ExcludedAt = &dismissedAt
	suppressed.FalsePositiveAt = &dismissedAt
	suppressed.ExcludedReason = chrepo.ExcludedReasonManual
	suppressed.EventKind = chrepo.EventKindSuppression
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{suppressed}))

	// The redelivered scanner copy: same id, fresher inserted_at, no
	// suppression stamps.
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	page, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, page.Results, "the suppression copy outranks the redelivered scanner copy")
	require.Zero(t, page.TotalCount)

	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, dismissed.Results, 1)
	require.Equal(t, finding.ID.String(), dismissed.Results[0].ID)
}

func TestListRiskResults_ClickHouseRetroFlagCopies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("List CH Retro Copies")})
	require.NoError(t, err)

	base := time.Now().UTC().AddDate(0, 0, -7).Truncate(time.Hour)
	createdAt := base.Add(4 * time.Hour)
	day := createdAt.Truncate(24 * time.Hour)
	scope := chrepo.RetroExclusionScope{
		OrganizationID: orgID,
		ProjectID:      projectID.String(),
		DayStart:       day,
		DayEnd:         day.AddDate(0, 0, 1),
	}
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	keep := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(30*time.Minute), "presidio", "pii.email_address", "alice@example.com", "<redacted len=9 sha=aaaaaaaa>", "fp-keep", "")
	retroHidden := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(1*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com", "<redacted len=7 sha=bbbbbbbb>", "fp-hidden", "")
	fpLater := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(3*time.Hour), "gitleaks", "secret.aws_secret_key", "alice@example.com", "<redacted len=5 sha=dddddddd>", "fp-fp", "")

	// Annotated at ingest; the retro reversal below appends its un-flag copy.
	unhideExclusion := uuid.Must(uuid.NewV7())
	ingestExcluded := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(2*time.Hour), "gitleaks", "secret.slack_token", "alice@example.com", "<redacted len=6 sha=cccccccc>", "fp-unhide", "")
	ingestAt := createdAt.Add(time.Minute)
	ingestExcluded.ExcludedAt = &ingestAt
	ingestExcluded.ExclusionID = &unhideExclusion

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{keep, retroHidden, ingestExcluded, fpLater}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Retro apply hides retroHidden; retro reversal un-hides ingestExcluded.
	// Both go through the real append-copy mechanism (synchronous inserts).
	now := time.Now().UTC()
	exclusionID := uuid.Must(uuid.NewV7())
	require.NoError(t, chQueries.AppendRetroExclusionApply(ctx, scope, exclusionID,
		chrepo.FormatCHTime(now), chrepo.FormatCHTime(now.Add(time.Microsecond)), chrepo.RetroExclusionPredicate{
			PolicyID:           "",
			RuleID:             "secret.github_pat",
			Source:             "",
			TenantFingerprints: nil,
			RuleIDFilter:       "",
			SourceFilter:       "",
		}))
	require.NoError(t, chQueries.AppendRetroExclusionReversal(ctx, scope, unhideExclusion,
		chrepo.FormatCHTime(now.Add(2*time.Microsecond)), chrepo.BlanketReversal()))

	// False-positive mirror shape: a newer copy of the same id with
	// false_positive_at set (raw fixture insert, exempt from the no-raw-SQL
	// test rule; message_created_at must match the original so the id dedup's
	// inserted_at tiebreak decides).
	require.NoError(t, ti.chConn.Exec(ctx, `
		INSERT INTO risk_findings (id, created_at, message_created_at, organization_id, project_id, risk_policy_id, rule_id, source, category, chat_id, false_positive_at)
		VALUES (?, ?, ?, ?, ?, ?, 'secret.aws_secret_key', 'gitleaks', 'secrets', ?, ?)
	`, fpLater.ID, createdAt, fpLater.MessageCreatedAt, orgID, projectID.String(), policy.ID, chatID.String(), createdAt.Add(2*time.Minute)))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	page, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	var got []string
	for _, r := range page.Results {
		got = append(got, r.ID)
	}
	require.Equal(t, []string{ingestExcluded.ID.String(), keep.ID.String()}, got,
		"retro-hidden and fp-marked rows disappear; retro-un-hidden rows reappear")
	require.Equal(t, int64(2), page.TotalCount)

	// Unique match: when a group's newest occurrence is retro-hidden, the
	// older live occurrence represents the group again.
	uniqNew := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(3*time.Hour), "gitleaks", "secret.db_password", "alice@example.com", "<redacted len=8 sha=eeeeeeee>", "fp-uniq", "")
	uniqOld := chListFinding(t, projectID, orgID, chatID, msgID, policy.ID, createdAt, base.Add(1*time.Hour), "gitleaks", "secret.db_password", "alice@example.com", "<redacted len=8 sha=ffffffff>", "fp-uniq", "")
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{uniqNew, uniqOld}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	// Fresh timestamp: the flag copy must sort after the rows inserted above.
	flagAt := time.Now().UTC()
	require.NoError(t, chQueries.AppendRetroExclusionApplyByIDs(ctx, scope, exclusionID,
		chrepo.FormatCHTime(flagAt), chrepo.FormatCHTime(flagAt.Add(time.Microsecond)), []uuid.UUID{uniqNew.ID}))

	unique, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{UniqueMatch: new(true)})
	require.NoError(t, err)
	var uniqueIDs []string
	for _, r := range unique.Results {
		uniqueIDs = append(uniqueIDs, r.ID)
	}
	require.Contains(t, uniqueIDs, uniqOld.ID.String())
	require.NotContains(t, uniqueIDs, uniqNew.ID.String())
}
