package risk_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// retroDay returns a stable in-retention timestamp and its partition-day
// scope bounds for one tenant.
func retroDay(orgID, projectID string) (time.Time, chrepo.RetroExclusionScope) {
	createdAt := time.Now().UTC().AddDate(0, 0, -2).Truncate(time.Hour)
	day := createdAt.Truncate(24 * time.Hour)
	return createdAt, chrepo.RetroExclusionScope{
		OrganizationID: orgID,
		ProjectID:      projectID,
		DayStart:       day,
		DayEnd:         day.AddDate(0, 0, 1),
	}
}

// latestExclusionState reads the effective (latest-copy) exclusion flags for
// one finding id. Raw SQL on purpose: ClickHouse fixture reads are exempt
// from the no-raw-SQL test rule.
func latestExclusionState(t *testing.T, ti *testInstance, id uuid.UUID) (excluded bool, exclusionID string, falsePositive bool) {
	t.Helper()
	rows, err := ti.chConn.Query(t.Context(), `
		SELECT excluded_at IS NOT NULL, coalesce(toString(exclusion_id), ''), false_positive_at IS NOT NULL
		FROM risk_findings WHERE id = ? ORDER BY inserted_at DESC LIMIT 1
	`, id)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next(), "finding %s must exist", id)
	require.NoError(t, rows.Scan(&excluded, &exclusionID, &falsePositive))
	return excluded, exclusionID, falsePositive
}

// TestRetroExclusion_RuleIDApplyAndReverse walks the full retroactive cycle
// for a rule_id predicate: apply flags live rows (skipping dead-letter and
// rows held by another exclusion, preserving false-positive marks), re-runs
// append nothing, and reversal un-hides everything the exclusion held —
// including a row annotated at ingest time.
func TestRetroExclusion_RuleIDApplyAndReverse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	createdAt, scope := retroDay(orgID, projectID.String())

	exclusionID := uuid.Must(uuid.NewV7())
	otherExclusion := uuid.Must(uuid.NewV7())
	chat := uuid.Must(uuid.NewV7())
	msg := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }

	live1 := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt, "gitleaks", "secret.github_pat", "alice@example.com")
	live2 := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(time.Minute), "gitleaks", "secret.github_pat", "bob@example.com")

	fpMarked := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(2*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com")
	fpAt := createdAt.Add(3 * time.Minute)
	fpMarked.FalsePositiveAt = &fpAt

	deadLetter := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(4*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com")
	deadLetter.DeadLetterReason = "scan timeout"

	heldByOther := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(5*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com")
	otherAt := createdAt.Add(6 * time.Minute)
	heldByOther.ExcludedAt = &otherAt
	heldByOther.ExclusionID = &otherExclusion

	ingestExcluded := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(7*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com")
	ingestAt := createdAt.Add(8 * time.Minute)
	ingestExcluded.ExcludedAt = &ingestAt
	ingestExcluded.ExclusionID = &exclusionID

	otherRule := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(9*time.Minute), "gitleaks", "secret.aws_access_key", "alice@example.com")

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		live1, live2, fpMarked, deadLetter, heldByOther, ingestExcluded, otherRule,
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	predicate := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "secret.github_pat",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}

	// Apply: the two live rows plus the false-positive one; dead-letter and
	// the row held by another exclusion stay untouched.
	count, err := chQueries.CountRetroExclusionApply(ctx, scope, predicate)
	require.NoError(t, err)
	require.Equal(t, uint64(3), count)

	now := time.Now().UTC()
	require.NoError(t, chQueries.AppendRetroExclusionApply(ctx, scope, exclusionID,
		chrepo.FormatCHTime(now), chrepo.FormatCHTime(now.Add(time.Microsecond)), predicate))

	excluded, exID, fp := latestExclusionState(t, ti, live1.ID)
	require.True(t, excluded)
	require.Equal(t, exclusionID.String(), exID)
	require.False(t, fp)

	excluded, exID, fp = latestExclusionState(t, ti, fpMarked.ID)
	require.True(t, excluded, "false-positive rows are still flagged, like the Postgres apply")
	require.Equal(t, exclusionID.String(), exID)
	require.True(t, fp, "the copy preserves false_positive_at verbatim")

	excluded, exID, _ = latestExclusionState(t, ti, heldByOther.ID)
	require.True(t, excluded)
	require.Equal(t, otherExclusion.String(), exID, "rows held by another exclusion are never re-flagged")

	excluded, _, _ = latestExclusionState(t, ti, deadLetter.ID)
	require.False(t, excluded, "dead-letter rows never match")

	excluded, _, _ = latestExclusionState(t, ti, otherRule.ID)
	require.False(t, excluded)

	// Idempotency: everything matching is already flagged.
	count, err = chQueries.CountRetroExclusionApply(ctx, scope, predicate)
	require.NoError(t, err)
	require.Zero(t, count)

	// Reversal (blanket — the disable/delete path): the three applied rows
	// plus the ingest-annotated one.
	count, err = chQueries.CountRetroExclusionReversal(ctx, scope, exclusionID, chrepo.BlanketReversal())
	require.NoError(t, err)
	require.Equal(t, uint64(4), count)

	require.NoError(t, chQueries.AppendRetroExclusionReversal(ctx, scope, exclusionID,
		chrepo.FormatCHTime(now.Add(2*time.Microsecond)), chrepo.BlanketReversal()))

	for _, id := range []uuid.UUID{live1.ID, live2.ID, ingestExcluded.ID} {
		excluded, _, _ = latestExclusionState(t, ti, id)
		require.False(t, excluded, "reversal un-hides %s, including ingest-annotated rows", id)
	}
	excluded, _, fp = latestExclusionState(t, ti, fpMarked.ID)
	require.False(t, excluded)
	require.True(t, fp, "reversal keeps the false-positive mark")

	excluded, exID, _ = latestExclusionState(t, ti, heldByOther.ID)
	require.True(t, excluded)
	require.Equal(t, otherExclusion.String(), exID, "reversal leaves other exclusions' rows alone")

	count, err = chQueries.CountRetroExclusionReversal(ctx, scope, exclusionID, chrepo.BlanketReversal())
	require.NoError(t, err)
	require.Zero(t, count)
}

// TestRetroExclusion_FiltersAndPolicyScope pins the contract that rule and
// source filters apply to every match type (the scan-time ExclusionSet
// semantics) and that policy scoping mirrors Postgres: a policy-bound
// exclusion only touches that policy's findings, while a global exclusion
// touches everything including rows with no policy attribution.
func TestRetroExclusion_FiltersAndPolicyScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	createdAt, scope := retroDay(orgID, projectID.String())

	policyP := uuid.NewString()
	policyQ := uuid.NewString()
	chat := uuid.Must(uuid.NewV7())
	msg := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }

	inPolicyP := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt, "gitleaks", "secret.github_pat", "alice@example.com")
	inPolicyP.RiskPolicyID = policyP
	wrongSource := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(time.Minute), "presidio", "secret.github_pat", "alice@example.com")
	wrongSource.RiskPolicyID = policyP
	inPolicyQ := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(2*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com")
	inPolicyQ.RiskPolicyID = policyQ
	noPolicy := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(3*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com")

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{inPolicyP, wrongSource, inPolicyQ, noPolicy}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Policy-bound + source filter: exactly the one row in P from gitleaks.
	count, err := chQueries.CountRetroExclusionApply(ctx, scope, chrepo.RetroExclusionPredicate{
		PolicyID:           policyP,
		RuleID:             "secret.github_pat",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "gitleaks",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)

	// Global: every policy including unattributed rows.
	count, err = chQueries.CountRetroExclusionApply(ctx, scope, chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "secret.github_pat",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	})
	require.NoError(t, err)
	require.Equal(t, uint64(4), count)
}

// TestRetroExclusion_ExactFingerprints matches an exact-value exclusion
// against fingerprints computed under every pepper version, so rows written
// before a rotation still match; rows without fingerprints never do.
func TestRetroExclusion_ExactFingerprints(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	createdAt, scope := retroDay(orgID, projectID.String())

	fingerprinter, err := risk.ParsePepperKeyRing(keyRingJSON(t, "v2", map[string][]byte{
		"v1": []byte("retired-pepper-key-material-0001"),
		"v2": []byte("current-pepper-key-material-0002"),
	}))
	require.NoError(t, err)

	secret := "AKIAIOSFODNN7EXAMPLE"
	fpV1, err := fingerprinter.TenantedHS256WithVersion("v1", orgID, []byte(secret))
	require.NoError(t, err)
	fpV2, err := fingerprinter.TenantedHS256WithVersion("v2", orgID, []byte(secret))
	require.NoError(t, err)

	chat := uuid.Must(uuid.NewV7())
	msg := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }

	oldPepper := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt, "gitleaks", "secret.aws_access_key", "alice@example.com")
	oldPepper.FingerprintTenantHS256 = risk.EncodeFingerprint(fpV1)
	newPepper := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(time.Minute), "gitleaks", "secret.aws_access_key", "alice@example.com")
	newPepper.FingerprintTenantHS256 = risk.EncodeFingerprint(fpV2)
	noFingerprint := chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(2*time.Minute), "llm_judge", "judge.verdict", "alice@example.com")

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{oldPepper, newPepper, noFingerprint}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	predicate := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: []string{risk.EncodeFingerprint(fpV1), risk.EncodeFingerprint(fpV2)},
		RuleIDFilter:       "",
		SourceFilter:       "",
	}
	count, err := chQueries.CountRetroExclusionApply(ctx, scope, predicate)
	require.NoError(t, err)
	require.Equal(t, uint64(2), count, "both pepper generations match; the fingerprint-less row cannot")
}

// TestRetroExclusion_RegexReconstruction exercises the regex path end to end
// at the repository level: candidates carry the reveal metadata, the shared
// RevealMatcher reconstructs the plaintext from the Postgres chat store, and
// the matched ids are flagged by id with the usual idempotency.
func TestRetroExclusion_RegexReconstruction(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "AKIAIOSFODNN7EXAMPLE"
	content := "please rotate " + secret + " before the audit"
	start := strings.Index(content, secret)

	pgRepo := riskrepo.New(ti.conn)
	chatID, err := pgRepo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := pgRepo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	reconstructable := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		ruleID:        "secret.aws_access_key",
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		matchRedacted: "AKIA**************LE",
		surface:       "content",
	})
	// A judge-style row without match metadata must not even be a candidate.
	insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		ruleID:        "judge.verdict",
		surface:       "none",
	})
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// The unmask fixture writes rows two days back.
	_, scope := retroDay(orgID, projectID.String())

	chQueries := chrepo.New(ti.chConn)
	candidates, err := chQueries.ListRetroRegexCandidates(ctx, scope, chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}, uuid.Nil, 100)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "only rows with reconstructable match metadata are candidates")
	require.Equal(t, reconstructable, candidates[0].ID)

	// Reconstruct and match exactly as the reconcile activity does: load the
	// anchor, resolve the chat it is attributed to, hydrate, then gate the
	// candidates on the recorded match length.
	reveal := risk.NewRevealMatcher(testenv.NewLogger(t), pgRepo, nil)
	c := candidates[0]
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
		OrganizationID: orgID,
	}
	anchor := reveal.LoadAnchor(ctx, projectID, row)
	resolvedChatID, attributed := risk.ResolveChatID(row, anchor)
	require.True(t, attributed)
	require.Equal(t, chatID, resolvedChatID)
	reveal.HydratePartContent(ctx, &anchor)

	match, ok := risk.MatchingReconstruction(row.MatchLen, reveal.Candidates(ctx, resolvedChatID, row, anchor))
	require.True(t, ok)
	require.Equal(t, secret, match)
	require.True(t, regexp.MustCompile(`^AKIA[0-9A-Z]{16}$`).MatchString(match))

	exclusionID := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	require.NoError(t, chQueries.AppendRetroExclusionApplyByIDs(ctx, scope, exclusionID,
		chrepo.FormatCHTime(now), chrepo.FormatCHTime(now.Add(time.Microsecond)), []uuid.UUID{c.ID}))

	excluded, exID, _ := latestExclusionState(t, ti, c.ID)
	require.True(t, excluded)
	require.Equal(t, exclusionID.String(), exID)

	// Flagged rows drop out of the next candidate listing (idempotency).
	candidates, err = chQueries.ListRetroRegexCandidates(ctx, scope, chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}, uuid.Nil, 100)
	require.NoError(t, err)
	require.Empty(t, candidates)

	// The flagged row is now a REVERSAL candidate for this exclusion, and a
	// by-id reversal un-hides it again.
	held, err := chQueries.ListRetroRegexReversalCandidates(ctx, scope, exclusionID, uuid.Nil, 100)
	require.NoError(t, err)
	require.Len(t, held, 1)
	require.Equal(t, c.ID, held[0].ID)

	require.NoError(t, chQueries.AppendRetroExclusionReversalByIDs(ctx, scope, exclusionID,
		chrepo.FormatCHTime(now.Add(2*time.Microsecond)), []uuid.UUID{c.ID}))
	excluded, _, _ = latestExclusionState(t, ti, c.ID)
	require.False(t, excluded)
	held, err = chQueries.ListRetroRegexReversalCandidates(ctx, scope, exclusionID, uuid.Nil, 100)
	require.NoError(t, err)
	require.Empty(t, held)
}

// TestRetroExclusion_KeepGuardedReversal pins the reversal semantics for an
// ACTIVE exclusion: only held rows that provably no longer match are
// un-flagged. Rows still matching stay held with no copy churn, and — for
// exact matching — rows with no stored fingerprint stay held too, because
// nothing can be proven about them and the fingerprint-based apply could
// never re-flag them.
func TestRetroExclusion_KeepGuardedReversal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID
	createdAt, scope := retroDay(orgID, projectID.String())

	exclusionID := uuid.Must(uuid.NewV7())
	chat := uuid.Must(uuid.NewV7())
	msg := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	heldAt := createdAt.Add(30 * time.Minute)

	hold := func(row chrepo.RiskFindingRow) chrepo.RiskFindingRow {
		row.ExcludedAt = &heldAt
		row.ExclusionID = &exclusionID
		return row
	}

	stillMatching := hold(chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt, "gitleaks", "secret.github_pat", "alice@example.com"))
	staleRule := hold(chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(time.Minute), "gitleaks", "secret.aws_access_key", "alice@example.com"))

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{stillMatching, staleRule}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	keep, err := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "secret.github_pat",
		Source:             "",
		TenantFingerprints: nil,
		RuleIDFilter:       "",
		SourceFilter:       "",
	}.KeepMatching()
	require.NoError(t, err)

	count, err := chQueries.CountRetroExclusionReversal(ctx, scope, exclusionID, keep)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count, "only the row that no longer matches reverses")

	now := time.Now().UTC()
	require.NoError(t, chQueries.AppendRetroExclusionReversal(ctx, scope, exclusionID,
		chrepo.FormatCHTime(now), keep))

	excluded, exID, _ := latestExclusionState(t, ti, stillMatching.ID)
	require.True(t, excluded, "a still-matching held row is never exposed, even transiently")
	require.Equal(t, exclusionID.String(), exID)
	excluded, _, _ = latestExclusionState(t, ti, staleRule.ID)
	require.False(t, excluded)

	// Exact matching: the keep guard retains held rows with no stored
	// fingerprint alongside rows whose fingerprint is in the predicate set.
	exactExclusion := uuid.Must(uuid.NewV7())
	holdExact := func(row chrepo.RiskFindingRow, fp string) chrepo.RiskFindingRow {
		row.ExcludedAt = &heldAt
		row.ExclusionID = &exactExclusion
		row.FingerprintTenantHS256 = fp
		return row
	}
	matchingFp := holdExact(chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(2*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com"), "fp-current")
	noFp := holdExact(chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(3*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com"), "")
	staleFp := holdExact(chOverviewFinding(t, projectID, orgID, chat, msg(), createdAt.Add(4*time.Minute), "gitleaks", "secret.github_pat", "alice@example.com"), "fp-other")

	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{matchingFp, noFp, staleFp}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	exactKeep, err := chrepo.RetroExclusionPredicate{
		PolicyID:           "",
		RuleID:             "",
		Source:             "",
		TenantFingerprints: []string{"fp-current"},
		RuleIDFilter:       "",
		SourceFilter:       "",
	}.KeepMatching()
	require.NoError(t, err)

	count, err = chQueries.CountRetroExclusionReversal(ctx, scope, exactExclusion, exactKeep)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count, "only the provably non-matching fingerprint reverses")

	// Fresh timestamp: the copy must sort after the rows inserted above.
	require.NoError(t, chQueries.AppendRetroExclusionReversal(ctx, scope, exactExclusion,
		chrepo.FormatCHTime(time.Now().UTC()), exactKeep))

	excluded, _, _ = latestExclusionState(t, ti, matchingFp.ID)
	require.True(t, excluded)
	excluded, _, _ = latestExclusionState(t, ti, noFp.ID)
	require.True(t, excluded, "a held row with no fingerprint cannot be proven stale and stays hidden")
	excluded, _, _ = latestExclusionState(t, ti, staleFp.ID)
	require.False(t, excluded)
}

// TestRetroExclusion_RegexSkipsReparentedCandidate pins the cross-chat
// attribution guard that the reconcile's regex evaluation shares with the
// unmask endpoint: when the chat id stamped on a ClickHouse row disagrees with
// the chat its anchored message actually belongs to, the candidate is refused
// instead of matched against another chat's content.
func TestRetroExclusion_RegexSkipsReparentedCandidate(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "AKIAIOSFODNN7EXAMPLE"
	content := "please rotate " + secret + " before the audit"
	start := strings.Index(content, secret)

	pgRepo := riskrepo.New(ti.conn)
	anchorChat, err := pgRepo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	stampedChat, err := pgRepo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	// The anchored message lives in one chat while the finding row claims the
	// other — the shape a re-parented message leaves behind.
	msgID, err := pgRepo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: anchorChat, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	row := &chrepo.RiskFindingUnmaskRow{
		ID:             uuid.Must(uuid.NewV7()),
		ChatMessageID:  msgID.String(),
		ChatID:         stampedChat.String(),
		RuleID:         "secret.aws_access_key",
		StartPos:       int32(start),
		EndPos:         int32(start + len(secret)),
		MatchLen:       uint32(len(secret)),
		Surface:        "content",
		OrganizationID: orgID,
	}

	reveal := risk.NewRevealMatcher(testenv.NewLogger(t), pgRepo, nil)
	anchor := reveal.LoadAnchor(ctx, projectID, row)
	require.Equal(t, uuid.NullUUID{UUID: anchorChat, Valid: true}, anchor.ChatID)

	chatID, attributed := risk.ResolveChatID(row, anchor)
	require.False(t, attributed, "content from another chat must not be attributed to the stamped chat")
	require.Equal(t, uuid.Nil, chatID, "a refused attribution never yields a usable chat id")
}
