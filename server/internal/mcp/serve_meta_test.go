// serve_meta_test.go verifies MCP protocol termination for meta-MCP-backed
// /mcp/{slug} endpoints: 2026-07-28 initialize and server/discover, the fixed
// four-tool contract, per-request protocol-version declarations, the issuer
// gate, and the no-/x/mcp-exposure rule.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// createMetaMcpEndpoint writes a meta_mcp_servers row and an mcp_endpoints
// row exposing it under slug. issuerID, when non-Nil, gates the endpoint.
func createMetaMcpEndpoint(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	organizationID string,
	slug string,
	issuerID uuid.UUID,
) metamcprepo.MetaMcpServer {
	t.Helper()

	var issuer uuid.NullUUID
	if issuerID != uuid.Nil {
		issuer = uuid.NullUUID{UUID: issuerID, Valid: true}
	}

	meta, err := metamcprepo.New(conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      organizationID,
		ProjectID:           projectID,
		Name:                "test gateway",
		UserSessionIssuerID: issuer,
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       projectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: uuid.NullUUID{UUID: meta.ID, Valid: true},
		Slug:            slug,
	})
	require.NoError(t, err)

	return meta
}

// seedMetaMember creates a member mcp_server (remote-backed, named and
// slugged) and attaches it to the meta server at the given sort order.
func seedMetaMember(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	projectID uuid.UUID,
	metaID uuid.UUID,
	name, slug string,
	sortOrder int32,
) {
	t.Helper()

	remote := remotemcptest.SeedServer(t, ctx, conn, remotemcprepo.CreateServerParams{
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           "https://member.example.com/mcp/" + uuid.NewString(),
	})

	// Remote-backed servers must carry an issuer (mcp_servers_issuer_required_check).
	memberIssuerID := createUserSessionIssuer(t, ctx, conn, projectID)

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           projectID,
		Name:                conv.ToPGText(name),
		Slug:                conv.ToPGText(slug),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: memberIssuerID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		ToolsetID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:          "private",
	})
	require.NoError(t, err)

	_, err = metamcprepo.New(conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: metaID,
		McpServerID:     server.ID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)
}

func makeMetaRPCBody(t *testing.T, method string, params map[string]any) []byte {
	t.Helper()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		reqBody["params"] = params
	}
	bs, err := json.Marshal(reqBody)
	require.NoError(t, err)
	return bs
}

func decodeRPCResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope), "body=%s", w.Body.String())
	return envelope
}

func TestServePublic_MetaEndpoint_Initialize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "member alpha", "member-alpha-"+uuid.NewString()[:8], 0)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Equal(t, mcpversions.ServedMetaServer, w.Header().Get(mcpversions.HTTPHeader))

	envelope := decodeRPCResponse(t, w)
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(envelope["result"], &result))
	require.Equal(t, mcpversions.ServedMetaServer, result.ProtocolVersion)
	require.Equal(t, "Gram Gateway", result.ServerInfo.Name)
	// Instructions are deliberately generic: the member inventory belongs to
	// list_servers, so neither the meta server's name nor its members appear.
	require.Contains(t, result.Instructions, "list_servers")
	require.Contains(t, result.Instructions, "rediscovery")
	require.NotContains(t, result.Instructions, "test gateway")
}

func TestServePublic_MetaEndpoint_ServerDiscover(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "server/discover", nil), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	envelope := decodeRPCResponse(t, w)
	var result struct {
		ProtocolVersions []string `json:"protocolVersions"`
		ServerInfo       struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	require.NoError(t, json.Unmarshal(envelope["result"], &result))
	require.Equal(t, []string{mcpversions.ServedMetaServer}, result.ProtocolVersions)
	require.Equal(t, "Gram Gateway", result.ServerInfo.Name)
}

func TestServePublic_MetaEndpoint_ToolsList_FixedContract(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	envelope := decodeRPCResponse(t, w)
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(envelope["result"], &result))

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	require.Equal(t, []string{"list_servers", "describe_server", "describe_tools", "execute_tool"}, names)
}

func TestServePublic_MetaEndpoint_ListServers_ReturnsOrderedMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	secondSlug := "member-second-" + uuid.NewString()[:8]
	firstSlug := "member-first-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "member second", secondSlug, 2)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "member first", firstSlug, 1)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	envelope := decodeRPCResponse(t, w)
	var result struct {
		Content           []map[string]json.RawMessage `json:"content"`
		StructuredContent struct {
			Servers []struct {
				Slug      string `json:"slug"`
				Name      string `json:"name"`
				SortOrder int    `json:"sortOrder"`
				Status    string `json:"status"`
			} `json:"servers"`
		} `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(envelope["result"], &result))
	require.Len(t, result.StructuredContent.Servers, 2)
	require.Equal(t, firstSlug, result.StructuredContent.Servers[0].Slug)
	require.Equal(t, secondSlug, result.StructuredContent.Servers[1].Slug)
	require.Equal(t, "unknown", result.StructuredContent.Servers[0].Status)

	require.Len(t, result.Content, 1)
	require.Contains(t, result.Content[0], "text")
	require.NotContains(t, result.Content[0], "data", "text content chunks must not carry a data member")
}

func TestServePublic_MetaEndpoint_DrillDownToolsNotYetAvailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	for _, tool := range []string{"describe_server", "describe_tools", "execute_tool"} {
		w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
			"name":      tool,
			"arguments": map[string]any{},
		}), "", nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code)

		envelope := decodeRPCResponse(t, w)
		require.Contains(t, string(envelope["error"]), "not yet available", "tool %s must answer deterministically", tool)
	}
}

func TestServePublic_MetaEndpoint_UnsupportedDeclaredVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", map[string]string{
		mcpversions.HTTPHeader: "2031-01-01",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, mcpversions.ServedMetaServer, w.Header().Get(mcpversions.HTTPHeader))

	envelope := decodeRPCResponse(t, w)
	require.Contains(t, string(envelope["error"]), "unsupported protocol version")
	require.Contains(t, string(envelope["error"]), mcpversions.ServedMetaServer)
}

// TestServePublic_MetaEndpoint_UnsanitizableDeclaredVersion pins that a
// declared version that fails sanitization (here: an embedded control byte)
// is treated as a malformed declaration — the structured unsupported-version
// error — rather than silently collapsing to "absent", and that the hostile
// raw bytes are never echoed back.
func TestServePublic_MetaEndpoint_UnsanitizableDeclaredVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", map[string]string{
		mcpversions.HTTPHeader: "2026-07-28\x00hostile",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, mcpversions.ServedMetaServer, w.Header().Get(mcpversions.HTTPHeader))

	envelope := decodeRPCResponse(t, w)
	require.Contains(t, string(envelope["error"]), "unsupported protocol version")
	require.Contains(t, string(envelope["error"]), "(unparseable)")
	require.NotContains(t, w.Body.String(), "hostile", "raw declaration bytes must not be echoed")
}

// TestServePublic_MetaEndpoint_MistypedMetaVersionDeclaration pins that a
// `_meta` protocol-version member that is present but not a string is a
// malformed declaration — the structured unsupported-version error — rather
// than silently collapsing to "absent".
func TestServePublic_MetaEndpoint_MistypedMetaVersionDeclaration(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion": 20260728,
		},
	}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	envelope := decodeRPCResponse(t, w)
	require.Contains(t, string(envelope["error"]), "unsupported protocol version")
	require.Contains(t, string(envelope["error"]), "(unparseable)")
}

func TestServePublic_MetaEndpoint_ConflictingVersionDeclarations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{
		"_meta": map[string]any{
			"io.modelcontextprotocol/protocolVersion": mcpversions.Version20251125,
		},
	}), "", map[string]string{
		mcpversions.HTTPHeader: mcpversions.Version20260728,
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)

	envelope := decodeRPCResponse(t, w)
	require.Contains(t, string(envelope["error"]), "conflicting protocol version declarations")
}

func TestServePublic_MetaEndpoint_IssuerGated_NoAuth_EmitsChallenge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)

	// The 401 status itself is written by the error middleware in
	// production; the in-handler contract is the returned error plus the
	// WWW-Authenticate challenge pointing at this endpoint's resource
	// metadata on the /mcp surface.
	w, err := servePublicHTTP(t, ctx, ti, slug, makeInitializeBody(), "", nil)
	require.Error(t, err, "issuer-gated meta endpoint must reject unauthenticated requests")
	wwwAuth := w.Header().Get("WWW-Authenticate")
	expected := `Bearer resource_metadata="` + ti.serverURL.String() + `/.well-known/oauth-protected-resource/mcp/` + slug + `"`
	require.Equal(t, expected, wwwAuth)
	// The served-version header is stable regardless of outcome and is
	// stamped before the issuer gate can bail out.
	require.Equal(t, mcpversions.ServedMetaServer, w.Header().Get(mcpversions.HTTPHeader))
}

func TestServeMCPEndpoint_MetaEndpoint_NoXmcpExposure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	req := httptest.NewRequest(http.MethodPost, "/x/mcp/"+slug, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	err := ti.service.ServeMCPEndpoint(w, req, slug, "x/mcp")
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestWellKnown_MetaEndpoint_IssuerGated_ServesMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)

	logger := ti.logger
	mcpEndpoint, mcpServer, metaServer, err := ti.service.ResolveMCPEndpointAndServer(ctx, logger, slug)
	require.NoError(t, err)
	require.Nil(t, mcpServer)
	require.NotNil(t, metaServer)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+slug, nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	require.NoError(t, ti.service.ServeWellKnownProtectedResourceForMetaServer(ctx, w, req, logger, mcpEndpoint, metaServer, "mcp"))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "authorization_servers")
	require.Contains(t, w.Body.String(), "/mcp/"+slug)

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/"+slug, nil)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	require.NoError(t, ti.service.ServeWellKnownAuthorizationServerForMetaServer(ctx, w, req, logger, mcpEndpoint, metaServer, "mcp"))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "authorization_endpoint")
}

func TestWellKnown_MetaEndpoint_NoIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	logger := ti.logger
	mcpEndpoint, _, metaServer, err := ti.service.ResolveMCPEndpointAndServer(ctx, logger, slug)
	require.NoError(t, err)
	require.NotNil(t, metaServer)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/"+slug, nil)
	w := httptest.NewRecorder()
	err = ti.service.ServeWellKnownProtectedResourceForMetaServer(ctx, w, req, logger, mcpEndpoint, metaServer, "mcp")
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// TestHandleConsentMCP_MetaEndpoint_NotFound pins that the consent
// tool-picker transport does not exist for meta-MCP-backed endpoints: they
// have no per-tool picker (member tool catalogs land with the AGE-3291
// runtime), so the transport answers NotFound — not the BadRequest a
// missing-consent-headers request would otherwise get, and never the
// "neither toolset nor mcp server backend" 500. The test harness enables the
// consent_tool_filtering feature for the org, so the filtering gate is not
// what produces the 404 here.
func TestHandleConsentMCP_MetaEndpoint_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+"/connect/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.HandleConsentMCP(w, req)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
