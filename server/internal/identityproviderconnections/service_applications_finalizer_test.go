package identityproviderconnections_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	apprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
)

func finalizerRun(t *testing.T, ctx context.Context, si *serviceInstance, id uuid.UUID, started time.Time, status string) apprepo.OktaApplicationReconcileRun {
	t.Helper()
	q := apprepo.New(si.conn.conn)
	run, err := q.CreateReconcileRun(ctx, apprepo.CreateReconcileRunParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	require.NoError(t, q.BackdateReconcileRun(ctx, apprepo.BackdateReconcileRunParams{ID: run.ID, OrganizationID: si.orgID, StartedAt: pgtype.Timestamptz{Time: started, Valid: true}, Status: "running"}))
	if status != "running" {
		_, err = q.FinishReconcileRun(ctx, apprepo.FinishReconcileRunParams{ID: run.ID, OrganizationID: si.orgID, Status: status, ApplicationsSeen: 3, ApplicationsAdded: 2, AssignmentsAdded: 1, SkippedAppIds: []string{appA}, Truncated: true, Error: pgtype.Text{String: "original attempt result", Valid: true}})
		require.NoError(t, err)
	}
	return finalizerReadRun(t, ctx, si, run.ID)
}

func finalizerReadRun(t *testing.T, ctx context.Context, si *serviceInstance, id uuid.UUID) apprepo.OktaApplicationReconcileRun {
	t.Helper()
	run, err := apprepo.New(si.conn.conn).GetReconcileRun(ctx, apprepo.GetReconcileRunParams{ID: id, OrganizationID: si.orgID})
	require.NoError(t, err)
	return run
}

// Include updated_at so a retry that rewrites the same watermark is detected.
func finalizerWatermark(t *testing.T, ctx context.Context, si *serviceInstance, id uuid.UUID) [2]pgtype.Timestamptz {
	t.Helper()
	row := finalizerConnection(t, ctx, si, id)
	return [2]pgtype.Timestamptz{row.ApplicationsSyncedAt, row.UpdatedAt}
}

func finalizerConnection(t *testing.T, ctx context.Context, si *serviceInstance, id uuid.UUID) repo.OktaIdentityProviderConnection {
	t.Helper()
	row, err := repo.New(si.conn.conn).GetOktaIdentityProviderConnectionIncludingDeleted(ctx, repo.GetOktaIdentityProviderConnectionIncludingDeletedParams{OrganizationID: si.orgID, ID: id})
	require.NoError(t, err)
	return row.OktaIdentityProviderConnection
}

func TestApplicationsSync_FinalizeFailure_InterruptedAttemptsAndLateFinish(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	id := mustParseUUID(t, verifiedConnection(t, ctx, si).ID)
	syncer := newSyncer(t, si)
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	startedAt := cutoff.Add(-10 * time.Minute)
	older := finalizerRun(t, ctx, si, id, cutoff.Add(-time.Minute), "running")
	atCutoff := finalizerRun(t, ctx, si, id, cutoff, "running")
	failed := finalizerRun(t, ctx, si, id, cutoff.Add(-2*time.Minute), "failed")
	succeeded := finalizerRun(t, ctx, si, id, cutoff.Add(-3*time.Minute), "succeeded")
	newer := finalizerRun(t, ctx, si, id, cutoff.Add(time.Microsecond), "running")
	require.NoError(t, syncer.FinalizeFailure(ctx, id, startedAt, cutoff))
	watermark := finalizerWatermark(t, ctx, si, id)
	require.True(t, watermark[0].Valid)
	require.True(t, startedAt.Equal(watermark[0].Time), "the watermark stops at the attempt start, not the failure")
	closed := make([]apprepo.OktaApplicationReconcileRun, 0, 2)
	for _, run := range []apprepo.OktaApplicationReconcileRun{older, atCutoff} {
		got := finalizerReadRun(t, ctx, si, run.ID)
		require.Equal(t, "failed", got.Status)
		require.True(t, got.FinishedAt.Valid)
		require.Equal(t, pgtype.Text{String: "interrupted", Valid: true}, got.Error)
		require.Equal(t, run.StartedAt, got.StartedAt)
		closed = append(closed, got)
	}
	for _, run := range []apprepo.OktaApplicationReconcileRun{failed, succeeded, newer} {
		require.Equal(t, run, finalizerReadRun(t, ctx, si, run.ID))
	}
	// A late activity cannot replace the terminal result or its counters.
	_, err := apprepo.New(si.conn.conn).FinishReconcileRun(ctx, apprepo.FinishReconcileRunParams{ID: older.ID, OrganizationID: si.orgID, Status: "succeeded", ApplicationsSeen: 99, ApplicationsAdded: 99, SkippedAppIds: []string{}})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, syncer.FinalizeFailure(ctx, id, startedAt, cutoff))
	require.Equal(t, watermark, finalizerWatermark(t, ctx, si, id))
	for _, run := range append(closed, failed, succeeded, newer) {
		require.Equal(t, run, finalizerReadRun(t, ctx, si, run.ID))
	}
}

func TestApplicationsSync_FinalizeFailure_AlreadyFailedAttempt(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	id := mustParseUUID(t, verifiedConnection(t, ctx, si).ID)
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	startedAt := cutoff.Add(-5 * time.Minute).Add(789 * time.Nanosecond)
	failed := finalizerRun(t, ctx, si, id, cutoff.Add(-time.Minute), "failed")
	syncer := newSyncer(t, si)
	require.NoError(t, syncer.FinalizeFailure(ctx, id, startedAt, cutoff))
	watermark := finalizerWatermark(t, ctx, si, id)
	require.True(t, watermark[0].Valid)
	require.True(t, startedAt.Truncate(time.Microsecond).Equal(watermark[0].Time), "terminal failure advances the watermark even with no running attempts")
	require.Equal(t, failed, finalizerReadRun(t, ctx, si, failed.ID))
	require.NoError(t, syncer.FinalizeFailure(ctx, id, startedAt, cutoff))
	require.Equal(t, watermark, finalizerWatermark(t, ctx, si, id))
	require.Equal(t, failed, finalizerReadRun(t, ctx, si, failed.ID))
}

func TestApplicationsSync_FinalizeFailure_PreservesNewerWatermark(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	id := mustParseUUID(t, verifiedConnection(t, ctx, si).ID)
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	old := finalizerRun(t, ctx, si, id, cutoff.Add(-time.Minute), "running")
	newer := finalizerRun(t, ctx, si, id, cutoff.Add(time.Minute), "succeeded")
	_, err := apprepo.New(si.conn.conn).MarkApplicationsSynced(ctx, apprepo.MarkApplicationsSyncedParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id, SyncedAt: newer.StartedAt})
	require.NoError(t, err)
	before := finalizerWatermark(t, ctx, si, id)
	require.NoError(t, newSyncer(t, si).FinalizeFailure(ctx, id, cutoff.Add(-5*time.Minute), cutoff))
	require.Equal(t, before, finalizerWatermark(t, ctx, si, id))
	require.Equal(t, old, finalizerReadRun(t, ctx, si, old.ID))
	require.Equal(t, newer, finalizerReadRun(t, ctx, si, newer.ID))
}

func TestApplicationsSync_FinalizeFailure_RevokedConnection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	cutoff := time.Now().UTC().Truncate(time.Microsecond)
	finalizerRun(t, ctx, si, id, cutoff.Add(-time.Minute), "running")
	_, err := si.svc.Revoke(ctx, &gen.RevokePayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)
	before := finalizerWatermark(t, ctx, si, id)
	require.Zero(t, countRows(t, ctx, si, "okta_application_reconcile_runs", id))
	syncer := newSyncer(t, si)
	require.NoError(t, syncer.FinalizeFailure(ctx, id, cutoff.Add(-time.Minute), cutoff))
	require.NoError(t, syncer.FinalizeFailure(ctx, id, cutoff.Add(-time.Minute), cutoff))
	require.Equal(t, before, finalizerWatermark(t, ctx, si, id))
	require.Zero(t, countRows(t, ctx, si, "okta_application_reconcile_runs", id))
}

func TestApplicationsSync_FinalizeFailure_KeepsLateRequestDue(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	syncer := newSyncer(t, si)
	setFixtures(si, []okta.App{fixtureApp(appA, "Existing app", "oidc_client")}, nil, nil)
	runSync(t, ctx, syncer, id)
	candidates, err := syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates)

	// The failing attempt starts, then a sync is requested while it runs.
	startedAt := time.Now().UTC().Truncate(time.Microsecond)
	attempt := finalizerRun(t, ctx, si, id, startedAt, "running")
	_, err = si.svc.SyncApplications(ctx, &gen.SyncApplicationsPayload{SessionToken: nil, ID: verified.ID})
	require.NoError(t, err)
	requestedAt := finalizerConnection(t, ctx, si, id).ApplicationsSyncRequestedAt
	require.True(t, requestedAt.Valid)
	require.True(t, requestedAt.Time.After(startedAt))

	cutoff := requestedAt.Time.Add(time.Minute)
	require.NoError(t, syncer.FinalizeFailure(ctx, id, startedAt, cutoff))
	closed := finalizerReadRun(t, ctx, si, attempt.ID)
	require.Equal(t, "failed", closed.Status)
	require.Equal(t, "interrupted", closed.Error.String)
	watermark := finalizerWatermark(t, ctx, si, id)
	require.True(t, startedAt.Equal(watermark[0].Time))
	candidates, err = syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "a sync requested during the failed attempt stays due")
	require.Equal(t, id, candidates[0].ConnectionID)

	// A finalize retry changes nothing.
	require.NoError(t, syncer.FinalizeFailure(ctx, id, startedAt, cutoff))
	require.Equal(t, watermark, finalizerWatermark(t, ctx, si, id))
	require.Equal(t, closed, finalizerReadRun(t, ctx, si, attempt.ID))
}

// Pause at the external read, after Run has committed its running attempt.
type finalizerBlockingClient struct {
	okta.Client
	entered chan struct{}
	release chan struct{}
}

func (c *finalizerBlockingClient) ListApps(ctx context.Context, req okta.ListAppsRequest) ([]okta.App, error) {
	close(c.entered)
	select {
	case <-c.release:
		apps, err := c.Client.ListApps(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list apps: %w", err)
		}
		return apps, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("list apps: %w", ctx.Err())
	}
}

type finalizerClientFactory struct {
	okta.ClientFactory
	client okta.Client
}

func (f finalizerClientFactory) Client(okta.Config) (okta.Client, error) {
	return f.client, nil
}

func TestApplicationsSync_FinalizeFailure_InFlightRun(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	verified := verifiedConnection(t, ctx, si)
	id := mustParseUUID(t, verified.ID)
	setFixtures(si, []okta.App{fixtureApp(appA, "Existing app", "oidc_client")}, nil, nil)
	runSync(t, ctx, newSyncer(t, si), id)
	setFixtures(si, []okta.App{fixtureApp(appB, "Late app", "oidc_client")}, nil, nil)
	client := &finalizerBlockingClient{Client: si.oktaFakes.Fake(fullOrgURL), entered: make(chan struct{}), release: make(chan struct{})}
	syncer := oktaapplications.NewSyncer(testenv.NewLogger(t), testenv.NewMeterProvider(t), si.conn.conn, finalizerClientFactory{ClientFactory: si.oktaFakes, client: client})
	done := make(chan error, 1)
	go func() { done <- syncer.Run(ctx, id, false) }()
	select {
	case <-client.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	run := latestRun(t, ctx, si, id)
	require.Equal(t, "running", run.Status)
	// Derive the terminal cutoff from the DB clock to avoid host/container skew.
	cutoff := run.StartedAt.Time.Add(time.Second)
	require.NoError(t, syncer.FinalizeFailure(ctx, id, run.StartedAt.Time, cutoff))
	candidates, err := syncer.ListCandidates(ctx, 10, nil)
	require.NoError(t, err)
	require.Empty(t, candidates, "terminal finalization defers the next sync until its interval")
	closed := finalizerReadRun(t, ctx, si, run.ID)
	require.Equal(t, "failed", closed.Status)
	require.Equal(t, "interrupted", closed.Error.String)
	watermark := finalizerWatermark(t, ctx, si, id)
	close(client.release)
	select {
	case err := <-done:
		require.NoError(t, err, "a late superseded Run handles the guarded finish's ErrNoRows")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Equal(t, closed, finalizerReadRun(t, ctx, si, run.ID))
	require.Equal(t, watermark, finalizerWatermark(t, ctx, si, id))
	rows, err := apprepo.New(si.conn.conn).ListLiveApplicationIDs(ctx, apprepo.ListLiveApplicationIDsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: id})
	require.NoError(t, err)
	require.Equal(t, []string{appA}, rows, "the late snapshot must not apply")
}
