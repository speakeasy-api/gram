package remotemcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/urn"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type staticToolVariationLoader struct {
	variations []variationsrepo.ToolVariation
	err        error
	params     variationsrepo.ListByGroupIDAndToolSourceParams
}

func (l *staticToolVariationLoader) ListByGroupIDAndToolSource(
	_ context.Context,
	params variationsrepo.ListByGroupIDAndToolSourceParams,
) ([]variationsrepo.ToolVariation, error) {
	l.params = params
	return l.variations, l.err
}

func TestToolsVariationInterceptor_VariesRemoteToolsList(t *testing.T) {
	t.Parallel()

	groupID := uuid.New()
	projectID := uuid.New()
	sourceID := uuid.NewString()
	loader := &staticToolVariationLoader{
		variations: []variationsrepo.ToolVariation{
			{
				SrcToolUrn:      urn.NewTool(urn.ToolKindExternalMCP, sourceID, "import_to_google_doc"),
				Name:            pgtype.Text{String: "create_rich_doc", Valid: true},
				Description:     pgtype.Text{String: "Create a native Google Doc from rich HTML.", Valid: true},
				Title:           pgtype.Text{String: "Create rich document", Valid: true},
				ReadOnlyHint:    pgtype.Bool{Bool: false, Valid: true},
				DestructiveHint: pgtype.Bool{Bool: false, Valid: true},
				OpenWorldHint:   pgtype.Bool{Bool: true, Valid: true},
			},
		},
	}
	interceptor := remotemcp.NewToolsVariationInterceptor(
		loader,
		groupID,
		projectID,
		proxy.ServerIdentity{RemoteMCPServerID: sourceID},
	)
	resp := newToolsListResponse(t, []*mcp.Tool{
		{Name: "create_doc", Description: "Create plain text.", InputSchema: map[string]any{"type": "object"}},
		{Name: "import_to_google_doc", Description: "Import a file.", InputSchema: map[string]any{"type": "object"}},
	})

	require.NoError(t, interceptor.InterceptToolsListResponse(t.Context(), resp))
	require.Equal(t, groupID, loader.params.GroupID)
	require.Equal(t, projectID, loader.params.ProjectID)
	require.Equal(t, "externalmcp", loader.params.KindValue)
	require.Equal(t, sourceID, loader.params.SourceValue)
	require.Equal(t, "create_doc", resp.Result.Tools[0].Name)
	require.Equal(t, "create_rich_doc", resp.Result.Tools[1].Name)
	require.Equal(t, "Create a native Google Doc from rich HTML.", resp.Result.Tools[1].Description)
	require.Equal(t, "Create rich document", resp.Result.Tools[1].Annotations.Title)
	require.False(t, resp.Result.Tools[1].Annotations.ReadOnlyHint)
	require.NotNil(t, resp.Result.Tools[1].Annotations.DestructiveHint)
	require.False(t, *resp.Result.Tools[1].Annotations.DestructiveHint)
	require.NotNil(t, resp.Result.Tools[1].Annotations.OpenWorldHint)
	require.True(t, *resp.Result.Tools[1].Annotations.OpenWorldHint)
}

func TestToolsVariationInterceptor_RestoresAliasBeforeUpstreamCall(t *testing.T) {
	t.Parallel()

	sourceID := uuid.NewString()
	loader := &staticToolVariationLoader{
		variations: []variationsrepo.ToolVariation{
			{
				SrcToolUrn: urn.NewTool(urn.ToolKindExternalMCP, sourceID, "import_to_google_doc"),
				Name:       pgtype.Text{String: "create_rich_doc", Valid: true},
			},
		},
	}
	interceptor := remotemcp.NewToolsVariationInterceptor(
		loader,
		uuid.New(),
		uuid.New(),
		proxy.ServerIdentity{RemoteMCPServerID: sourceID},
	)
	params := json.RawMessage(`{"name":"create_rich_doc","arguments":{"source_format":"html"}}`)
	rpcReq := &jsonrpc.Request{Params: params}
	call := &proxy.ToolsCallRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "create_rich_doc",
			Arguments: json.RawMessage(`{"source_format":"html"}`),
		},
		UserRequest: &proxy.UserRequest{
			JSONRPCMessages: []jsonrpc.Message{rpcReq},
		},
	}

	require.NoError(t, interceptor.InterceptToolsCallRequest(t.Context(), call))
	require.Equal(t, "import_to_google_doc", call.Params.Name)

	var forwarded mcp.CallToolParamsRaw
	require.NoError(t, json.Unmarshal(rpcReq.Params, &forwarded))
	require.Equal(t, "import_to_google_doc", forwarded.Name)
	require.JSONEq(t, `{"source_format":"html"}`, string(forwarded.Arguments))
}
