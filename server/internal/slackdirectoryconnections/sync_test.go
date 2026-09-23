package slackdirectoryconnections_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type directoryFunc func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error)

func (f directoryFunc) Fetch(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
	return f(ctx, token, team, report)
}

func snapshot(users ...string) directoryFunc {
	return func(_ context.Context, _, _ string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		members := make([]slackdirectoryconnections.DirectoryMember, 0, len(users))
		for _, user := range users {
			members = append(members, slackdirectoryconnections.DirectoryMember{UserID: user, DisplayName: "Example " + user, Email: "example@demo.getgram.ai", Status: "active", MemberType: "person", UpdatedAt: nil})
		}
		report(slackdirectoryconnections.SyncProgress{Phase: "fetching", Pages: 1, Members: len(members), ExcludedExternal: 2, Bots: 0})
		return members, nil
	}
}
func syncRequest(f *fixture, c *gen.SlackDirectoryConnection) slackdirectoryconnections.SyncInput {
	return slackdirectoryconnections.SyncInput{OrganizationID: f.auth.ActiveOrganizationID, ConnectionID: uuid.MustParse(c.ID), Generation: uuid.MustParse(c.Generation), ActorID: f.auth.UserID, StartedAt: time.Now().UTC().Truncate(time.Microsecond)}
}
func syncer(f *fixture, provider slackdirectoryconnections.DirectoryProvider) *slackdirectoryconnections.DirectorySync {
	return slackdirectoryconnections.NewDirectorySync(f.db, f.enc, provider, audit.NewLogger(), f.productFeatures)
}
func members(t *testing.T, ctx context.Context, f *fixture) []repo.ListSlackDirectoryMembersRow {
	t.Helper()
	rows, err := repo.New(f.db).ListSlackDirectoryMembers(ctx, repo.ListSlackDirectoryMembersParams{OrganizationID: f.auth.ActiveOrganizationID, ConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Search: "", PageSize: 100})
	require.NoError(t, err)
	return rows
}
func connection(t *testing.T, ctx context.Context, f *fixture, input slackdirectoryconnections.SyncInput) repo.SlackDirectoryConnection {
	t.Helper()
	row, err := repo.New(f.db).GetSlackDirectoryConnection(ctx, repo.GetSlackDirectoryConnectionParams{OrganizationID: input.OrganizationID, ID: input.ConnectionID})
	require.NoError(t, err)
	return row
}
func TestSyncRetainsAbsentMembersAndMappings(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01", "UEXAMPLE02")).Run(ctx, input, nil))
	rows := members(t, ctx, f)
	require.Len(t, rows, 2)
	original := rows[0]
	mapping, err := repo.New(f.db).CreateSlackMappingForTest(ctx, repo.CreateSlackMappingForTestParams{OrganizationID: input.OrganizationID, SlackTeamID: c.WorkspaceID, SlackUserID: original.SlackUserID, UserID: f.auth.UserID})
	require.NoError(t, err)
	require.NoError(t, repo.New(f.db).SetSlackMappingRevisionForTest(ctx, repo.SetSlackMappingRevisionForTestParams{OrganizationID: input.OrganizationID, ID: original.ID, MappingRevision: 7}))
	input.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, syncer(f, snapshot()).Run(ctx, input, nil))
	rows = members(t, ctx, f)
	require.Len(t, rows, 2)
	require.Equal(t, original.ID, rows[0].ID)
	require.Equal(t, "unknown", rows[0].Status)
	require.Equal(t, original.LastSeenAt, rows[0].LastSeenAt)
	require.Equal(t, int64(7), rows[0].MappingRevision)
	require.Equal(t, "test_review", rows[0].MappingConflictReason.String)
	retained, err := repo.New(f.db).GetSlackMappingForTest(ctx, repo.GetSlackMappingForTestParams{OrganizationID: input.OrganizationID, ID: mapping.ID})
	require.NoError(t, err)
	require.Equal(t, mapping, retained)
	row := connection(t, ctx, f, input)
	require.Equal(t, input.Generation, row.LastFullSyncGeneration.UUID)
	require.True(t, row.LastFullSyncSucceededAt.Valid)
	latest, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionSync)
	require.NoError(t, err)
	var summary audit.SlackDirectorySyncSummary
	require.NoError(t, json.Unmarshal(latest.Metadata, &summary))
	require.Equal(t, 2, summary.ExcludedExternal)
	var previousSnapshot, nextSnapshot struct {
		MemberCount int64 `json:"MemberCount"`
	}
	require.NoError(t, json.Unmarshal(latest.BeforeSnapshot, &previousSnapshot))
	require.NoError(t, json.Unmarshal(latest.AfterSnapshot, &nextSnapshot))
	require.Equal(t, int64(2), previousSnapshot.MemberCount)
	require.Zero(t, nextSnapshot.MemberCount)
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Zero(t, list.Connections[0].MemberCount)
}

func TestSyncFailureKeepsPublishedSnapshot(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, input, nil))
	before := connection(t, ctx, f, input)
	beforeMembers := members(t, ctx, f)
	input.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	fail := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		return nil, &slackdirectoryconnections.SyncError{Code: "provider_unavailable", Retryable: true, Reconnect: false, RetryAfter: 0}
	})
	require.Error(t, syncer(f, fail).Run(ctx, input, nil))
	after := connection(t, ctx, f, input)
	require.Equal(t, before.LastFullSyncSucceededAt, after.LastFullSyncSucceededAt)
	require.Equal(t, beforeMembers, members(t, ctx, f))
	require.Equal(t, "provider_unavailable", after.LastErrorCode.String)
	// The same workflow attempt may retry and publish a complete new snapshot.
	require.NoError(t, syncer(f, snapshot("UEXAMPLE02")).Run(ctx, input, nil))
	require.False(t, connection(t, ctx, f, input).LastErrorCode.Valid)
}

func TestSyncSerializesFetchAndReplay(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	started := make(chan struct{})
	release := make(chan struct{})
	releaseFetch := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseFetch)
	result := make(chan error, 1)
	provider := directoryFunc(func(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return snapshot("UEXAMPLE01")(ctx, token, team, report)
	})
	go func() { result <- syncer(f, provider).Run(ctx, input, nil) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("directory fetch did not start")
	}
	err := syncer(f, snapshot("UEXAMPLE02")).Run(ctx, input, nil)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, "sync_busy", syncErr.Code)
	releaseFetch()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("directory sync did not finish")
	}
	neverFetch := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		return nil, errors.New("must not fetch")
	})
	require.NoError(t, syncer(f, neverFetch).Run(ctx, input, nil))
	input.StartedAt = input.StartedAt.Add(-time.Minute)
	require.NoError(t, syncer(f, neverFetch).Run(ctx, input, nil))
	require.Len(t, members(t, ctx, f), 1)
}

func TestSyncDisconnectDuringFetchDiscardsSnapshot(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	provider := directoryFunc(func(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		_, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
		if err != nil {
			return nil, fmt.Errorf("disconnect during fetch: %w", err)
		}
		return snapshot("UEXAMPLE01")(ctx, token, team, report)
	})
	require.NoError(t, syncer(f, provider).Run(ctx, input, nil))
	require.Empty(t, members(t, ctx, f))
	row := connection(t, ctx, f, input)
	require.True(t, row.DisconnectedAt.Valid)
	require.False(t, row.LastFullSyncSucceededAt.Valid)
	require.False(t, row.LastErrorCode.Valid)
}

func TestSyncReconnectDuringFetchDiscardsSnapshot(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	provider := directoryFunc(func(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		authorize(t, ctx, f, begin(t, ctx, f, &c.ID), c.WorkspaceID)
		return snapshot("UEXAMPLE01")(ctx, token, team, report)
	})
	require.NoError(t, syncer(f, provider).Run(ctx, input, nil))
	require.Empty(t, members(t, ctx, f))
	row := connection(t, ctx, f, input)
	require.NotEqual(t, input.Generation, row.Generation)
	require.False(t, row.LastFullSyncSucceededAt.Valid)
}

func TestSyncRevokedTokenRequiresReconnect(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	provider := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		return nil, &slackdirectoryconnections.SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	})
	require.Error(t, syncer(f, provider).Run(ctx, input, nil))
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "reconnect_required", list.Connections[0].Status)
	_, err = f.service.Sync(ctx, &gen.SyncPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.Error(t, err)
}

func TestSyncStaleFailureDoesNotPoisonReconnection(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	provider := directoryFunc(func(ctx context.Context, _, _ string, _ func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		authorize(t, ctx, f, begin(t, ctx, f, &c.ID), c.WorkspaceID)
		return nil, &slackdirectoryconnections.SyncError{Code: "authorization_expired", Retryable: false, Reconnect: true, RetryAfter: 0}
	})
	require.NoError(t, syncer(f, provider).Run(ctx, input, nil))
	row := connection(t, ctx, f, input)
	require.Equal(t, "connected", row.Health)
	require.False(t, row.LastSyncFailedAt.Valid)
	require.False(t, row.LastErrorCode.Valid)
}

func TestSyncBatchesAndPreservesObservedMapping(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	users := make([]string, 501)
	for i := range users {
		users[i] = fmt.Sprintf("UEXAMPLE%04d", i)
	}
	require.NoError(t, syncer(f, snapshot(users...)).Run(ctx, input, nil))
	original := members(t, ctx, f)[0]
	mapping, err := repo.New(f.db).CreateSlackMappingForTest(ctx, repo.CreateSlackMappingForTestParams{OrganizationID: input.OrganizationID, SlackTeamID: c.WorkspaceID, SlackUserID: original.SlackUserID, UserID: f.auth.UserID})
	require.NoError(t, err)
	require.NoError(t, repo.New(f.db).SetSlackMappingRevisionForTest(ctx, repo.SetSlackMappingRevisionForTestParams{OrganizationID: input.OrganizationID, ID: original.ID, MappingRevision: 9}))
	input.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, syncer(f, snapshot(users...)).Run(ctx, input, nil))
	updated := members(t, ctx, f)[0]
	require.Equal(t, original.ID, updated.ID)
	require.Equal(t, int64(9), updated.MappingRevision)
	require.Equal(t, "test_review", updated.MappingConflictReason.String)
	retained, err := repo.New(f.db).GetSlackMappingForTest(ctx, repo.GetSlackMappingForTestParams{OrganizationID: input.OrganizationID, ID: mapping.ID})
	require.NoError(t, err)
	require.Equal(t, mapping, retained)
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, int64(501), list.Connections[0].MemberCount)
}

func TestSyncProductFeatureDisabledBeforeFetch(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, f.auth.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, false))
	provider := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		t.Error("disabled feature fetched Slack profiles")
		return nil, nil
	})
	err := syncer(f, provider).Run(ctx, input, nil)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, "feature_disabled", syncErr.Code)
	require.False(t, syncErr.Retryable)
	require.False(t, connection(t, ctx, f, input).LastSyncStartedAt.Valid)
}

func TestSyncProductFeatureDisabledDuringFetchPreservesSnapshot(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	input := syncRequest(f, c)
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, input, nil))
	previous := connection(t, ctx, f, input)
	input.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	provider := directoryFunc(func(ctx context.Context, token, team string, report func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		err := f.productFeatures.SetFeatureEnabled(ctx, input.OrganizationID, productfeatures.FeatureClaudeTagSupport, false)
		if err != nil {
			return nil, fmt.Errorf("disable feature during sync: %w", err)
		}
		return snapshot("UEXAMPLE02")(ctx, token, team, report)
	})
	err := syncer(f, provider).Run(ctx, input, nil)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, "feature_disabled", syncErr.Code)
	require.Equal(t, previous.LastFullSyncSucceededAt, connection(t, ctx, f, input).LastFullSyncSucceededAt)
	rows := members(t, ctx, f)
	require.Len(t, rows, 1)
	require.Equal(t, "UEXAMPLE01", rows[0].SlackUserID)
	count, err := audittest.AuditLogCountByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionSync)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestSyncProductFeatureRecheckUsesHeldConnection(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	config := f.db.Config()
	config.MaxConns = 1
	config.MinConns = 0
	config.MinIdleConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	redis, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), pool, redis)
	worker := slackdirectoryconnections.NewDirectorySync(pool, f.enc, snapshot("UEXAMPLE01"), audit.NewLogger(), features)
	limited, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, worker.Run(limited, syncRequest(f, c), nil))
	require.Len(t, members(t, ctx, f), 1)
}
