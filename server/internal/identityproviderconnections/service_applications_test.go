package identityproviderconnections_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
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

func fixtureApp(id, label, name string) okta.App {
	return okta.App{ID: id, Label: label, Name: name, SignOnMode: "OPENID_CONNECT", Status: "ACTIVE", Features: []string{"IMPORT_NEW_USERS"}, Created: testTime(), LastUpdated: testTime()}
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
	run, err := apprepo.New(si.conn.conn).GetLatestReconcileRun(ctx, apprepo.GetLatestReconcileRunParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	return run
}

func TestApplicationsSync_AddRemoveReassign(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	setFixtures(si,
		[]okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client"), fixtureApp("0oadash0000000000000", "Okta Dashboard", "okta_enduser")},
		map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER", Status: "PROVISIONED"}}},
		map[string][]okta.AppGroup{appB: {{ID: "00gone", Priority: 0}}},
	)
	require.NoError(t, syncer.Run(ctx, id))

	res := listApplications(t, ctx, si, verified.ID, false)
	require.Len(t, res.Applications, 2)
	require.Equal(t, "Linear", res.Applications[0].Label)
	require.Equal(t, 1, res.Applications[0].GroupAssignments)
	require.Equal(t, "Notion", res.Applications[1].Label)
	require.Equal(t, 1, res.Applications[1].UserAssignments)
	require.Equal(t, []string{"IMPORT_NEW_USERS"}, res.Applications[1].Features)
	require.NotNil(t, res.LastRun)
	require.Equal(t, "succeeded", res.LastRun.Status)
	require.Equal(t, 2, res.LastRun.ApplicationsSeen)
	require.Equal(t, 2, res.LastRun.ApplicationsAdded)
	require.Equal(t, 2, res.LastRun.AssignmentsAdded)
	require.Equal(t, []string{"0oadash0000000000000"}, res.LastRun.SkippedAppIds)
	require.False(t, res.LastRun.Truncated)
	require.NotNil(t, res.Sync.SyncedAt)

	// App B disappears, app C appears, app A's user becomes a group.
	setFixtures(si,
		[]okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appC, "Slack", "oidc_client")},
		nil,
		map[string][]okta.AppGroup{appA: {{ID: "00gtwo", Priority: 0}}},
	)
	require.NoError(t, syncer.Run(ctx, id))

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
	var sawB bool
	for _, app := range removed.Applications {
		if app.OktaAppID == appB {
			sawB = true
			require.NotNil(t, app.RemovedAt)
		}
	}
	require.True(t, sawB)

	// App B comes back: revived, not duplicated.
	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client"), fixtureApp(appC, "Slack", "oidc_client")}, nil, nil)
	require.NoError(t, syncer.Run(ctx, id))
	res = listApplications(t, ctx, si, verified.ID, false)
	require.Len(t, res.Applications, 3)
	require.Equal(t, 1, res.LastRun.ApplicationsAdded)
	require.Equal(t, 1, res.LastRun.AssignmentsRemoved)
}

func TestApplicationsSync_UnchangedRunTouchesNoRows(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)

	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client")}, map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}}, nil)
	require.NoError(t, syncer.Run(ctx, id))
	first, err := apprepo.New(si.conn.conn).ListApplications(ctx, apprepo.ListApplicationsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id, IncludeRemoved: false, LimitCount: 10})
	require.NoError(t, err)
	require.Len(t, first, 1)

	require.NoError(t, syncer.Run(ctx, id))
	second, err := apprepo.New(si.conn.conn).ListApplications(ctx, apprepo.ListApplicationsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id, IncludeRemoved: false, LimitCount: 10})
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
}

func TestApplicationsSync_PageCapTruncatesWithoutRemoving(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client")},
		map[string][]okta.AppUser{appA: {{ID: "00uone", Scope: "USER"}}, appB: {{ID: "00utwo", Scope: "USER"}}}, nil)
	require.NoError(t, syncer.Run(ctx, id))

	// The application listing blows its page cap: recorded, nothing removed.
	fake.SetMethodError("ListApps", okta.ErrTooManyPages)
	require.NoError(t, syncer.Run(ctx, id))
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "too_many_applications", run.Error.String)
	require.Len(t, listApplications(t, ctx, si, verified.ID, false).Applications, 2)

	// One app's assignment listing blows its cap: the app stays, its
	// assignments stay, the run is marked truncated, the other app's
	// assignments still reconcile.
	fake.SetMethodError("ListApps", nil)
	fake.SetMethodError("ListAppUsers", okta.ErrTooManyPages)
	setFixtures(si, []okta.App{fixtureApp(appA, "Notion", "oidc_client"), fixtureApp(appB, "Linear", "oidc_client")}, nil, nil)
	require.NoError(t, syncer.Run(ctx, id))
	run = latestRun(t, ctx, si, id)
	require.Equal(t, "succeeded", run.Status)
	require.True(t, run.Truncated)
	require.Zero(t, run.AssignmentsRemoved)
	res := listApplications(t, ctx, si, verified.ID, false)
	require.Equal(t, 1, res.Applications[0].UserAssignments)
	require.Equal(t, 1, res.Applications[1].UserAssignments)
}

func TestApplicationsSync_RateLimitIsRetryable(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	fake.SetMethodError("ListApps", &okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps", StatusCode: http.StatusTooManyRequests, ErrorCode: "E0000047", Summary: "API call exceeded rate limit"})
	err := syncer.Run(ctx, id)
	require.Error(t, err)
	require.True(t, oktaapplications.IsRetryable(err))
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "failed", run.Status)
	require.Equal(t, "rate_limited", run.Error.String)

	// The watermark did not move: the connection is still due.
	candidates, err := syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, id, candidates[0].ConnectionID)

	// The retry resumes and completes.
	fake.SetMethodError("ListApps", nil)
	require.NoError(t, syncer.Run(ctx, id))
	require.Equal(t, "succeeded", latestRun(t, ctx, si, id).Status)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestApplicationsSync_CredentialRejectedAdvancesWatermark(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	fake := si.oktaFakes.Fake(fullOrgURL)

	fake.SetMethodError("ListApps", &okta.APIError{Method: http.MethodGet, Path: "/api/v1/apps", StatusCode: http.StatusUnauthorized, ErrorCode: "E0000011", Summary: "Invalid token provided"})
	require.NoError(t, syncer.Run(ctx, id))
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

	require.NoError(t, syncer.Run(ctx, id))
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates, "a fresh snapshot is not due")

	_, err = si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "a manual sync makes it due again")

	// A revoked connection is gone from candidates and its run is a no-op.
	_, err = si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates)
	require.NoError(t, syncer.Run(ctx, id))
}

func TestSyncApplications_RPC(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	created := createConnection(t, ctx, si, fullOrgURL)
	_, err := si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.Zero(t, si.syncTrigger.Calls())

	submitClientID(t, ctx, si, created.ID)
	_, err = si.svc.Verify(ctx, &gen.VerifyPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	id := mustParseUUID(t, created.ID)
	require.NoError(t, newSyncer(t, si).Run(ctx, id))
	fetched, err := si.svc.Get(ctx, &gen.GetPayload{SessionToken: nil, ID: &created.ID})
	require.NoError(t, err)
	require.NotNil(t, fetched.Connection.ApplicationsSync.SyncedAt)
	require.Equal(t, 21600, fetched.Connection.ApplicationsSync.IntervalSeconds)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn.conn, audit.ActionIdentityProviderConnectionSyncApplications)
	require.NoError(t, err)

	synced, err := si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Nil(t, synced.ApplicationsSync.SyncedAt, "the watermark is cleared")
	require.Equal(t, 1, si.syncTrigger.Calls())

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
	require.NoError(t, newSyncer(t, si).Run(ctx, id))
	require.Len(t, listApplications(t, ctx, si, verified.ID, true).Applications, 1)

	_, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)

	q := apprepo.New(si.conn.conn)
	apps, err := q.ListApplications(ctx, apprepo.ListApplicationsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id, IncludeRemoved: true, LimitCount: 10})
	require.NoError(t, err)
	require.Empty(t, apps)
	keys, err := q.ListLiveAssignmentKeys(ctx, apprepo.ListLiveAssignmentKeysParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	require.Empty(t, keys)
	_, err = q.GetLatestReconcileRun(ctx, apprepo.GetLatestReconcileRunParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.Error(t, err)
}
