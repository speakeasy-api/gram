// serve_meta_runtime_test.go verifies the meta MCP drill-down runtime:
// describe_server/describe_tools/execute_tool routing, qualified-name
// handling, member exclusion rules (including the refusal of hosted members),
// the notification/version interaction, and the challenge-resumption
// resolver.
package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// seedToolsMetaMember attaches a public remote member backed by a live MCP
// upstream serving the named tools, returning the member's slug.
func seedToolsMetaMember(t *testing.T, ctx context.Context, ti *testInstance, metaID uuid.UUID, name string, sortOrder int32, toolNames ...string) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	tools := make([]testmcp.Tool, 0, len(toolNames))
	for _, toolName := range toolNames {
		tools = append(tools, testmcp.Tool{
			Name:        toolName,
			Description: "returns pong",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Response:    testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "pong from " + toolName}}},
		})
	}
	upstream := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: tools})
	t.Cleanup(upstream.Close)

	slug := "member-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, metaID, name, slug, sortOrder, upstream.URL)
	return slug
}

func callMetaTool(t *testing.T, ctx context.Context, ti *testInstance, endpointSlug, tool string, arguments map[string]any) map[string]json.RawMessage {
	t.Helper()

	w, err := servePublicHTTP(t, ctx, ti, endpointSlug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      tool,
		"arguments": arguments,
	}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	return decodeRPCResponse(t, w)
}

type metaStructuredToolResult struct {
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

func decodeMetaToolResult(t *testing.T, envelope map[string]json.RawMessage) metaStructuredToolResult {
	t.Helper()
	require.NotContains(t, envelope, "error", "expected a tool result, got error: %s", string(envelope["error"]))
	var result metaStructuredToolResult
	require.NoError(t, json.Unmarshal(envelope["result"], &result))
	return result
}

func TestServePublic_MetaEndpoint_DescribeServer_ListsMemberCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedToolsMetaMember(t, ctx, ti, meta.ID, "remote member", 1, "alpha_tool", "beta_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": member})
	result := decodeMetaToolResult(t, envelope)
	require.False(t, result.IsError)

	var described struct {
		Server struct {
			Slug   string `json:"slug"`
			Status string `json:"status"`
		} `json:"server"`
		Tools []map[string]json.RawMessage `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &described))
	require.Equal(t, member, described.Server.Slug)
	require.Equal(t, "unknown", described.Server.Status, "a remote member's status stays unknown until cached health exists")

	names := make([]string, 0, len(described.Tools))
	for _, tool := range described.Tools {
		var name string
		require.NoError(t, json.Unmarshal(tool["name"], &name))
		names = append(names, name)
		// Catalog listing deliberately carries no schemas.
		require.NotContains(t, tool, "inputSchema")
	}
	require.ElementsMatch(t, []string{member + "--alpha_tool", member + "--beta_tool"}, names)
}

func TestServePublic_MetaEndpoint_DescribeTools_ReportsMissingNames(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedToolsMetaMember(t, ctx, ti, meta.ID, "remote member", 1, "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "describe_tools", map[string]any{
		"tools": []string{member + "--alpha_tool", member + "--missing_tool"},
	})
	result := decodeMetaToolResult(t, envelope)
	require.False(t, result.IsError)

	var described struct {
		Tools []struct {
			Name        string          `json:"name"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
		NotFound []string `json:"not_found"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &described))
	require.Len(t, described.Tools, 1)
	require.Equal(t, member+"--alpha_tool", described.Tools[0].Name)
	require.NotEmpty(t, described.Tools[0].InputSchema, "describe_tools must carry the full input schema")
	require.Equal(t, []string{member + "--missing_tool"}, described.NotFound)
}

func TestServePublic_MetaEndpoint_DescribeServer_UnknownMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	envelope := callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": "nonexistent"})
	require.Contains(t, string(envelope["error"]), "unknown server")
}

func TestServePublic_MetaEndpoint_ExecuteTool_MalformedName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      "unqualifiedname",
		"arguments": map[string]any{},
	})
	require.Contains(t, string(envelope["error"]), "must be of the form serverslug--toolname")
}

func TestServePublic_MetaEndpoint_ExecuteTool_Dispatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedToolsMetaMember(t, ctx, ti, meta.ID, "remote member", 1, "alpha_tool")

	// The outer _meta rides along to prove forwarding does not disturb routing.
	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": "execute_tool",
		"arguments": map[string]any{
			"name":      member + "--alpha_tool",
			"arguments": map[string]any{},
		},
		"_meta": map[string]any{
			"io.modelcontextprotocol/clientInfo": map[string]any{"name": "meta-test-client", "version": "1.2.3"},
		},
	}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	text, isError := metaToolResultText(t, decodeRPCResponse(t, w))
	require.False(t, isError, "text=%s", text)
	require.Contains(t, text, "pong from alpha_tool")
}

func TestServePublic_MetaEndpoint_ExecuteTool_UnknownToolOnKnownMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedToolsMetaMember(t, ctx, ti, meta.ID, "remote member", 1, "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      member + "--no_such_tool",
		"arguments": map[string]any{},
	})
	require.NotContains(t, envelope, "error", "an unknown tool on a known member must not abort the call: %s", string(envelope["error"]))
	_, isError := metaToolResultText(t, envelope)
	require.True(t, isError, "an unknown tool on a known member is a member-scoped failure")
}

func TestServePublic_MetaEndpoint_ListServers_StatusByBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	proxiedSlug := "member-remote-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", proxiedSlug, 2, mcpservers.VisibilityPrivate)
	tunneledSlug := "member-tunnel-" + uuid.NewString()[:8]
	tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *authCtx.ProjectID, meta.ID, "tunneled member", tunneledSlug, 3, "")

	listStatuses := func() map[string]string {
		envelope := callMetaTool(t, ctx, ti, slug, "list_servers", map[string]any{})
		result := decodeMetaToolResult(t, envelope)
		var listed struct {
			Servers []struct {
				Slug   string `json:"slug"`
				Status string `json:"status"`
			} `json:"servers"`
		}
		require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
		statuses := map[string]string{}
		for _, server := range listed.Servers {
			statuses[server.Slug] = server.Status
		}
		return statuses
	}

	statuses := listStatuses()
	require.Equal(t, "unknown", statuses[proxiedSlug], "remote members stay unknown until cached health exists")
	require.Equal(t, "unavailable", statuses[tunneledSlug], "a tunneled member with no live route is unavailable")

	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), "http://tunnel-gateway.internal.example:8443", time.Hour))
	statuses = listStatuses()
	require.Equal(t, "available", statuses[tunneledSlug], "a published route flips the tunneled member to available")
	require.Equal(t, "unknown", statuses[proxiedSlug], "route publication must not disturb the remote member's status")
}

// A proxied member is drill-down navigable end to end: describe_server and
// describe_tools read its live tools/list, and a dead member degrades
// member-scoped.
func TestServePublic_MetaEndpoint_DrillDown_ProxiedMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	proxiedSlug := "member-remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", proxiedSlug, 1, upstream.url)

	envelope := callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": proxiedSlug})
	require.Contains(t, string(envelope["result"]), proxiedSlug+"--ping",
		"describe_server must qualify the proxied member's live tool names")

	envelope = callMetaTool(t, ctx, ti, slug, "describe_tools", map[string]any{"tools": []string{proxiedSlug + "--ping"}})
	require.Contains(t, string(envelope["result"]), `"inputSchema"`,
		"describe_tools must return the proxied tool's schema")
	require.NotContains(t, string(envelope["result"]), `"failed"`,
		"a healthy member must not be reported failed")

	// An unreachable member degrades member-scoped in describe_tools rather
	// than failing the whole call.
	deadSlug := "member-dead-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "dead member", deadSlug, 2, "http://127.0.0.1:1/mcp")
	envelope = callMetaTool(t, ctx, ti, slug, "describe_tools", map[string]any{
		"tools": []string{proxiedSlug + "--ping", deadSlug + "--anything"},
	})
	body := string(envelope["result"])
	require.Contains(t, body, proxiedSlug+"--ping", "the healthy member must still be described")
	require.Contains(t, body, `"failed"`, "the dead member must land in failed")
	require.Contains(t, body, deadSlug, "failed must name the dead member")
}

// A membership row pointing at a slugless server (legacy pre-2026-05 rows;
// new attaches are validated) is excluded from the servable snapshot rather
// than erroring: the qualified-name contract cannot address it.
func TestServePublic_MetaEndpoint_ListServers_ExcludesSluglessMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	remote := remotemcptest.SeedServer(t, ctx, ti.conn, remotemcprepo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: "streamable-http",
		Url:           "https://member.example.com/mcp/" + uuid.NewString(),
	})
	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("slugless legacy"),
		Slug:                pgtype.Text{String: "", Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID), Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		Visibility:          mcpservers.VisibilityPublic,
	})
	require.NoError(t, err)
	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       *authCtx.ProjectID,
		MetaMcpServerID: meta.ID,
		McpServerID:     server.ID,
		SortOrder:       1,
	})
	require.NoError(t, err)

	envelope := callMetaTool(t, ctx, ti, slug, "list_servers", map[string]any{})
	result := decodeMetaToolResult(t, envelope)
	var listed struct {
		Servers []any `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Empty(t, listed.Servers)
}

// A notification declaring an unsupported revision is refused with an HTTP
// error status, as every revision requires for a notification the server
// cannot accept, under a null id because it answers no request.
func TestServePublic_MetaEndpoint_NotificationWithUnsupportedVersionIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	require.NoError(t, err)

	w, err := servePublicHTTP(t, ctx, ti, slug, body, "", map[string]string{
		mcpversions.HTTPHeader: unservedProtocolVersion,
	})
	require.NoError(t, err)
	requireRefusedNotification(t, w, unservedProtocolVersion, mcpversions.SupportedMetaServer())
}

// Challenge-resumption arms of the meta endpoint resolver, driven through
// the public surface that invokes it: HandleIDPCallback resuming a cached
// challenge. Resolution runs before the IDP code exchange, so the fail-closed
// arms need no identity mock.
func TestHandleIDPCallback_MetaEndpointResumption(t *testing.T) {
	t.Parallel()

	newChallenge := func(t *testing.T, ctx context.Context, ti *testInstance, slug string, issuerID uuid.UUID, metaID uuid.UUID) string {
		t.Helper()
		challengeID := uuid.NewString()
		require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
			ID:                  challengeID,
			UserSessionIssuerID: issuerID,
			Endpoint: mcp.EndpointRef{
				McpSlug:         slug,
				MetaMcpServerID: uuid.NullUUID{UUID: metaID, Valid: true},
			},
			ClientID:            "test-client",
			RedirectURI:         "http://localhost:3000/callback",
			State:               "client-state",
			CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
			CodeChallengeMethod: "S256",
			CSRFToken:           "csrf-token",
			CreatedAt:           time.Now(),
		}))
		return challengeID
	}
	driveCallback := func(t *testing.T, ti *testInstance, slug, challengeID string) (*httptest.ResponseRecorder, error) {
		t.Helper()
		q := url.Values{"state": {challengeID}, "code": {"idp-auth-code-123"}}
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/idp_callback?"+q.Encode(), nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", slug)
		req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		return w, ti.service.HandleIDPCallback(w, req)
	}

	t.Run("resumes onto the meta endpoint", func(t *testing.T) {
		t.Parallel()
		mock := &mockIdentityResolver{
			exchangeResult: &identity.IDPUserInfo{Sub: "idp-user-123", Email: "test@example.com", Name: "Test User"},
			upsertResult:   "user-" + uuid.NewString()[:8],
			hasAccessOK:    true,
		}
		ctx, ti := newTestMCPServiceWithIdentityResolver(t, mock)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
		slug := "meta-" + uuid.NewString()
		meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)

		challengeID := newChallenge(t, ctx, ti, slug, issuerID, meta.ID)
		w, err := driveCallback(t, ti, slug, challengeID)
		require.NoError(t, err)
		require.Equal(t, http.StatusFound, w.Code)
		require.Contains(t, w.Header().Get("Location"), "/connect", "resolution must succeed and hand off to consent")
	})

	t.Run("fails closed on re-pointed endpoint", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPService(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
		slug := "meta-" + uuid.NewString()
		createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)

		// The cached challenge claims a different meta server than the one
		// the endpoint currently resolves to.
		challengeID := newChallenge(t, ctx, ti, slug, issuerID, uuid.New())
		_, err := driveCallback(t, ti, slug, challengeID)
		require.ErrorContains(t, err, "authn challenge endpoint does not match",
			"a challenge must not resume onto a re-pointed endpoint")
	})

	t.Run("issuer detached closes in-flight challenges", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestMCPService(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
		slug := "meta-" + uuid.NewString()
		ungated := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

		challengeID := newChallenge(t, ctx, ti, slug, issuerID, ungated.ID)
		_, err := driveCallback(t, ti, slug, challengeID)
		require.ErrorContains(t, err, "not found")
	})
}

// Snapshot RBAC: a private-visibility member is invisible without
// mcp:connect on the member server and visible with it.
func TestServePublic_MetaEndpoint_ListServers_RBACFiltersPrivateMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	memberSlug := "member-private-" + uuid.NewString()[:8]
	serverID := seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "private member", memberSlug, 1, mcpservers.VisibilityPrivate)

	var listed struct {
		Servers []struct {
			Slug string `json:"slug"`
		} `json:"servers"`
	}

	deniedCtx := authztest.WithExactGrants(t, ctx)
	envelope := callMetaTool(t, deniedCtx, ti, slug, "list_servers", map[string]any{})
	require.NoError(t, json.Unmarshal(decodeMetaToolResult(t, envelope).StructuredContent, &listed))
	require.Empty(t, listed.Servers)

	unrelatedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, uuid.NewString()),
	})
	envelope = callMetaTool(t, unrelatedCtx, ti, slug, "list_servers", map[string]any{})
	require.NoError(t, json.Unmarshal(decodeMetaToolResult(t, envelope).StructuredContent, &listed))
	require.Empty(t, listed.Servers)

	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, serverID.String()),
	})
	envelope = callMetaTool(t, grantedCtx, ti, slug, "list_servers", map[string]any{})
	require.NoError(t, json.Unmarshal(decodeMetaToolResult(t, envelope).StructuredContent, &listed))
	require.Len(t, listed.Servers, 1)
	require.Equal(t, memberSlug, listed.Servers[0].Slug)
}

// A member whose server carries an unrecognized visibility value is filtered
// from the snapshot (fail closed), even for the owning org.
func TestServePublic_MetaEndpoint_ListServers_UnknownVisibilityFailsClosed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "odd member", "member-odd-"+uuid.NewString()[:8], 1, "experimental")

	envelope := callMetaTool(t, ctx, ti, slug, "list_servers", map[string]any{})
	result := decodeMetaToolResult(t, envelope)
	var listed struct {
		Servers []any `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Empty(t, listed.Servers)
}

// An anonymous caller on an ungated endpoint carries no AuthContext, which
// authz reports as Unauthorized: a denial that filters the private member,
// never a request-level error.
func TestServePublic_MetaEndpoint_Anonymous_PrivateServerMemberFiltered(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	openSlug := "member-open-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "open member", openSlug, 1, mcpservers.VisibilityPublic)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "closed member", "member-closed-"+uuid.NewString()[:8], 2, mcpservers.VisibilityPrivate)

	envelope := callMetaTool(t, context.Background(), ti, slug, "list_servers", map[string]any{})
	result := decodeMetaToolResult(t, envelope)
	var listed struct {
		Servers []struct {
			Slug string `json:"slug"`
		} `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Len(t, listed.Servers, 1)
	require.Equal(t, openSlug, listed.Servers[0].Slug)
}

// A session carrying a consent-screen tool selection cannot be spent on a
// meta endpoint: selections bind to "toolset:"/"mcp_server:" resources and a
// meta endpoint expects none, so the gate rejects into reauth instead of
// letting describe and dispatch drift on enforcement. This is what keeps
// gate.toolSelection provably nil on the meta path today.
func TestServePublic_MetaEndpoint_ToolSelectionSessionRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "meta-selection-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, authCtx.ActiveOrganizationID, slug, issuerID)
	seedMetaMember(t, ctx, ti.conn, projectID, meta.ID, "remote member", "member-remote-"+uuid.NewString()[:8], 1, mcpservers.VisibilityPublic)

	subject := createTestUser(t, ctx, ti, "selection-user-"+uuid.NewString())
	bearer, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)

	selection := fmt.Appendf(nil, `{"resource":"toolset:%s","grant_id":"%s","allow":[{"type":"tool","name":"alpha_tool"}]}`, uuid.New(), uuid.NewString())
	now := time.Now()
	_, err = usersessions_repo.New(ti.conn).CreateUserSession(context.Background(), usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuerID,
		UserSessionClientID: uuid.NullUUID{},
		SubjectUrn:          subject,
		Jti:                 jti,
		RefreshTokenHash:    conv.ToPGText("test-selection-" + uuid.NewString()),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ToolSelection:       selection,
		SignInID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}), bearer, nil)
	require.ErrorContains(t, err, "invalid access token",
		"a selection-bound session must reject into reauth, not serve unfiltered")
}

// TestServePublic_MetaEndpoint_DescribeTools_SharesOneShadowAdmission: a
// describe_tools request spanning several members admits its caller for the
// mandatory authz shadow once, failure included, rather than once per member
// check, each with its own timeout.
func TestServePublic_MetaEndpoint_DescribeTools_SharesOneShadowAdmission(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.features.SetFlag(feature.FlagMandatoryAuthzShadow, authCtx.ActiveOrganizationID, true)

	var admissions atomic.Int32
	ti.service.SetPrepareMandatory(func(ctx context.Context) (authz.MandatoryAuthorization, error) {
		admissions.Add(1)
		// A slow admission spends its whole budget and fails.
		<-ctx.Done()
		return authz.MandatoryAuthorization{}, ctx.Err()
	})

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	names := []string{}
	for i := range 3 {
		tool := fmt.Sprintf("tool_%d", i)
		member := seedToolsMetaMember(t, ctx, ti, meta.ID, fmt.Sprintf("remote member %d", i), int32(i+1), tool)
		names = append(names, member+"--"+tool)
	}

	envelope := callMetaTool(t, ctx, ti, slug, "describe_tools", map[string]any{"tools": names})
	result := decodeMetaToolResult(t, envelope)
	require.False(t, result.IsError, "a failed shadow admission never changes the response")
	require.Contains(t, string(result.StructuredContent), names[2])
	require.Equal(t, int32(1), admissions.Load(), "one shadow admission per request")
}

// A gateway membership written before gateways refused hosted servers is never
// served: list_servers omits it, and naming it answers with a tool error that
// says why, while the gateway's other members keep working.
func TestServePublic_MetaEndpoint_LegacyHostedMemberIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	remote := seedToolsMetaMember(t, ctx, ti, meta.ID, "remote member", 1, "alpha_tool")

	// Membership writes refuse hosted servers, so the legacy row is inserted
	// directly.
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "legacy-hosted-"+uuid.NewString()[:8])
	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	hostedSlug := "hosted-" + uuid.NewString()[:8]
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("hosted member"),
		Slug:                conv.ToPGText(hostedSlug),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility:          mcpservers.VisibilityPublic,
	})
	require.NoError(t, err)
	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       *authCtx.ProjectID,
		MetaMcpServerID: meta.ID,
		McpServerID:     serverID,
		SortOrder:       2,
	})
	require.NoError(t, err)

	const refusal = "hosted servers can't be used through a gateway"

	envelope := callMetaTool(t, ctx, ti, slug, "list_servers", map[string]any{})
	var listed struct {
		Servers []struct {
			Slug string `json:"slug"`
		} `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(decodeMetaToolResult(t, envelope).StructuredContent, &listed))
	require.Len(t, listed.Servers, 1)
	require.Equal(t, remote, listed.Servers[0].Slug)

	envelope = callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": hostedSlug})
	text, isError := metaToolResultText(t, envelope)
	require.True(t, isError)
	require.Contains(t, text, hostedSlug)
	require.Contains(t, text, refusal)

	envelope = callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      hostedSlug + "--anything",
		"arguments": map[string]any{},
	})
	text, isError = metaToolResultText(t, envelope)
	require.True(t, isError)
	require.Contains(t, text, refusal)

	envelope = callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      remote + "--alpha_tool",
		"arguments": map[string]any{},
	})
	text, isError = metaToolResultText(t, envelope)
	require.False(t, isError, "text=%s", text)
	require.Contains(t, text, "pong from alpha_tool")
}
