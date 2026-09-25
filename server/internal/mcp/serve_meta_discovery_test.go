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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
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
	member := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha")
	request := makeMetaRPCBody(t, "tools/list", map[string]any{})
	w, err := servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), `"name":"execute_tool"`)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), member.slug+"--alpha")
	require.NotContains(t, w.Body.String(), `"name":"execute_tool"`)
	require.Contains(t, w.Body.String(), `"inputSchema"`)
	setGatewayMode(t, ctx, ti, gateway.ID, "progressive")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, request, "", nil)
	require.NoError(t, err)
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
	subject := urn.NewUserSubject("gateway-mode-test-" + uuid.NewString())
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText("gateway-mode-test-" + uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
	require.NoError(t, err)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
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
	page.Result.NextCursor = ""
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Result.Tools, 1)
	require.Empty(t, page.Result.NextCursor)
	seedHostedMetaMember(t, ctx, ti, gateway.ID, "Added tools", 2, mcpservers.VisibilityPublic, "new_tool")
	w, err = servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{"cursor": cursor}), "", nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), "gateway inventory changed")
}
