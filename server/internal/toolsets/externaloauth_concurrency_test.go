package toolsets_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestToolsetsService_PrivateMCPNeverRetainsExternalOAuthDuringConcurrentUpdates(t *testing.T) {
	t.Parallel()

	for _, firstOperation := range []string{"add external OAuth", "make private"} {
		t.Run(firstOperation+" first", func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestToolsetsService(t)
			ctx = withAccountType(t, ctx, "pro")
			toolset := createMinimalPublicToolset(t, ctx, ti, "Concurrent External OAuth "+firstOperation)

			blocker := testenv.BeginTx(t, ctx, ti.conn)
			t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(ctx)) })
			var blockerPID int32
			//nolint:glint // notestingrawsql: backend identity is a PostgreSQL test synchronization primitive
			require.NoError(t, blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID))
			//nolint:glint // notestingrawsql: intentionally holds the row lock shared by the two service operations
			_, err := blocker.Exec(ctx, `
SELECT id FROM toolsets WHERE project_id = $1 AND slug = $2 FOR UPDATE
`, toolset.ProjectID, toolset.Slug)
			require.NoError(t, err)

			add := func() error {
				_, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
					Slug: toolset.Slug,
					ExternalOauthServer: &types.ExternalOAuthServerForm{
						Metadata: map[string]any{"issuer": "https://example.com"},
					},
				})
				if err != nil {
					return fmt.Errorf("add external OAuth server: %w", err)
				}
				return nil
			}
			makePrivate := func() error {
				_, err := ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
					Slug:        toolset.Slug,
					McpIsPublic: new(false),
				})
				if err != nil {
					return fmt.Errorf("make MCP private: %w", err)
				}
				return nil
			}

			first, second := add, makePrivate
			if firstOperation == "make private" {
				first, second = makePrivate, add
			}

			firstResult := make(chan error, 1)
			secondResult := make(chan error, 1)
			go func() { firstResult <- first() }()
			waitForLockWaiters(t, ctx, ti, blockerPID, 1)
			go func() { secondResult <- second() }()
			waitForLockWaiters(t, ctx, ti, blockerPID, 2)

			require.NoError(t, blocker.Commit(ctx))
			firstErr, secondErr := <-firstResult, <-secondResult
			if firstOperation == "add external OAuth" {
				require.NoError(t, firstErr)
				require.NoError(t, secondErr)
			} else {
				require.NoError(t, firstErr)
				var oopsErr *oops.ShareableError
				require.ErrorAs(t, secondErr, &oopsErr)
				require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
			}

			got, err := ti.service.GetToolset(ctx, &gen.GetToolsetPayload{Slug: toolset.Slug})
			require.NoError(t, err)
			require.False(t, *got.McpIsPublic)
			require.Nil(t, got.ExternalOauthServer)
		})
	}
}

func waitForLockWaiters(t *testing.T, ctx context.Context, ti *testInstance, blockerPID int32, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var got int
		//nolint:glint // notestingrawsql: pg_blocking_pids is a PostgreSQL test synchronization primitive
		require.NoError(t, ti.conn.QueryRow(ctx, `
WITH RECURSIVE blocked(pid) AS (
  SELECT pid
  FROM pg_stat_activity
  WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid))
  UNION
  SELECT activity.pid
  FROM pg_stat_activity AS activity
  JOIN blocked AS blocker ON blocker.pid = ANY(pg_blocking_pids(activity.pid))
  WHERE activity.datname = current_database()
)
SELECT count(*) FROM blocked
`, blockerPID).Scan(&got))
		if got >= want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("timed out waiting for %d blocked database operations", want)
}
