package slackdirectoryconnections_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/slackscopes"
)

// unusedDirectory fails the test if messaging ever fetches the directory.
func unusedDirectory(t *testing.T) directoryFunc {
	t.Helper()
	return func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		t.Error("messaging fetched the directory")
		return nil, nil
	}
}

// failingRefresh answers every renewal with a transient provider failure.
var failingRefresh = refresherFunc(func(context.Context, string) (*slackdirectoryconnections.TokenBundle, error) {
	return nil, &slackdirectoryconnections.ProviderError{Code: "transport", RetryAfter: 0}
})

func TestMessagingWorkspacesKeepsATokenWithTimeLeftWhenRenewalFails(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	storeTokens(t, ctx, f, c, time.Now().Add(10*time.Minute))
	sync := slackdirectoryconnections.NewDirectorySync(f.db, f.enc, unusedDirectory(t), audit.NewLogger(), failingRefresh)

	workspaces, err := sync.MessagingWorkspaces(ctx, f.auth.ActiveOrganizationID, slackscopes.UsersRead)
	require.NoError(t, err)
	require.Len(t, workspaces, 1)
	require.Equal(t, "old-access", workspaces[0].Token)
}

func TestMessagingWorkspacesReportsAnExpiringTokenItCannotRenew(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	storeTokens(t, ctx, f, c, time.Now().Add(30*time.Second))
	sync := slackdirectoryconnections.NewDirectorySync(f.db, f.enc, unusedDirectory(t), audit.NewLogger(), failingRefresh)

	workspaces, err := sync.MessagingWorkspaces(ctx, f.auth.ActiveOrganizationID, slackscopes.UsersRead)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.True(t, syncErr.Retryable)
	require.Empty(t, workspaces)
}

func TestMessagingWorkspacesReportsABusySync(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	storeTokens(t, ctx, f, c, time.Now().Add(30*time.Second))
	// A sync holds the lock, and renews the token itself.
	conn, err := f.db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	lockKey := "slack_directory_sync:" + c.ID
	locked, err := repo.New(conn).TryLockSlackDirectorySync(ctx, lockKey)
	require.NoError(t, err)
	require.True(t, locked)
	defer func() {
		_, _ = repo.New(conn).UnlockSlackDirectorySync(context.WithoutCancel(ctx), lockKey)
	}()
	noRefresh := refresherFunc(func(context.Context, string) (*slackdirectoryconnections.TokenBundle, error) {
		t.Error("renewed a token while a sync holds the lock")
		return nil, nil
	})
	sync := slackdirectoryconnections.NewDirectorySync(f.db, f.enc, unusedDirectory(t), audit.NewLogger(), noRefresh)

	workspaces, err := sync.MessagingWorkspaces(ctx, f.auth.ActiveOrganizationID, slackscopes.UsersRead)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, "sync_busy", syncErr.Code)
	require.Empty(t, workspaces)
}
