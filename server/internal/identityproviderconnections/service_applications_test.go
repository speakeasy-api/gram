package identityproviderconnections_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

const (
	appA = "0oaappa0000000000001"
	appB = "0oaappb0000000000002"
	appC = "0oaappc0000000000003"
)

func verifiedConnection(t *testing.T, ctx context.Context, si *serviceInstance) *gen.OktaIdentityProviderConnection {
	t.Helper()

	created := createConnection(t, ctx, si, fullOrgURL)
	submitClientID(t, ctx, si, created.ID)
	verified, err := si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, verified.Status)
	return verified
}

func newSyncer(t *testing.T, si *serviceInstance) *oktaapplications.Syncer {
	t.Helper()
	return oktaapplications.NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), si.conn.conn, si.oktaFakes)
}

func runSync(t *testing.T, ctx context.Context, syncer *oktaapplications.Syncer, id uuid.UUID) {
	t.Helper()
	require.NoError(t, syncer.Run(ctx, id, false))
}

func fixtureApp(id, label, name string) okta.App {
	return okta.App{ID: id, Label: label, Name: name, SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: []string{"PUSH_NEW_USERS", "IMPORT_NEW_USERS"}, Created: testTime(), LastUpdated: testTime()}
}

func setFixtures(si *serviceInstance, apps []okta.App, users map[string][]okta.AppUser, groups map[string][]okta.AppGroup) {
	si.oktaFakes.Fake(fullOrgURL).SetFixtures(okta.Fixtures{
		Users:         nil,
		Apps:          apps,
		AppUsers:      users,
		AppGroups:     groups,
		Groups:        nil,
		GrantedScopes: allScopes(),
	})
}

func listApplications(t *testing.T, ctx context.Context, si *serviceInstance, id string, includeRemoved bool) *gen.ListIdentityProviderConnectionApplicationsResult {
	t.Helper()
	res, err := si.svc.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ID: id, IncludeRemoved: includeRemoved})
	require.NoError(t, err)
	return res
}

func latestRun(t *testing.T, ctx context.Context, si *serviceInstance, id uuid.UUID) apprepo.OktaApplicationReconcileRun {
	t.Helper()
	run, err := oktaapplications.LatestRun(ctx, si.conn.conn, si.orgID, id)
	require.NoError(t, err)
	require.NotNil(t, run)
	return *run
}

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	require.NoError(t, err)
	return parsed
}

func countRows(t *testing.T, ctx context.Context, si *serviceInstance, table string, id uuid.UUID) int64 {
	t.Helper()
	q := apprepo.New(si.conn.conn)
	var n int64
	var err error
	switch table {
	case "okta_applications":
		n, err = q.CountApplicationsForConnection(ctx, apprepo.CountApplicationsForConnectionParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	case "okta_application_assignments":
		n, err = q.CountAssignmentsForConnection(ctx, apprepo.CountAssignmentsForConnectionParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	default:
		n, err = q.CountReconcileRunsForConnection(ctx, apprepo.CountReconcileRunsForConnectionParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	}
	require.NoError(t, err)
	return n
}

func TestApplicationsSync_AddRemoveReassign(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	setFixtures(si,
		[]okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client"), fixtureApp("0oadash0000000000000", "Okta Dashboard", "okta_enduser")},
		map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER", Status: "PROVISIONED"}, {ID: "00uone", Scope: "USER", Status: "PROVISIONED"}}},
		map[string][]okta.AppGroup{appB: {{ID: "00gone", Priority: 0}}},
	)
	runSync(t, ctx, syncer, id)

	res := listApplications(t, ctx, si, verified.ID, false)
	require.Len(t, res.Applications, 2)
	require.Equal(t, "Linear", res.Applications[0].Label)
	require.Equal(t, 1, res.Applications[0].GroupAssignments)
	require.Equal(t, "Notion", res.Applications[1].Label)
	require.Equal(t, 1, res.Applications[1].UserAssignments, "a duplicated assignment in the listing is one row")
	require.Equal(t, []string{"IMPORT_NEW_USERS", "PUSH_NEW_USERS"}, res.Applications[1].Features, "features are stored sorted")
	require.NotNil(t, res.LastRun)
	require.Equal(t, "succeeded", res.LastRun.Status)
	require.Equal(t, 2, res.LastRun.ApplicationsSeen)
	require.Equal(t, 2, res.LastRun.ApplicationsAdded)
	require.Equal(t, 2, res.LastRun.AssignmentsAdded)
	require.Equal(t, []string{"0oadash0000000000000"}, res.LastRun.SkippedAppIds)
	require.False(t, res.LastRun.Truncated)
	require.NotNil(t, res.Sync.SyncedAt)
	require.Equal(t, res.LastRun.StartedAt, *res.Sync.SyncedAt, "the watermark is the run's start")

	// App B disappears, app C appears, app A's user becomes a group.
	setFixtures(si,
		[]okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appC, "Slack", "oidc_client")},
		nil,
		map[string][]okta.AppGroup{appA: {{ID: "00gtwo", Priority: 0}}},
	)
	runSync(t, ctx, syncer, id)

	res = listApplications(t, ctx, si, verified.ID, false)
	require.Len(t, res.Applications, 2)
	require.Equal(t, appA, res.Applications[0].OktaAppID)
	require.Equal(t, 0, res.Applications[0].UserAssignments)
	require.Equal(t, 1, res.Applications[0].GroupAssignments)
	require.Equal(t, appC, res.Applications[1].OktaAppID)
	require.Equal(t, 1, res.LastRun.ApplicationsAdded)
	require.Equal(t, 1, res.LastRun.ApplicationsRemoved)
	require.Equal(t, 1, res.LastRun.AssignmentsAdded)
	require.Equal(t, 2, res.LastRun.AssignmentsRemoved, "the user assignment and the removed app's group assignment")

	removed := listApplications(t, ctx, si, verified.ID, true)
	require.Len(t, removed.Applications, 3)
	require.Equal(t, appB, removed.Applications[2].OktaAppID, "removed rows sort last")
	require.NotNil(t, removed.Applications[2].RemovedAt)

	// App B comes back: revived, not duplicated.
	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client"), fixtureApp(appC, "Slack", "oidc_client")}, nil, nil)
	runSync(t, ctx, syncer, id)
	res = listApplications(t, ctx, si, verified.ID, false)
	require.Len(t, res.Applications, 3)
	require.Equal(t, 1, res.LastRun.ApplicationsAdded)
	require.Equal(t, 1, res.LastRun.AssignmentsRemoved)
}

func TestApplicationsSync_UnchangedRunLeavesUpdatedAtAlone(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client")}, map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}}, nil)
	runSync(t, ctx, syncer, id)
	first, err := oktaapplications.ListSnapshot(ctx, si.conn.conn, si.orgID, id, false, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	runSync(t, ctx, syncer, id)
	second, err := oktaapplications.ListSnapshot(ctx, si.conn.conn, si.orgID, id, false, 10)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, first[0].UpdatedAt.Time, second[0].UpdatedAt.Time, "unchanged content leaves updated_at alone")
	require.True(t, second[0].LastSeenAt.Time.After(first[0].LastSeenAt.Time), "the watermark still advances")

	run := latestRun(t, ctx, si, id)
	require.Equal(t, "succeeded", run.Status)
	require.Zero(t, run.ApplicationsAdded)
	require.Zero(t, run.ApplicationsRemoved)
	require.Zero(t, run.AssignmentsAdded)
	require.Zero(t, run.AssignmentsRemoved)
	require.Equal(t, int32(1), run.ApplicationsSeen)

	// A changed attribute moves updated_at.
	setFixtures(si, []okta.App{{ID: appA, Label: "Notion (renamed)", Name: "oidc_client", SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: nil, Created: testTime(), LastUpdated: testTime()}}, map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}}, nil)
	runSync(t, ctx, syncer, id)
	third, err := oktaapplications.ListSnapshot(ctx, si.conn.conn, si.orgID, id, false, 10)
	require.NoError(t, err)
	require.True(t, third[0].UpdatedAt.Time.After(second[0].UpdatedAt.Time))
	require.Equal(t, "Notion (renamed)", third[0].Label)
}

func TestApplicationsSync_PageCapKeepsPartialPagesAndRemovesNothingMissing(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client")},
		map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}, appB: {{ID: "00utwo", Scope: "USER"}}},
		map[string][]okta.AppGroup{appA: {{ID: "00gone", Priority: 0}}})
	runSync(t, ctx, syncer, id)

	// The application listing blows its page cap: the run is truncated and
	// nothing missing from it is removed.
	fake.SetMethodError("ListApps", okta.ErrTooManyPages)
	runSync(t, ctx, syncer, id)
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "succeeded", run.Status)
	require.True(t, run.Truncated)
	require.Zero(t, run.ApplicationsRemoved)
	require.Len(t, listApplications(t, ctx, si, verified.ID, false).Applications, 2)

	// Only app A's user listing blows its cap: A's users stay, A's groups
	// still reconcile (its group is gone), B's users still reconcile.
	fake.SetMethodError("ListApps", nil)
	fake.SetAppError("ListAppUsers", appA, okta.ErrTooManyPages)
	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client")}, nil, nil)
	runSync(t, ctx, syncer, id)
	run = latestRun(t, ctx, si, id)
	require.Equal(t, "succeeded", run.Status)
	require.True(t, run.Truncated)
	require.Equal(t, int32(2), run.AssignmentsRemoved, "A's group and B's user")
	res := listApplications(t, ctx, si, verified.ID, false)
	require.Equal(t, 0, res.Applications[0].UserAssignments, "Linear's users were listed and are gone")
	require.Equal(t, 0, res.Applications[0].GroupAssignments)
	require.Equal(t, 1, res.Applications[1].UserAssignments, "Notion keeps its user because that listing was incomplete")
	require.Equal(t, 0, res.Applications[1].GroupAssignments)
}

func TestApplicationsSync_RateLimitIsRetriedThenRecorded(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client")},
		map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}, appB: {{ID: "00utwo", Scope: "USER"}}}, nil)
	runSync(t, ctx, syncer, id)

	// 429 mid-run, on the second app's assignments: nothing partial lands.
	fake.SetAppError("ListAppUsers", appB, &okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps/x/users", StatusCode: http.StatusTooManyRequests, ErrorCode: "E0000047", Summary: "API call exceeded rate limit"})
	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client")},
		map[string][]okta.AppUser{appA: {{ID: "00uthree", Scope: "USER"}}, appB: {{ID: "00utwo", Scope: "USER"}}}, nil)
	err := syncer.Run(ctx, id, false)
	require.Error(t, err)
	require.True(t, oktaapplications.IsRetryable(err))
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "rate_limited", run.Error.String)
	require.Equal(t, 1, listApplications(t, ctx, si, verified.ID, false).Applications[1].UserAssignments)

	// The watermark did not move, so Temporal's retry is what runs next.
	beforeRetry := listApplications(t, ctx, si, verified.ID, false).Sync.SyncedAt
	require.NotNil(t, beforeRetry)
	require.True(t, run.StartedAt.Time.After(mustParseTime(t, *beforeRetry)))

	// The last attempt records the failure and advances the watermark.
	require.NoError(t, syncer.Run(ctx, id, true))
	run = latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "rate_limited", run.Error.String)
	afterFinal := listApplications(t, ctx, si, verified.ID, false).Sync.SyncedAt
	require.NotNil(t, afterFinal)
	require.Equal(t, run.StartedAt.Time.UTC().Format(time.RFC3339), mustParseTime(t, *afterFinal).UTC().Format(time.RFC3339))

	// The next scheduled run re-runs from scratch and applies the change.
	fake.SetAppError("ListAppUsers", appB, nil)
	runSync(t, ctx, syncer, id)
	require.Equal(t, "succeeded", latestRun(t, ctx, si, id).Status)
	res := listApplications(t, ctx, si, verified.ID, false)
	require.Equal(t, 1, res.Applications[1].UserAssignments)
	require.Equal(t, 1, res.LastRun.AssignmentsAdded)
	require.Equal(t, 1, res.LastRun.AssignmentsRemoved)
}

func TestApplicationsSync_OktaOutageIsRetried(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	fake.SetMethodError("ListApps", &okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps", StatusCode: http.StatusBadGateway, ErrorCode: "", Summary: "upstream unavailable"})
	err := syncer.Run(ctx, id, false)
	require.True(t, oktaapplications.IsRetryable(err))
	require.Equal(t, "okta_unreachable", latestRun(t, ctx, si, id).Error.String)
	candidates, err := syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "a transient failure keeps the connection due")
}

func TestApplicationsSync_CredentialRejectedAdvancesWatermark(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	fake.SetMethodError("ListApps", &okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps", StatusCode: http.StatusUnauthorized, ErrorCode: "E0000011", Summary: "Invalid token provided"})
	runSync(t, ctx, syncer, id)
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "credential_rejected", run.Error.String)

	candidates, err := syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates, "a deterministic failure waits for the interval")

	// Connection state is the verify path's to change.
	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &verified.ID})
	require.NoError(t, err)
	require.Equal(t, identityproviderconnections.StatusVerified, fetched.Connection.Status)
}

func TestApplicationsSync_SupersededRunIsDiscarded(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	// A later run already applied: model it by pushing the watermark past
	// any run this test starts.
	_, err := apprepo.New(si.conn.conn).MarkApplicationsSynced(ctx, apprepo.MarkApplicationsSyncedParams{
		SyncedAt:                     pgtype.Timestamptz{Time: time.Now().Add(time.Hour).UTC(), Valid: true, InfinityModifier: pgtype.Finite},
		IdentityProviderConnectionID: id,
		OrganizationID:               si.orgID,
	})
	require.NoError(t, err)
	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client")}, nil, nil)
	runSync(t, ctx, syncer, id)
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "superseded", run.Error.String)
	require.Zero(t, countRows(t, ctx, si, "okta_applications", id))
}

func TestApplicationsSync_InterruptedRunIsClosedByTheNext(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	q := apprepo.New(si.conn.conn)
	stale, err := q.CreateReconcileRun(ctx, apprepo.CreateReconcileRunParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	old, err := q.CreateReconcileRun(ctx, apprepo.CreateReconcileRunParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	require.NoError(t, q.BackdateReconcileRun(ctx, apprepo.BackdateReconcileRunParams{
		Status:         "succeeded",
		StartedAt:      pgtype.Timestamptz{Time: time.Now().Add(-40 * 24 * time.Hour).UTC(), Valid: true, InfinityModifier: pgtype.Finite},
		ID:             old.ID,
		OrganizationID: si.orgID,
	}))

	runSync(t, ctx, syncer, id)

	closed, err := q.GetReconcileRun(ctx, apprepo.GetReconcileRunParams{ID: stale.ID, OrganizationID: si.orgID})
	require.NoError(t, err)
	require.Equal(t, "failed", closed.Status)
	require.Equal(t, "interrupted", closed.Error.String)
	require.EqualValues(t, 2, countRows(t, ctx, si, "okta_application_reconcile_runs", id), "the 40-day-old run is pruned")
}

func TestApplicationsSync_CandidatesOnlyVerifiedAndDue(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	syncer := newSyncer(t, si)

	created := createConnection(t, ctx, si, fullOrgURL)
	candidates, err := syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates, "pending connections never sync")

	submitClientID(t, ctx, si, created.ID)
	_, err = si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	id := mustParseUUID(t, created.ID)

	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, id, candidates[0].ConnectionID)
	require.Equal(t, si.orgID, candidates[0].OrganizationID)

	candidates, err = syncer.ListCandidates(ctx, 10, []uuid.UUID{id})
	require.NoError(t, err)
	require.Empty(t, candidates, "attempted ids are excluded")

	runSync(t, ctx, syncer, id)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates, "a fresh snapshot is not due")

	// A request made after the last run started keeps the connection due.
	_, err = si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "a manual sync makes it due again")
	runSync(t, ctx, syncer, id)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates)

	// A revoked connection is gone from candidates and its run is a no-op.
	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates)
	runSync(t, ctx, syncer, id)
}

func TestSyncApplications_RPC(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	_, err := si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	_, err = si.svc.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ID: created.ID, IncludeRemoved: false})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.Zero(t, si.syncTrigger.Calls())

	submitClientID(t, ctx, si, created.ID)
	_, err = si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	id := mustParseUUID(t, created.ID)
	runSync(t, ctx, newSyncer(t, si), id)
	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.NotNil(t, fetched.Connection.ApplicationsSync.SyncedAt)
	require.Nil(t, fetched.Connection.ApplicationsSync.RequestedAt)
	require.Equal(t, 21600, fetched.Connection.ApplicationsSync.IntervalSeconds)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSyncApplications)
	require.NoError(t, err)

	synced, err := si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.NotNil(t, synced.ApplicationsSync.SyncedAt, "the watermark is kept")
	require.NotNil(t, synced.ApplicationsSync.RequestedAt)
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, si.syncTrigger.Calls())
	}, 2*time.Second, 10*time.Millisecond)

	after, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSyncApplications)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSyncApplications)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)

	_, err = si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeRateLimitExceeded)

	_, err = si.svc.SyncApplications(asSupportSession(ctx, si), &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeForbidden)

	otherCtx, _ := asOtherOrganization(t, ctx, si)
	_, err = si.svc.SyncApplications(otherCtx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.ListApplications(otherCtx, &gen.ListApplicationsPayload{SessionToken: nil, ID: created.ID, IncludeRemoved: false})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRevoke_DeletesApplicationsSnapshot(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)

	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client")}, map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}}, nil)
	runSync(t, ctx, newSyncer(t, si), id)
	require.EqualValues(t, 1, countRows(t, ctx, si, "okta_applications", id))
	require.EqualValues(t, 1, countRows(t, ctx, si, "okta_application_assignments", id))
	require.EqualValues(t, 1, countRows(t, ctx, si, "okta_application_reconcile_runs", id))

	_, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)

	require.Zero(t, countRows(t, ctx, si, "okta_applications", id))
	require.Zero(t, countRows(t, ctx, si, "okta_application_assignments", id))
	require.Zero(t, countRows(t, ctx, si, "okta_application_reconcile_runs", id))
}
