package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const toolsListRPC = `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`

// newPublicToolsetForProtocolTest creates a public toolset-backed MCP server, the
// surface Gram answers from its own inventory.
func newPublicToolsetForProtocolTest(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()

	toolsetsRepo := toolsets_repo.New(ti.conn)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            conv.ToPGText("protocol gate"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            true,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)

	return toolset.McpSlug.String
}

// TestServePublic_RejectsHandshakelessProtocolVersion is the regression test for a
// client on 2026-07-28 receiving a result shaped for the revision this surface
// actually answers. There is no `initialize` on that revision in which to tell it
// otherwise, so the refusal is the only signal it gets — and the spec's fallback
// flow is built on receiving it.
func TestServePublic_RejectsHandshakelessProtocolVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	slug := newPublicToolsetForProtocolTest(t, ctx, ti, "gate-header")

	w, err := servePublicHTTP(t, ctx, ti, slug, []byte(toolsListRPC), "",
		map[string]string{mcpversions.HTTPHeader: mcpversions.Version20260728})
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())

	var envelope struct {
		JSONRPC string `json:"jsonrpc"`
		ID      any    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    struct {
				Supported []string `json:"supported"`
			} `json:"data"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), "body: %s", w.Body.String())
	require.Equal(t, "2.0", envelope.JSONRPC)
	require.Equal(t, -32022, envelope.Error.Code, "the code MCP allocates for an unsupported revision")
	require.Equal(t, []string{mcpversions.ServedHostedToolset}, envelope.Error.Data.Supported,
		"the client needs a revision to retry with")
	require.InDelta(t, 1, envelope.ID, 0, "the refusal must correlate to the request")
}

// TestServePublic_RejectsHandshakelessProtocolVersionFromMeta covers the same
// declaration made only in `_meta`, which is where 2026-07-28 puts it; the header
// merely mirrors it.
func TestServePublic_RejectsHandshakelessProtocolVersionFromMeta(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	slug := newPublicToolsetForProtocolTest(t, ctx, ti, "gate-meta")

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	w, err := servePublicHTTP(t, ctx, ti, slug, []byte(body), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	require.Contains(t, w.Body.String(), "-32022")
}

// TestServePublic_ServesHandshakeEraProtocolVersions pins the blast radius. Every
// revision that agrees a version at `initialize` keeps working exactly as before,
// including ones newer than the surface answers: such a client is told what the
// server speaks and adapts.
func TestServePublic_ServesHandshakeEraProtocolVersions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	slug := newPublicToolsetForProtocolTest(t, ctx, ti, "gate-legacy")

	for name, header := range map[string]string{
		"the served revision":       mcpversions.Version20250326,
		"a newer handshake version": mcpversions.Version20250618,
		"the newest handshake one":  mcpversions.Version20251125,
		"an unrecognized version":   "2099-01-01",
		"no version at all":         "",
	} {
		headers := map[string]string{}
		if header != "" {
			headers[mcpversions.HTTPHeader] = header
		}

		w, err := servePublicHTTP(t, ctx, ti, slug, []byte(toolsListRPC), "", headers)
		require.NoError(t, err, name)
		require.Equal(t, http.StatusOK, w.Code, "%s must keep working: %s", name, w.Body.String())
	}
}

// TestServePublic_ToolsListEmptyArrayNotNull covers the shape violation the gate
// work surfaced: a toolset resolving to no tools answered `"tools":null`, and MCP
// list results carry an array.
func TestServePublic_ToolsListEmptyArrayNotNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	slug := newPublicToolsetForProtocolTest(t, ctx, ti, "gate-empty")

	w, err := servePublicHTTP(t, ctx, ti, slug, []byte(toolsListRPC), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	var envelope struct {
		Result struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), "body: %s", w.Body.String())
	require.JSONEq(t, `[]`, string(envelope.Result.Tools), "an empty tool list is an empty array, not null")
}
