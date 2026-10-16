package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func setGatewayMode(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID, mode string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := metamcprepo.New(ti.conn)
	server, err := queries.GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{ID: id, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	_, err = queries.UpdateMetaMCPServer(ctx, metamcprepo.UpdateMetaMCPServerParams{ID: id, ProjectID: server.ProjectID, OrganizationID: server.OrganizationID, Name: server.Name, UserSessionIssuerID: server.UserSessionIssuerID, DiscoveryMode: conv.ToPGText(mode)})
	require.NoError(t, err)
}

func TestGatewayDiscoveryDefaultChangesLive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	denied := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Private tools", 2, mcpservers.VisibilityPublic, "hidden")
	setToolsetMcpPrivate(t, ctx, ti, denied.toolsetID, *authCtx.ProjectID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	request := makeMetaRPCBody(t, "tools/list", map[string]any{})
	w, err := servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.NotContains(t, w.Body.String(), denied.slug+"--hidden")
	require.NotContains(t, w.Body.String(), `"name":"execute_tool"`)
	require.Contains(t, w.Body.String(), `"inputSchema"`)
	setGatewayMode(t, ctx, ti, gateway.ID, "progressive")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
}

func TestGatewayDirectRoutesRemoteTool(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newRecordingUpstream(t, "ping")
	memberSlug := "remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Remote tools", memberSlug, 1, upstream.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), memberSlug+"--ping")
	result := callMetaTool(t, t.Context(), ti, slug, memberSlug+"--ping", map[string]any{})
	require.NotContains(t, result, "error")
	require.Contains(t, result, "result")
	rejected := callMetaTool(t, t.Context(), ti, slug, "execute_tool", map[string]any{"name": memberSlug + "--ping"})
	require.Contains(t, rejected, "error")
}

func TestGatewayModeOnlySessionIsBoundAndUnrestricted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha", "beta")
	otherSlug := "gateway-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, otherSlug, issuerID)
	mode := metamcp.DiscoveryModeDirect
	policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: "meta_mcp_server:" + gateway.ID.String(), Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}})
	require.NoError(t, err)
	subject := urn.NewUserSubject(authCtx.UserID)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText("gateway-mode-test-" + uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.Contains(t, w.Body.String(), member.slug+"--beta")
	require.NotContains(t, w.Body.String(), `"name":"execute_tool"`)
	_, err = servePublicHTTP(t, t.Context(), ti, otherSlug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.ErrorContains(t, err, "invalid access token")
}

func TestGatewayDirectPaginationRejectsChangedInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	names := make([]string, 101)
	for i := range names {
		names[i] = fmt.Sprintf("tool_%03d", i)
	}
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Paged tools", 1, mcpservers.VisibilityPublic, names...)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	var page struct {
		Result struct {
			Tools      []json.RawMessage `json:"tools"`
			NextCursor string            `json:"nextCursor"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Result.Tools, 100)
	cursor := page.Result.NextCursor
	require.NotEmpty(t, cursor)
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{"cursor": cursor}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	page.Result.NextCursor = ""
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Result.Tools, 1)
	require.Empty(t, page.Result.NextCursor)
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Added tools", 2, mcpservers.VisibilityPublic, "new_tool")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{"cursor": cursor}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "gateway inventory changed")
}

func TestDirectRejectsMalformedCatalogWhileProgressiveRemainsUsable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	upstream := newListingUpstream(t, []any{map[string]any{"name": "valid", "inputSchema": map[string]any{"type": "object"}}, map[string]any{"description": "missing name", "inputSchema": map[string]any{"type": "object"}}}, nil)
	memberSlug := "catalog-member"
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Catalog member", memberSlug, 1, upstream)
	result := callMetaTool(t, t.Context(), ti, slug, "describe_tools", map[string]any{"tools": []string{memberSlug + "--valid"}})
	require.NotContains(t, result, "error")
	require.Contains(t, string(result["result"]), memberSlug+"--valid")
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "invalid definitions")
	require.NotContains(t, w.Body.String(), memberSlug+"--valid")
}

// newListingUpstream serves a minimal streamable-HTTP MCP server whose
// tools/list returns tools. beforeList, when set, runs first and can fail the
// listing by returning false.
func newListingUpstream(t *testing.T, tools []any, beforeList func() bool) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode upstream request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(request.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "catalog-test", "version": "1"}}
		case "tools/list":
			if beforeList != nil && !beforeList() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			result = map[string]any{"tools": tools}
		default:
			result = map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Errorf("encode upstream response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

func TestGatewayDirectListsReachableMembersWhenOneIsDown(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	downSlug := "down-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Down tools", downSlug, 2, downURL)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.NotContains(t, w.Body.String(), downSlug+"--")
	require.NotContains(t, w.Body.String(), `"error"`)
}

func TestGatewayDirectDescribesMembersConcurrently(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Each listing waits until both are in flight, so a sequential describe
	// times out the first member.
	var inFlight atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	barrier := func() bool {
		if inFlight.Add(1) >= 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
			return true
		case <-time.After(5 * time.Second):
			return false
		}
	}
	slugs := []string{"first-" + uuid.NewString()[:8], "second-" + uuid.NewString()[:8]}
	for i, memberSlug := range slugs {
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, barrier)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+memberSlug, memberSlug, int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), slugs[0]+"--ping")
	require.Contains(t, w.Body.String(), slugs[1]+"--ping")
}

func TestGatewayDirectBoundsTheWholeInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, testenv.NewMeterProvider(t), mcp.MetaRuntimeConfig{MemberCallTimeout: 500 * time.Millisecond, ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: 0})
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Five batches of members that never answer would take five call timeouts
	// one batch at a time; the inventory stops after its overall budget.
	hang := func() bool {
		<-time.After(3 * time.Second)
		return true
	}
	for i := range 25 {
		memberSlug := fmt.Sprintf("member%d-%s", i, uuid.NewString()[:8])
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, hang)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+memberSlug, memberSlug, int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	started := time.Now()
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Less(t, time.Since(started), 2*time.Second)
	// Members past the budget are skipped, not a failed listing.
	require.NotContains(t, w.Body.String(), `"error"`)
	require.Contains(t, w.Body.String(), `"tools":[]`)
}

func TestGatewayDirectGivesQueuedMembersTheirOwnDeadline(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, testenv.NewMeterProvider(t), mcp.MetaRuntimeConfig{MemberCallTimeout: 1500 * time.Millisecond, ValidationTimeout: 0, AutoVerifyWait: 0, RecheckInterval: 0})
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Seven members against a concurrency limit of six: the seventh starts
	// only after a slow member finishes, past half of one shared deadline.
	slow := func() bool {
		<-time.After(time.Second)
		return true
	}
	slugs := make([]string, 7)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("member%d-%s", i, uuid.NewString()[:8])
		upstream := newListingUpstream(t, []any{map[string]any{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}, slow)
		seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Member "+slugs[i], slugs[i], int32(i+1), upstream)
	}
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	for _, memberSlug := range slugs {
		require.Contains(t, w.Body.String(), memberSlug+"--ping")
	}
}
