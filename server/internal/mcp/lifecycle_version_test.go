package mcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/stretchr/testify/require"
)

func TestServePublic_Declared20260728LifecycleRequestsDoNotInitialize(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"hosted", "meta"} {
		for _, method := range []string{mcpversions.MethodInitialize, mcpversions.MethodServerDiscover} {
			t.Run(surface+"/"+method, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				authCtx, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				slug := "lifecycle-mcp"
				supported := mcpversions.SupportedHostedToolset()
				if surface == "hosted" {
					toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
					slug = toolset.McpSlug.String
				} else {
					createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
					supported = mcpversions.SupportedMetaServer()
				}
				body := makeMetaRPCBody(t, method, map[string]any{
					"protocolVersion": mcpversions.Version20251125,
					"_meta":           map[string]any{"io.modelcontextprotocol/protocolVersion": mcpversions.Version20260728},
				})
				w, err := servePublicHTTP(t, ctx, ti, slug, body, "", nil)
				require.NoError(t, err)
				requireUnsupportedProtocolVersionResponse(t, w, mcpversions.Version20260728, supported)
				require.Empty(t, w.Header().Get("Mcp-Session-Id"))
			})
		}
	}
}
