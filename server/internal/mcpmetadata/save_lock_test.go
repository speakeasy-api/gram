package mcpmetadata_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_metadata"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// A save waits for a concurrent save of the same backend, so it sees a row that save inserted under the other key.
func TestSetMcpMetadata_SerializesConcurrentSavesAcrossKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPMetadataService(t)
	toolset := createHostedToolset(t, ctx, ti, "hosted-save-lock")
	createCanonicalWrapper(t, ctx, ti, toolset)
	seedServerKeyedTwin(t, ctx, ti, toolset, "server", "SERVER_KEY")

	// A save on a pod from before the server-key writers holds the backend and inserts a toolset-keyed row.
	tx := testenv.BeginTx(t, ctx, ti.conn)
	other := repo.New(tx)
	require.NoError(t, other.AcquireMetadataBackendLock(ctx, repo.AcquireMetadataBackendLockParams{ProjectID: toolset.ProjectID, BackendID: toolset.ID}))
	inserted, err := testrepo.New(tx).InsertToolsetKeyedMcpMetadataFixture(ctx, testrepo.InsertToolsetKeyedMcpMetadataFixtureParams{
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ProjectID: toolset.ProjectID,
		Instructions: conv.ToPGText("toolset"), HeaderDisplayNames: []byte(`{}`), DefaultEnvironmentID: uuid.NullUUID{},
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
	require.Equal(t, inserted.String(), got.metadata.ID, "the toolset-keyed row wins and is re-keyed")
	require.Equal(t, "after", *got.metadata.Instructions)
	requireHostedIDs(t, ctx, ti, got.metadata, toolset.ID, true)
	requireSingleMetadataRow(t, ctx, ti, toolset.ID)
}
