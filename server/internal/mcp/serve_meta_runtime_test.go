// serve_meta_runtime_test.go verifies the meta MCP drill-down runtime for
// hosted members: describe_server/describe_tools/execute_tool routing,
// qualified-name handling, member exclusion rules, proxied-member
// not-implemented answers, the notification/version interaction, and the
// challenge-resumption resolver.
package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	variations_repo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type hostedMemberFixture struct {
	slug      string
	serverID  uuid.UUID
	toolsetID uuid.UUID
	toolURNs  map[string]urn.Tool
}

// seedHostedMetaMember: public-MCP toolset with the named HTTP tools, fronted
// by a toolset-backed mcp_server attached to the meta server.
func seedHostedMetaMember(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	metaID uuid.UUID,
	name string,
	sortOrder int32,
	visibility string,
	toolNames ...string,
) hostedMemberFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolsetSlug := "member-ts-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, toolsetSlug)
	toolURNs := addHTTPTools(t, ctx, ti, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID, toolNames...)

	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	memberSlug := "hosted-" + uuid.NewString()[:8]
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText(name),
		Slug:                conv.ToPGText(memberSlug),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility:          visibility,
	})
	require.NoError(t, err)

	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       *authCtx.ProjectID,
		MetaMcpServerID: metaID,
		McpServerID:     server.ID,
		SortOrder:       sortOrder,
	})
	require.NoError(t, err)

	return hostedMemberFixture{slug: memberSlug, serverID: server.ID, toolsetID: toolset.ID, toolURNs: toolURNs}
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

func TestServePublic_MetaEndpoint_DescribeServer_HostedMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool", "beta_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": member.slug})
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
	require.Equal(t, member.slug, described.Server.Slug)
	require.Equal(t, "available", described.Server.Status)

	names := make([]string, 0, len(described.Tools))
	for _, tool := range described.Tools {
		var name string
		require.NoError(t, json.Unmarshal(tool["name"], &name))
		names = append(names, name)
		// Catalog listing deliberately carries no schemas.
		require.NotContains(t, tool, "inputSchema")
	}
	require.ElementsMatch(t, []string{member.slug + "--alpha_tool", member.slug + "--beta_tool"}, names)
}

func TestServePublic_MetaEndpoint_DescribeTools_HostedMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "describe_tools", map[string]any{
		"tools": []string{member.slug + "--alpha_tool", member.slug + "--missing_tool"},
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
	require.Equal(t, member.slug+"--alpha_tool", described.Tools[0].Name)
	require.NotEmpty(t, described.Tools[0].InputSchema, "describe_tools must carry the full input schema")
	require.Equal(t, []string{member.slug + "--missing_tool"}, described.NotFound)
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

func TestServePublic_MetaEndpoint_ExecuteTool_HostedMember_Dispatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	// The fixture tool has no configured upstream URL, so a fully routed call
	// reaches the member dispatch, resolves the tool, attempts execution, and
	// fails there — surfacing as an internal error. Routing failures look
	// different (not-found codes, unknown server / tool not found messages),
	// so this pins that the meta MCP handed the call to the member's tool
	// execution path without pinning dispatch-internal message text. The
	// outer _meta rides along to prove forwarding does not disturb routing.
	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name": "execute_tool",
		"arguments": map[string]any{
			"name":      member.slug + "--alpha_tool",
			"arguments": map[string]any{},
		},
		"_meta": map[string]any{
			"io.modelcontextprotocol/clientInfo": map[string]any{"name": "meta-test-client", "version": "1.2.3"},
		},
	}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	envelope := decodeRPCResponse(t, w)

	var rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(envelope["error"], &rpcErr))
	// The fixture tool has no server URL, which real execution rejects as
	// invalid; what matters is that dispatch reached execution at all.
	require.Equal(t, int(oops.MCPCodeInvalidParams), rpcErr.Code)
	require.NotContains(t, rpcErr.Message, "unknown server")
	require.NotContains(t, rpcErr.Message, "tool not found")
}

func TestServePublic_MetaEndpoint_ExecuteTool_UnknownToolOnKnownMember(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      member.slug + "--no_such_tool",
		"arguments": map[string]any{},
	})
	require.Contains(t, string(envelope["error"]), "tool not found")
}

// Discovery and execution agree on an ambiguous name: a variation renaming one
// tool onto another's name drops it from the member catalog, and execute_tool
// refuses it rather than dispatching an arbitrary one of the pair.
func TestServePublic_MetaEndpoint_AmbiguousToolName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool", "beta_tool")

	variationsRepo := variations_repo.New(ti.conn)
	group, err := variationsRepo.InitGlobalToolVariationsGroup(ctx, variations_repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   *authCtx.ProjectID,
		Name:        "default-group",
		Description: conv.ToPGText("default group"),
	})
	require.NoError(t, err)

	_, err = variationsRepo.UpsertToolVariation(ctx, variations_repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  member.toolURNs["alpha_tool"],
		SrcToolName: "alpha_tool",
		Name:        conv.ToPGText("beta_tool"),
	})
	require.NoError(t, err)

	envelope := callMetaTool(t, ctx, ti, slug, "describe_server", map[string]any{"server": member.slug})
	var described struct {
		Tools []any `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(decodeMetaToolResult(t, envelope).StructuredContent, &described))
	require.Empty(t, described.Tools)

	envelope = callMetaTool(t, ctx, ti, slug, "execute_tool", map[string]any{
		"name":      member.slug + "--beta_tool",
		"arguments": map[string]any{},
	})
	require.Contains(t, string(envelope["error"]), "ambiguous tool name")
}

func TestServePublic_MetaEndpoint_ListServers_StatusByBackend(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	hostedMember := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")
	proxiedSlug := "member-remote-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", proxiedSlug, 2, mcpservers.VisibilityPrivate)
	tunneledSlug := "member-tunnel-" + uuid.NewString()[:8]
	tunnelID := seedTunneledMetaMember(t, ctx, ti, *authCtx.ProjectID, meta.ID, "tunneled member", tunneledSlug, 3)

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
	require.Equal(t, "available", statuses[hostedMember.slug], "hosted members execute in-process and are always available")
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

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, "ts-slugless-"+uuid.NewString()[:8])
	serverID, err := uuid.NewV7()
	require.NoError(t, err)
	server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:         serverID,
		ProjectID:  *authCtx.ProjectID,
		Name:       conv.ToPGText("slugless legacy"),
		Slug:       pgtype.Text{String: "", Valid: false},
		ToolsetID:  uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility: mcpservers.VisibilityPublic,
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

// A notification carrying a bad protocol-version declaration is dropped, not
// answered: JSON-RPC 2.0 forbids responding to notifications, even with an
// error.
func TestServePublic_MetaEndpoint_NotificationWithBadVersionIsDropped(t *testing.T) {
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
		mcpversions.HTTPHeader: "2031-01-01",
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, w.Code, "body=%s", w.Body.String())
	require.Empty(t, w.Body.String())
}

// Challenge-resumption resolver (buildResolvedMetaMcpEndpointByRef) arms.
func TestBuildResolvedMetaMcpEndpointByRef(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)

	t.Run("resolves with denormalized org", func(t *testing.T) {
		t.Parallel()
		endpoint, err := ti.service.BuildResolvedMcpEndpointByRefForTest(ctx, mcp.EndpointRef{
			McpSlug:         slug,
			MetaMcpServerID: uuid.NullUUID{UUID: meta.ID, Valid: true},
		})
		require.NoError(t, err)
		require.Equal(t, authCtx.ActiveOrganizationID, endpoint.OrganizationID, "org id must come from the denormalized meta row")
		require.Equal(t, issuerID, endpoint.UserSessionIssuerID)
		require.Equal(t, "mcp", endpoint.RouteBase, "empty ref RouteBase defaults to mcp")
	})

	t.Run("fails closed on re-pointed endpoint", func(t *testing.T) {
		t.Parallel()
		_, err := ti.service.BuildResolvedMcpEndpointByRefForTest(ctx, mcp.EndpointRef{
			McpSlug:         slug,
			MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		})
		require.ErrorIs(t, err, mcp.ErrToolsetEndpointMismatchForTest)
	})

	t.Run("issuer detached closes in-flight challenges", func(t *testing.T) {
		t.Parallel()
		ungatedSlug := "meta-" + uuid.NewString()
		ungated := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, ungatedSlug, uuid.Nil)
		_, err := ti.service.BuildResolvedMcpEndpointByRefForTest(ctx, mcp.EndpointRef{
			McpSlug:         ungatedSlug,
			MetaMcpServerID: uuid.NullUUID{UUID: ungated.ID, Valid: true},
		})
		require.ErrorContains(t, err, "not found")
	})
}

func setToolsetMcpPrivate(t *testing.T, ctx context.Context, ti *testInstance, toolsetID uuid.UUID, projectID uuid.UUID) {
	t.Helper()
	repo := toolsets_repo.New(ti.conn)
	toolset, err := repo.GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	_, err = repo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
		Name:                   toolset.Name,
		Description:            toolset.Description,
		DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
		McpSlug:                toolset.McpSlug,
		McpIsPublic:            false,
		McpEnabled:             toolset.McpEnabled,
		Slug:                   toolset.Slug,
		ProjectID:              toolset.ProjectID,
	})
	require.NoError(t, err)
}

// Snapshot RBAC: a private-visibility member is invisible without
// mcp:connect and visible with it.
func TestServePublic_MetaEndpoint_ListServers_RBACFiltersPrivateMembers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "private member", 1, mcpservers.VisibilityPrivate, "alpha_tool")

	deniedCtx := authztest.WithExactGrants(t, ctx)
	envelope := callMetaTool(t, deniedCtx, ti, slug, "list_servers", map[string]any{})
	result := decodeMetaToolResult(t, envelope)
	var listed struct {
		Servers []struct {
			Slug string `json:"slug"`
		} `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Empty(t, listed.Servers)

	// A grant on the mcp_servers id must NOT admit a toolset-backed member:
	// the dashboard keys these grants on the toolset id, so accepting the
	// server id here would let the check pass on an id nothing writes.
	serverIDCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, member.serverID.String()),
	})
	envelope = callMetaTool(t, serverIDCtx, ti, slug, "list_servers", map[string]any{})
	result = decodeMetaToolResult(t, envelope)
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Empty(t, listed.Servers)

	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, member.toolsetID.String()),
	})
	envelope = callMetaTool(t, grantedCtx, ti, slug, "list_servers", map[string]any{})
	result = decodeMetaToolResult(t, envelope)
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Len(t, listed.Servers, 1)
	require.Equal(t, member.slug, listed.Servers[0].Slug)
}

// Describe parity with the direct surface's per-tool RBAC filter: an
// authenticated caller whose tool-scoped connect grant passes the
// toolset-level gate but names a different tool gets an empty catalog, not
// the schemas the member endpoint would hide.
func TestServePublic_MetaEndpoint_DescribeServer_FiltersRBACHiddenTools(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	setToolsetMcpPrivate(t, ctx, ti, member.toolsetID, *authCtx.ProjectID)

	otherToolSelector := authz.NewSelector(authz.ScopeMCPConnect, member.toolsetID.String())
	otherToolSelector[authz.SelectorKeyTool] = "some_other_tool"
	partialCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: otherToolSelector,
	})
	envelope := callMetaTool(t, partialCtx, ti, slug, "describe_server", map[string]any{"server": member.slug})
	result := decodeMetaToolResult(t, envelope)
	var described struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &described))
	require.Empty(t, described.Tools)

	// Positive control: a grant naming the tool admits it, so the filter is
	// consulting grants rather than hiding everything.
	grantedSelector := authz.NewSelector(authz.ScopeMCPConnect, member.toolsetID.String())
	grantedSelector[authz.SelectorKeyTool] = "alpha_tool"
	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: grantedSelector,
	})
	envelope = callMetaTool(t, grantedCtx, ti, slug, "describe_server", map[string]any{"server": member.slug})
	result = decodeMetaToolResult(t, envelope)
	require.NoError(t, json.Unmarshal(result.StructuredContent, &described))
	require.Len(t, described.Tools, 1)
	require.Equal(t, member.slug+"--alpha_tool", described.Tools[0].Name)
}

// Toolset-level gate parity with ServeToolsetResolved's connection check: an
// authenticated caller whose grants never name the member's toolset — none
// at all, or only a per-tool grant on an unrelated toolset — reads the
// private-toolset member as nonexistent on describe and execute alike.
func TestServePublic_MetaEndpoint_PrivateToolset_NoConnectGrant_ReadsUnknown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	setToolsetMcpPrivate(t, ctx, ti, member.toolsetID, *authCtx.ProjectID)

	unrelatedSelector := authz.NewSelector(authz.ScopeMCPConnect, uuid.NewString())
	unrelatedSelector[authz.SelectorKeyTool] = "alpha_tool"
	for name, deniedCtx := range map[string]context.Context{
		"no grants":       authztest.WithExactGrants(t, ctx),
		"unrelated grant": authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeMCPConnect, Selector: unrelatedSelector}),
	} {
		envelope := callMetaTool(t, deniedCtx, ti, slug, "describe_server", map[string]any{"server": member.slug})
		require.Contains(t, string(envelope["error"]), "unknown server", "describe_server with %s", name)

		envelope = callMetaTool(t, deniedCtx, ti, slug, "execute_tool", map[string]any{
			"name":      member.slug + "--alpha_tool",
			"arguments": map[string]any{},
		})
		require.Contains(t, string(envelope["error"]), "unknown server", "execute_tool with %s", name)

		// Deliberate disclosure: the member's server row is public, and
		// list_servers gates on server visibility alone, so the slug still
		// lists; the private toolset reads as nonexistent only on drill-down.
		envelope = callMetaTool(t, deniedCtx, ti, slug, "list_servers", map[string]any{})
		require.Contains(t, string(envelope["result"]), member.slug, "list_servers with %s", name)
	}

	// Positive control: a toolset-level grant opens both describe and
	// execute, so the gate consults grants rather than refusing everyone.
	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPConnect,
		Selector: authz.NewSelector(authz.ScopeMCPConnect, member.toolsetID.String()),
	})
	envelope := callMetaTool(t, grantedCtx, ti, slug, "describe_server", map[string]any{"server": member.slug})
	require.NotContains(t, string(envelope["error"]), "unknown server")
	envelope = callMetaTool(t, grantedCtx, ti, slug, "execute_tool", map[string]any{
		"name":      member.slug + "--alpha_tool",
		"arguments": map[string]any{},
	})
	require.NotContains(t, string(envelope["error"]), "unknown server",
		"a granted subject must reach execution")
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
	seedHostedMetaMember(t, ctx, ti, meta.ID, "odd member", 1, "experimental", "alpha_tool")

	envelope := callMetaTool(t, ctx, ti, slug, "list_servers", map[string]any{})
	result := decodeMetaToolResult(t, envelope)
	var listed struct {
		Servers []any `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Empty(t, listed.Servers)
}

// Ungated meta endpoints serve anonymously: without the issuer gate no
// identity exists, so a private-toolset member is listed (server visibility
// is public) but its drill-down reads as nonexistent.
func TestServePublic_MetaEndpoint_Anonymous_PrivateToolsetMemberHidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	setToolsetMcpPrivate(t, ctx, ti, member.toolsetID, *authCtx.ProjectID)

	anonCtx := context.Background()
	envelope := callMetaTool(t, anonCtx, ti, slug, "list_servers", map[string]any{})
	result := decodeMetaToolResult(t, envelope)
	var listed struct {
		Servers []struct {
			Slug string `json:"slug"`
		} `json:"servers"`
	}
	require.NoError(t, json.Unmarshal(result.StructuredContent, &listed))
	require.Len(t, listed.Servers, 1)

	envelope = callMetaTool(t, anonCtx, ti, slug, "describe_server", map[string]any{"server": member.slug})
	require.Contains(t, string(envelope["error"]), "unknown server")
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
	seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	subject := urn.NewUserSubject("selection-user-" + uuid.NewString())
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
		RefreshTokenHash:    "test-selection-" + uuid.NewString(),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
		ToolSelection:       selection,
	})
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}), bearer, nil)
	require.ErrorContains(t, err, "invalid access token",
		"a selection-bound session must reject into reauth, not serve unfiltered")
}

// A meta session holding several member credentials — the normal state once
// per-member consent qualifies one credential per member — must not break
// hosted execution: the hosted member simply runs without a remote-session
// token instead of failing the whole call.
func TestServePublic_MetaEndpoint_ExecuteTool_HostedMember_MultiCredentialSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	slug := "meta-multicred-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, slug, sharedIssuerID)
	hosted := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")

	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "multicred-a", "", []uuid.UUID{sharedIssuerID})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "multicred-b", "", []uuid.UUID{sharedIssuerID})
	subject := urn.NewUserSubject("multicred-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, sharedIssuerID, clientA, subject, "token-a", time.Now().Add(time.Hour))
	insertRemoteSessionAccessToken(t, ctx, ti, sharedIssuerID, clientB, subject, "token-b", time.Now().Add(time.Hour))

	bearer, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(sharedIssuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, sharedIssuerID, subject, jti)

	w, err := servePublicHTTP(t, context.Background(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "execute_tool",
		"arguments": map[string]any{"name": hosted.slug + "--alpha_tool", "arguments": map[string]any{}},
	}), bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	envelope := decodeRPCResponse(t, w)

	var rpcErr struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(envelope["error"], &rpcErr))
	// The fixture tool has no server URL, so execution still fails as invalid
	// params — the point is that the multi-credential map no longer aborts
	// dispatch before execution.
	require.Equal(t, int(oops.MCPCodeInvalidParams), rpcErr.Code)
	require.NotContains(t, rpcErr.Message, "remote-session upstream tokens")
}
