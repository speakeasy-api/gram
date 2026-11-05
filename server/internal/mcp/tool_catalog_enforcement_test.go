package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newDestructiveToolUpstream serves one tool the upstream marks destructive and
// counts the tools/call requests that reach it.
func newDestructiveToolUpstream(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &req)

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2025-03-26",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "upstream", "version": "1.0"},
			}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{
				"name":        "ping",
				"inputSchema": map[string]any{"type": "object"},
				"annotations": map[string]any{"destructiveHint": true},
			}}}
		case "tools/call":
			calls.Add(1)
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}}
		default:
			result = map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func seedDestructiveBlock(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	selector := authz.NewSelector(authz.ScopeMCPBlockedConnect, serverID.String())
	selector[authz.SelectorKeyDisposition] = authz.DispositionDestructive
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID),
		Scope:          string(authz.ScopeMCPBlockedConnect),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func storedDestructiveHint(t *testing.T, ctx context.Context, ti *testInstance, serverID uuid.UUID) bool {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	rows, err := mcpserversrepo.New(ti.conn).ListMCPServerToolMetadata(ctx, mcpserversrepo.ListMCPServerToolMetadataParams{
		McpServerID:    serverID,
		ProjectID:      *authCtx.ProjectID,
		IncludeDeleted: false,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0].DestructiveHint.Valid && rows[0].DestructiveHint.Bool
}

func TestServePublic_DestructiveBlockAppliesToCallForToolMissingFromCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.clientCredentials.use(&scriptedClientCredentials{tokens: []string{"self-token"}})
	upstream, calls := newDestructiveToolUpstream(t)
	endpoint := newSelfClientEndpointWithEmptyCatalog(t, ctx, ti, upstream.URL)
	seedDestructiveBlock(t, ctx, ti, endpoint.mcpServerID)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, toolsCallBody(t), endpoint.token, nil)
	require.NoError(t, err)

	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	require.NotNil(t, resp.Error, "the call must be refused: %s", w.Body.String())

	require.Zero(t, calls.Load(), "the blocked call must not reach the upstream")
	require.True(t, storedDestructiveHint(t, ctx, ti, endpoint.mcpServerID))
}

func TestServePublic_DestructiveBlockAppliesToListingOfToolMissingFromCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	ti.clientCredentials.use(&scriptedClientCredentials{tokens: []string{"self-token"}})
	upstream, _ := newDestructiveToolUpstream(t)
	endpoint := newSelfClientEndpointWithEmptyCatalog(t, ctx, ti, upstream.URL)
	seedDestructiveBlock(t, ctx, ti, endpoint.mcpServerID)

	w, err := servePublicHTTP(t, context.Background(), ti, endpoint.slug, makeToolsListBody(), endpoint.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "tools/list: %s", w.Body.String())

	result := decodeMCPResult(t, w.Body.Bytes())
	require.Empty(t, result["tools"], "the blocked tool must be dropped from the same listing that records it")
	require.True(t, storedDestructiveHint(t, ctx, ti, endpoint.mcpServerID))
}
