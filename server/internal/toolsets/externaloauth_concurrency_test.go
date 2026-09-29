package toolsets_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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
			blockerPID := testenv.BackendPID(blocker)
			_, err := toolsetsrepo.New(blocker).GetToolsetForUpdate(ctx, toolsetsrepo.GetToolsetForUpdateParams{
				Slug:      string(toolset.Slug),
				ProjectID: uuid.MustParse(toolset.ProjectID),
			})
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
			testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, blockerPID, 1)
			go func() { secondResult <- second() }()
			testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, blockerPID, 2)

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

func TestToolsetsService_ExternalOAuthWritersTakeToolsetLock(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"update external OAuth", "remove external OAuth"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestToolsetsService(t)
			ctx = withAccountType(t, ctx, "pro")
			toolset := createMinimalPublicToolset(t, ctx, ti, "Lock "+operation)

			attached, err := ti.service.AddExternalOAuthServer(ctx, &gen.AddExternalOAuthServerPayload{
				Slug: toolset.Slug,
				ExternalOauthServer: &types.ExternalOAuthServerForm{
					Metadata: map[string]any{"issuer": "https://example.com"},
				},
			})
			require.NoError(t, err)

			blocker := testenv.BeginTx(t, ctx, ti.conn)
			t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(ctx)) })
			blockerPID := testenv.BackendPID(blocker)
			_, err = toolsetsrepo.New(blocker).GetToolsetForUpdate(ctx, toolsetsrepo.GetToolsetForUpdateParams{
				Slug:      string(toolset.Slug),
				ProjectID: uuid.MustParse(toolset.ProjectID),
			})
			require.NoError(t, err)

			result := make(chan error, 1)
			go func() {
				if operation == "update external OAuth" {
					_, err := ti.service.UpdateExternalOAuthServer(ctx, &gen.UpdateExternalOAuthServerPayload{
						Slug: toolset.Slug, Metadata: map[string]any{"issuer": "https://updated.example.com"},
					})
					result <- err
					return
				}
				_, err := ti.service.RemoveOAuthServer(ctx, &gen.RemoveOAuthServerPayload{Slug: toolset.Slug})
				result <- err
			}()

			testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, blockerPID, 1)
			requireMetadataUnlocked(t, ctx, ti, attached.ExternalOauthServer.ID)
			require.NoError(t, blocker.Commit(ctx))
			require.NoError(t, <-result)
		})
	}
}

func requireMetadataUnlocked(t *testing.T, ctx context.Context, ti *testInstance, externalOAuthServerID string) {
	t.Helper()
	probe := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { require.NoError(t, probe.Rollback(ctx)) }()

	_, err := testrepo.New(probe).LockExternalOAuthMetadataNowaitFixture(ctx, uuid.MustParse(externalOAuthServerID))
	require.NoError(t, err)
}
