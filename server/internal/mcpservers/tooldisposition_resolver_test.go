package mcpservers_test

// The resolver under test (remotemcp.ToolDispositionResolver) lives in the
// remotemcp package, but its integration test lives here: it reads the
// mcp_server_tool_metadata rows this package owns, and the remote-backed-server
// + metadata fixtures (createRemoteBackedMcpServer, AddToolMetadataBatch) are
// one-liners here versus a full FK chain to hand-roll in remotemcp. ti.dispositions
// is the same resolver instance wired as the service's cache invalidator, so
// writes and reads share one cache.

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestToolDispositionResolver_DerivesFromMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "list_items", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
			{ToolName: "delete_item", Title: nil, ReadOnlyHint: new(false), DestructiveHint: new(true), IdempotentHint: nil, OpenWorldHint: nil},
			// Reviewed with no behavior class (every hint unset): omitted from the
			// map, since an absent key reads as the empty disposition anyway.
			{ToolName: "ping", Title: nil, ReadOnlyHint: nil, DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	got, err := ti.dispositions.Dispositions(ctx, serverID, authCtx.ProjectID.String())
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"list_items":  "read_only",
		"delete_item": "destructive",
	}, got)
}

func TestToolDispositionResolver_UnknownServerResolvesEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// A remote-backed server with no recorded metadata resolves (successfully)
	// to an empty map — the negative-cache case.
	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	got, err := ti.dispositions.Dispositions(ctx, serverID, authCtx.ProjectID.String())
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestToolDispositionResolver_ServiceWriteInvalidatesCache proves the write path
// evicts the read cache: a delete through the service is reflected on the next
// resolve rather than waiting out the TTL, because the service shares this
// resolver as its cache invalidator.
func TestToolDispositionResolver_ServiceWriteInvalidatesCache(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)
	_, err := ti.service.AddToolMetadataBatch(ctx, &gen.AddToolMetadataBatchPayload{
		McpServerID: serverID,
		Tools: []*gen.ToolMetadataForm{
			{ToolName: "list_items", Title: nil, ReadOnlyHint: new(true), DestructiveHint: nil, IdempotentHint: nil, OpenWorldHint: nil},
		},
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Warm the cache.
	first, err := ti.dispositions.Dispositions(ctx, serverID, authCtx.ProjectID.String())
	require.NoError(t, err)
	require.Equal(t, "read_only", first["list_items"])

	// A delete through the service invalidates the cached view...
	err = ti.service.DeleteToolMetadata(ctx, &gen.DeleteToolMetadataPayload{
		McpServerID:      serverID,
		ToolName:         "list_items",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// ...so the next resolve reflects the deletion immediately.
	second, err := ti.dispositions.Dispositions(ctx, serverID, authCtx.ProjectID.String())
	require.NoError(t, err)
	require.Empty(t, second)
}

func TestToolDispositionResolver_InvalidServerIDFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.dispositions.Dispositions(ctx, "not-a-uuid", authCtx.ProjectID.String())
	require.Error(t, err)
}

func TestToolDispositionResolver_InvalidProjectIDFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverID := createRemoteBackedMcpServer(t, ctx, ti)

	_, err := ti.dispositions.Dispositions(ctx, serverID, "not-a-uuid")
	require.Error(t, err)
}
