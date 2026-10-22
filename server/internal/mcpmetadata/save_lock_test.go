package mcpmetadata_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// A save waits for a concurrent save of the same backend, so it updates the row that save inserted.
func TestSetMcpMetadata_SerializesConcurrentSaves(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-save-lock")
	createCanonicalWrapper(t, ctx, ti, toolset)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	other := repo.New(tx)
	require.NoError(t, other.AcquireMetadataBackendLock(ctx, repo.AcquireMetadataBackendLockParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID}))
	inserted, err := other.UpsertMetadataByMcpServerID(ctx, repo.UpsertMetadataByMcpServerIDParams{
		McpServerID: toolset.ID, ProjectID: toolset.ProjectID,
		ExternalDocumentationUrl: pgtype.Text{String: "", Valid: false}, ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
		LogoID: uuid.NullUUID{}, Instructions: conv.ToPGText("other"), DefaultEnvironmentID: uuid.NullUUID{},
		InstallationOverrideUrl: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	type result struct {
		metadata *types.McpMetadata
		err      error
	}
	done := make(chan result, 1)
	go func() {
		saved, err := ti.service.SetMcpMetadata(ctx, &gen.SetMcpMetadataPayload{ToolsetSlug: new(types.Slug(toolset.Slug)), Instructions: new("after")})
		done <- result{metadata: saved, err: err}
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), "%AcquireMetadataBackendLock%")
	require.Empty(t, done, "the save waits for the backend lock")
	require.NoError(t, tx.Commit(ctx))

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, inserted.ID.String(), got.metadata.ID, "the save updates the row the other save inserted")
	require.Equal(t, "after", *got.metadata.Instructions)
	requireHostedIDs(t, ctx, ti, got.metadata, toolset.ID)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)
}
