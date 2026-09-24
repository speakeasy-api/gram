package slackdirectoryconnections_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func mappingRequest(m *gen.SlackDirectoryMember, person *string) *gen.SetMappingPayload {
	return &gen.SetMappingPayload{SessionToken: nil, ID: m.ID, MappingRevision: m.MappingRevision, ObservationToken: m.ObservationToken, UserID: person}
}
func awaitMappingResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Slack mapping operation")
		return nil
	}
}
func readMapping(t *testing.T, ctx context.Context, f *fixture, id string) *gen.SlackDirectoryMember {
	t.Helper()
	m, err := f.service.GetMember(ctx, &gen.GetMemberPayload{SessionToken: nil, ID: id})
	require.NoError(t, err)
	return m
}
func mappingFixture(t *testing.T) (context.Context, *fixture, *gen.SlackDirectoryConnection, *gen.SlackDirectoryMember) {
	t.Helper()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, syncRequest(f, c), nil))
	return ctx, f, c, readMapping(t, ctx, f, members(t, ctx, f)[0].ID.String())
}
func addPerson(t *testing.T, ctx context.Context, f *fixture) string {
	t.Helper()
	id := "user_synthetic_" + uuid.NewString()
	require.NoError(t, repo.New(f.db).CreateSlackMappingPersonForTest(ctx, repo.CreateSlackMappingPersonForTestParams{OrganizationID: f.auth.ActiveOrganizationID, UserID: id}))
	return id
}
func TestMappingAssignmentHistoryRevisionAndAudit(t *testing.T) {
	t.Parallel()
	ctx, f, c, initial := mappingFixture(t)
	first, err := f.service.SetMapping(ctx, mappingRequest(initial, &f.auth.UserID))
	require.NoError(t, err)
	require.Equal(t, int64(1), first.MappingRevision)
	require.Equal(t, f.auth.UserID, first.Mapping.UserID)
	second, err := f.service.SetMapping(ctx, mappingRequest(first, &f.auth.UserID))
	require.NoError(t, err)
	require.Equal(t, first.Mapping.ID, second.Mapping.ID, "reconfirmation retains the continuous assignment")
	require.Equal(t, int64(2), second.MappingRevision)
	person := addPerson(t, ctx, f)
	reassigned, err := f.service.SetMapping(ctx, mappingRequest(second, &person))
	require.NoError(t, err)
	require.NotEqual(t, first.Mapping.ID, reassigned.Mapping.ID)
	old, err := repo.New(f.db).GetSlackMappingForTest(ctx, repo.GetSlackMappingForTestParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(first.Mapping.ID)})
	require.NoError(t, err)
	require.True(t, old.RevokedAt.Valid)
	require.Equal(t, f.auth.UserID, old.UserID)
	unmapped, err := f.service.SetMapping(ctx, mappingRequest(reassigned, nil))
	require.NoError(t, err)
	require.Nil(t, unmapped.Mapping)
	require.Equal(t, int64(4), unmapped.MappingRevision)
	_, err = f.service.SetMapping(ctx, mappingRequest(initial, &person))
	requireMappingCode(t, err, oops.CodeConflict)
	_, err = f.service.SetMapping(ctx, mappingRequest(unmapped, nil))
	requireMappingCode(t, err, oops.CodeConflict)
	remapped, err := f.service.SetMapping(ctx, mappingRequest(unmapped, &person))
	require.NoError(t, err)
	require.NotEqual(t, reassigned.Mapping.ID, remapped.Mapping.ID)
	_, err = f.service.SetMapping(ctx, mappingRequest(first, nil))
	requireMappingCode(t, err, oops.CodeConflict)
	for _, action := range []audit.Action{audit.ActionSlackIdentityMappingConfirm, audit.ActionSlackIdentityMappingReconfirm, audit.ActionSlackIdentityMappingReassign, audit.ActionSlackIdentityMappingUnmap} {
		count, err := audittest.AuditLogCountByAction(ctx, f.db, action)
		require.NoError(t, err)
		require.Positive(t, count)
	}
	latest, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionSlackIdentityMappingReassign)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(latest.AfterSnapshot)
	require.NoError(t, err)
	mapping, ok := after["Mapping"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, person, mapping["UserID"])
	require.EqualValues(t, reassigned.MappingRevision, after["MappingRevision"])
	// Multiple accounts, including accounts in the same workspace, may share a person.
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01", "UEXAMPLE02")).Run(ctx, syncRequest(f, c), nil))
	for _, row := range members(t, ctx, f) {
		if row.SlackUserID == "UEXAMPLE02" {
			_, err = f.service.SetMapping(ctx, mappingRequest(readMapping(t, ctx, f, row.ID.String()), &person))
			require.NoError(t, err)
		}
	}
}
func TestMappingBoundaries(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	p := mappingRequest(m, &f.auth.UserID)
	missingSession := *f.auth
	missingSession.SessionID = nil
	support := *f.auth
	support.SupportOrganizationID = support.ActiveOrganizationID
	support.IsAdmin = true
	other := *f.auth
	other.ActiveOrganizationID = "org_synthetic_other"
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &other), authz.NewGrant(authz.ScopeOrgAdmin, other.ActiveOrganizationID))
	for _, denied := range []context.Context{authztest.WithExactGrants(t, ctx), contextvalues.SetAuthContext(ctx, &missingSession), contextvalues.WithLegacyAPIKeyAuthorization(ctx, f.auth), contextvalues.WithValidatedSupportSession(ctx, &support), otherCtx} {
		_, err := f.service.SetMapping(denied, p)
		require.Error(t, err)
		_, err = f.service.GetMember(denied, &gen.GetMemberPayload{SessionToken: nil, ID: m.ID})
		require.Error(t, err)
	}
	for _, id := range []string{"nonexistent", addPerson(t, ctx, f)} {
		if id != "nonexistent" {
			require.NoError(t, repo.New(f.db).DeactivateSlackMappingPersonForTest(ctx, repo.DeactivateSlackMappingPersonForTestParams{OrganizationID: f.auth.ActiveOrganizationID, UserID: conv.ToPGText(id)}))
		}
		_, err := f.service.SetMapping(ctx, mappingRequest(m, &id))
		require.ErrorContains(t, err, "Choose an active person")
	}
}
func profileSnapshot(status, kind, email, name string) directoryFunc {
	return func(_ context.Context, _, _ string, _ func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		return []slackdirectoryconnections.DirectoryMember{{UserID: "UEXAMPLE01", DisplayName: name, Email: email, Status: status, MemberType: kind, UpdatedAt: nil}}, nil
	}
}
func checkMappingReview(t *testing.T, status, kind, email, reason string) {
	t.Helper()
	ctx, f, c, m := mappingFixture(t)
	m, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, syncRequest(f, c), nil))
	require.Equal(t, m.ObservationToken, readMapping(t, ctx, f, m.ID).ObservationToken, "no-op sync keeps dialog current")
	require.NoError(t, syncer(f, profileSnapshot(status, kind, email, "A changed display name")).Run(ctx, syncRequest(f, c), nil))
	changed := readMapping(t, ctx, f, m.ID)
	require.Equal(t, reason, conv.PtrValOr(changed.MappingConflictReason, ""))
	require.Equal(t, m.MappingRevision, changed.MappingRevision)
	require.Equal(t, m.Mapping.ID, changed.Mapping.ID)
	_, err = f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	requireMappingCode(t, err, oops.CodeConflict)
	if kind == "bot" {
		_, err = f.service.SetMapping(ctx, mappingRequest(changed, &f.auth.UserID))
		require.ErrorContains(t, err, "Bots and apps")
		_, err = f.service.SetMapping(ctx, mappingRequest(changed, nil))
		require.NoError(t, err)
		return
	}
	if reason != "" {
		require.NoError(t, syncer(f, profileSnapshot("deactivated", "person", email, "Another name")).Run(ctx, syncRequest(f, c), nil))
		sticky := readMapping(t, ctx, f, m.ID)
		require.Equal(t, changed.MappingConflictReason, sticky.MappingConflictReason)
		require.Equal(t, changed.MappingConflictDetectedAt, sticky.MappingConflictDetectedAt)
		changed = sticky
	}
	confirmed, err := f.service.SetMapping(ctx, mappingRequest(changed, &f.auth.UserID))
	require.NoError(t, err)
	require.Nil(t, confirmed.MappingConflictReason)
	require.Equal(t, m.Mapping.ID, confirmed.Mapping.ID)
}
func TestMappingDeactivationReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "deactivated", "person", "example@demo.getgram.ai", "member_deactivated")
}
func TestMappingUnknownStateReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "unknown", "person", "example@demo.getgram.ai", "member_unknown")
}
func TestMappingBotReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "active", "bot", "example@demo.getgram.ai", "member_became_bot")
}
func TestMappingUnknownTypeReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "active", "unknown", "example@demo.getgram.ai", "member_type_unknown")
}
func TestMappingEmailReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "active", "person", "changed@demo.getgram.ai", "email_changed")
}
func TestMappingMissingEmailNoReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "active", "person", "", "")
}
func TestMappingNormalizedEmailNoReview(t *testing.T) {
	t.Parallel()
	checkMappingReview(t, "active", "person", " EXAMPLE@demo.getgram.ai ", "")
}

func TestMappingAbsentReviewAndStaleHistory(t *testing.T) {
	t.Parallel()
	ctx, f, c, m := mappingFixture(t)
	m, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	require.NoError(t, syncer(f, snapshot()).Run(ctx, syncRequest(f, c), nil))
	absent := readMapping(t, ctx, f, m.ID)
	require.Equal(t, "member_absent", *absent.MappingConflictReason)
	require.False(t, absent.ObservedInLastSync)
	absent, err = f.service.SetMapping(ctx, mappingRequest(absent, &f.auth.UserID))
	require.NoError(t, err)
	require.NoError(t, syncer(f, snapshot()).Run(ctx, syncRequest(f, c), nil))
	require.Nil(t, readMapping(t, ctx, f, m.ID).MappingConflictReason, "repeated absence is not a new finding")
	_, err = f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.NoError(t, err)
	person := addPerson(t, ctx, f)
	corrected, err := f.service.SetMapping(ctx, mappingRequest(absent, &person))
	require.NoError(t, err)
	_, err = f.service.SetMapping(ctx, mappingRequest(corrected, nil))
	require.NoError(t, err)
}

// A fetched snapshot may finish after confirmation; publication must retain the
// assignment and record the newly observed conflict rather than clearing intent.
func TestMappingConfirmedDuringFetchIsReviewed(t *testing.T) {
	t.Parallel()
	ctx, f, c, m := mappingFixture(t)
	fetched, release := make(chan struct{}), make(chan struct{})
	provider := directoryFunc(func(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		close(fetched)
		<-release
		return profileSnapshot("deactivated", "person", "changed@demo.getgram.ai", "Changed")(ctx, token, team, report)
	})
	done := make(chan error, 1)
	go func() { done <- syncer(f, provider).Run(ctx, syncRequest(f, c), nil) }()
	select {
	case <-fetched:
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not start")
	}
	confirmed, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	close(release)
	require.NoError(t, awaitMappingResult(t, done))
	after := readMapping(t, ctx, f, m.ID)
	require.Equal(t, confirmed.Mapping.ID, after.Mapping.ID)
	require.Equal(t, confirmed.MappingRevision, after.MappingRevision)
	require.Equal(t, "member_deactivated", *after.MappingConflictReason)
}

func TestMappingAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	testenv.RejectWritesTo(t, ctx, f.db, "audit_logs")
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.Error(t, err)
	after := readMapping(t, ctx, f, m.ID)
	require.Equal(t, m.MappingRevision, after.MappingRevision)
	require.Nil(t, after.Mapping)
}

func TestMappingPublicationSeesCommittedConfirmation(t *testing.T) {
	t.Parallel()
	ctx, f, c, m := mappingFixture(t)
	tx := testenv.BeginTx(t, ctx, f.db)
	q := repo.New(tx)
	_, err := q.LockSlackDirectoryMembership(ctx, repo.LockSlackDirectoryMembershipParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(m.ID)})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- syncer(f, profileSnapshot("deactivated", "person", "example@demo.getgram.ai", "Changed")).Run(ctx, syncRequest(f, c), nil)
	}()
	testenv.WaitForBlockedBackend(t, ctx, f.db)
	require.NoError(t, q.ConfirmSlackIdentityMapping(ctx, repo.ConfirmSlackIdentityMappingParams{OrganizationID: f.auth.ActiveOrganizationID, SlackTeamID: m.WorkspaceID, SlackUserID: m.SlackUserID, UserID: f.auth.UserID}))
	require.NoError(t, q.AdvanceSlackMappingRevision(ctx, repo.AdvanceSlackMappingRevisionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(m.ID)}))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, awaitMappingResult(t, done))
	after := readMapping(t, ctx, f, m.ID)
	require.Equal(t, int64(1), after.MappingRevision)
	require.Equal(t, f.auth.UserID, after.Mapping.UserID)
	require.Equal(t, "member_deactivated", *after.MappingConflictReason)
}

func TestMappingPublicationSeesCommittedUnmap(t *testing.T) {
	t.Parallel()
	ctx, f, c, m := mappingFixture(t)
	m, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, f.db)
	q := repo.New(tx)
	_, err = q.LockSlackDirectoryMembership(ctx, repo.LockSlackDirectoryMembershipParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(m.ID)})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- syncer(f, profileSnapshot("deactivated", "person", "example@demo.getgram.ai", "Changed")).Run(ctx, syncRequest(f, c), nil)
	}()
	testenv.WaitForBlockedBackend(t, ctx, f.db)
	require.NoError(t, q.RevokeSlackIdentityMapping(ctx, repo.RevokeSlackIdentityMappingParams{OrganizationID: f.auth.ActiveOrganizationID, SlackTeamID: m.WorkspaceID, SlackUserID: m.SlackUserID}))
	require.NoError(t, q.AdvanceSlackMappingRevision(ctx, repo.AdvanceSlackMappingRevisionParams{OrganizationID: f.auth.ActiveOrganizationID, ID: uuid.MustParse(m.ID)}))
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, awaitMappingResult(t, done))
	after := readMapping(t, ctx, f, m.ID)
	require.Nil(t, after.Mapping)
	require.Nil(t, after.MappingConflictReason)
	require.Equal(t, int64(2), after.MappingRevision)
}

func TestMappingWaitsForTargetDeactivation(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	person := addPerson(t, ctx, f)
	tx := testenv.BeginTx(t, ctx, f.db)
	q := repo.New(tx)
	require.NoError(t, q.DeactivateSlackMappingPersonForTest(ctx, repo.DeactivateSlackMappingPersonForTestParams{OrganizationID: f.auth.ActiveOrganizationID, UserID: conv.ToPGText(person)}))
	done := make(chan error, 1)
	go func() { _, err := f.service.SetMapping(ctx, mappingRequest(m, &person)); done <- err }()
	testenv.WaitForBlockedBackend(t, ctx, f.db)
	require.NoError(t, tx.Commit(ctx))
	require.ErrorContains(t, awaitMappingResult(t, done), "Choose an active person")
	require.Nil(t, readMapping(t, ctx, f, m.ID).Mapping)
}

func TestMappingStatusFiltersMatchCounts(t *testing.T) {
	t.Parallel()
	ctx, f, c, m := mappingFixture(t)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	p := memberRequest()
	p.MappingStatus = conv.PtrEmpty("mapped")
	result, err := f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.Members, 1)
	require.NoError(t, syncer(f, snapshot()).Run(ctx, syncRequest(f, c), nil))
	result, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Zero(t, result.Total)
	require.Empty(t, result.Members)
	p.MappingStatus = conv.PtrEmpty("needs_review")
	result, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.Members, 1)
	_, err = f.service.SetMapping(ctx, mappingRequest(result.Members[0], nil))
	require.NoError(t, err)
	p.MappingStatus = conv.PtrEmpty("unmapped")
	result, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Nil(t, result.Members[0].Mapping)
}

func requireMappingCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var result *oops.ShareableError
	require.ErrorAs(t, err, &result)
	require.Equal(t, code, result.Code)
}

func TestMappingRejectsExistingForeignPersonAndDeletedUser(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	foreign := "user_synthetic_foreign"
	emptyTime := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	require.NoError(t, testrepo.New(f.db).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{ID: "org_synthetic_foreign", Name: "Synthetic foreign org", Slug: "synthetic-foreign", GramAccountType: "enterprise", WorkosID: pgtype.Text{String: "", Valid: false}, Whitelisted: false, FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite}, FreeTrialEndsAt: pgtype.Timestamptz{Time: time.Now().Add(14 * 24 * time.Hour), Valid: true, InfinityModifier: pgtype.Finite}, DisabledAt: emptyTime, CreatedAt: emptyTime}))
	require.NoError(t, testrepo.New(f.db).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: foreign, Email: "example@demo.getgram.ai", DisplayName: "Synthetic foreign person"}))
	require.NoError(t, testrepo.New(f.db).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: "org_synthetic_foreign", UserID: conv.ToPGText(foreign)}))
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &foreign))
	require.ErrorContains(t, err, "Choose an active person")
	deleted := addPerson(t, ctx, f)
	require.NoError(t, repo.New(f.db).DeleteSlackMappingUserForTest(ctx, deleted))
	_, err = f.service.SetMapping(ctx, mappingRequest(m, &deleted))
	require.ErrorContains(t, err, "Choose an active person")
	require.Nil(t, readMapping(t, ctx, f, m.ID).Mapping)
}

func TestMappingOnePersonAcrossWorkspaces(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	first, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE02")
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, syncRequest(f, c), nil))
	p := memberRequest()
	p.ConnectionID = &c.ID
	rows, err := f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Len(t, rows.Members, 1)
	second, err := f.service.SetMapping(ctx, mappingRequest(rows.Members[0], &f.auth.UserID))
	require.NoError(t, err)
	require.Equal(t, first.Mapping.UserID, second.Mapping.UserID)
	require.NotEqual(t, first.Mapping.ID, second.Mapping.ID)
}

func TestMappingUnmapAuditFailurePreservesAssignment(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	m, err := f.service.SetMapping(ctx, mappingRequest(m, &f.auth.UserID))
	require.NoError(t, err)
	testenv.RejectWritesTo(t, ctx, f.db, "audit_logs")
	_, err = f.service.SetMapping(ctx, mappingRequest(m, nil))
	require.Error(t, err)
	after := readMapping(t, ctx, f, m.ID)
	require.Equal(t, m.MappingRevision, after.MappingRevision)
	require.Equal(t, m.Mapping.ID, after.Mapping.ID)
}

func TestMappingInactivePersonRemainsVisibleForReview(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	person := addPerson(t, ctx, f)
	m, err := f.service.SetMapping(ctx, mappingRequest(m, &person))
	require.NoError(t, err)
	require.NoError(t, repo.New(f.db).DeactivateSlackMappingPersonForTest(ctx, repo.DeactivateSlackMappingPersonForTestParams{OrganizationID: f.auth.ActiveOrganizationID, UserID: conv.ToPGText(person)}))
	current := readMapping(t, ctx, f, m.ID)
	require.False(t, current.Mapping.Active)
	require.Equal(t, "needs_review", current.MappingStatus)
	p := memberRequest()
	p.MappingStatus = conv.PtrEmpty("needs_review")
	rows, err := f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(1), rows.Total)
	require.Len(t, rows.Members, 1)
	_, err = f.service.SetMapping(ctx, mappingRequest(current, nil))
	require.NoError(t, err)
}

func TestMappingGetForeignMembershipReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	other := *f.auth
	other.ActiveOrganizationID = "org_synthetic_other"
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &other), authz.NewGrant(authz.ScopeOrgAdmin, other.ActiveOrganizationID))
	_, err := f.service.GetMember(otherCtx, &gen.GetMemberPayload{SessionToken: nil, ID: m.ID})
	requireMappingCode(t, err, oops.CodeNotFound)
}

func TestSharedDemoRejectsMappingChanges(t *testing.T) {
	t.Parallel()
	ctx, f, _, m := mappingFixture(t)
	visitor := *f.auth
	visitor.ActiveOrganizationID = constants.DemoOrganizationID
	ctx = authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &visitor), authz.DemoScopeGrants()...)
	_, err := f.service.SetMapping(ctx, mappingRequest(m, &visitor.UserID))
	requireMappingCode(t, err, oops.CodeForbidden)
	_, err = f.service.SetMapping(ctx, mappingRequest(m, nil))
	requireMappingCode(t, err, oops.CodeForbidden)
}
