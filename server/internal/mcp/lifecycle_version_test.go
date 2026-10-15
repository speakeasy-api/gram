package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// An initialize declaring 2026-07-28 never starts a handshake, even when its
// body also proposes a handshake revision: that revision has no initialize, so
// no protocol version is negotiated and no session is assigned. The
// declaration is made both the conformant way, mirrored into the header with
// the required metadata, and in `_meta` alone, which a server may reject for
// the missing header but must not answer with a handshake.
func TestServePublic_Declared20260728InitializeDoesNotStartHandshake(t *testing.T) {
	t.Parallel()
	for _, surface := range []string{"hosted", "meta"} {
		for _, shape := range []string{"conformant", "meta only"} {
			t.Run(surface+"/"+shape, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				authCtx, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				slug := "lifecycle-" + uuid.NewString()[:8]
				if surface == "hosted" {
					toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
					slug = toolset.McpSlug.String
				} else {
					createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
				}

				meta := map[string]any{"io.modelcontextprotocol/protocolVersion": mcpversions.Version20260728}
				headers := map[string]string{}
				if shape == "conformant" {
					meta["io.modelcontextprotocol/clientInfo"] = map[string]any{"name": "lifecycle-client", "version": "1.0.0"}
					meta["io.modelcontextprotocol/clientCapabilities"] = map[string]any{}
					headers[mcpversions.HTTPHeader] = mcpversions.Version20260728
					headers["Mcp-Method"] = mcpversions.MethodInitialize
				}
				body := makeMetaRPCBody(t, mcpversions.MethodInitialize, map[string]any{
					"protocolVersion": mcpversions.Version20251125,
					"capabilities":    map[string]any{},
					"clientInfo":      map[string]any{"name": "lifecycle-client", "version": "1.0.0"},
					"_meta":           meta,
				})

				w, err := servePublicHTTP(t, ctx, ti, slug, body, "", headers)
				require.NoError(t, err)
				require.Empty(t, w.Header().Get("Mcp-Session-Id"), "no handshake, so no session")

				var response struct {
					Result json.RawMessage `json:"result"`
					Error  *struct {
						Code oops.MCPCode `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "body=%s", w.Body.String())
				require.Empty(t, response.Result, "initialize must not be answered: body=%s", w.Body.String())
				require.NotNil(t, response.Error, "body=%s", w.Body.String())
				if shape == "conformant" {
					require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
					require.Equal(t, oops.MCPCodeMethodNotFound, response.Error.Code)
				}
			})
		}
	}
}
