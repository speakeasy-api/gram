package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	externalmcp_types "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestGatewayToolSelectionRestrictsEveryMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	hosted := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Hosted tools", 1, mcpservers.VisibilityPublic, "alpha", "beta")
	allowedSlug := "remote-" + uuid.NewString()[:8]
	allowedID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Allowed remote", allowedSlug, 2, newRecordingUpstream(t, "ping").url)
	blockedSlug := "remote-" + uuid.NewString()[:8]
	blockedID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Blocked remote", blockedSlug, 3, newRecordingUpstream(t, "pong").url)
	members := map[string]uuid.UUID{hosted.slug: hosted.serverID, allowedSlug: allowedID, blockedSlug: blockedID}

	resource := "meta_mcp_server:" + gateway.ID.String()
	selection, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(hosted.slug, "alpha"), Mode: nil, Tools: nil},
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(allowedSlug, "ping"), Mode: nil, Tools: nil},
	})
	require.NoError(t, err)

	mint := func(mode metamcp.DiscoveryMode) string {
		return mintGatewaySelectionToken(t, ti, issuerID, slug, authCtx.UserID, &toolfilter.SessionPolicy{Resource: resource, Selection: selection, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}, Members: members})
	}
	call := func(token, name string, arguments map[string]any) string {
		return callGatewayTool(t, ti, slug, token, name, arguments)
	}

	direct := mint(metamcp.DiscoveryModeDirect)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), direct, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	listed := w.Body.String()
	require.Contains(t, listed, hosted.slug+"--alpha")
	require.Contains(t, listed, allowedSlug+"--ping")
	require.NotContains(t, listed, hosted.slug+"--beta")
	require.NotContains(t, listed, blockedSlug+"--pong")
	require.Contains(t, call(direct, allowedSlug+"--ping", map[string]any{}), "pong from ping")
	require.NotContains(t, call(direct, blockedSlug+"--pong", map[string]any{}), "pong from pong")
	require.NotContains(t, call(direct, hosted.slug+"--beta", map[string]any{}), `"result":{"content"`)

	progressive := mint(metamcp.DiscoveryModeProgressive)
	execute := func(name string) string {
		return call(progressive, "execute_tool", map[string]any{"name": name, "arguments": map[string]any{}})
	}
	require.Contains(t, execute(allowedSlug+"--ping"), "pong from ping")
	require.NotContains(t, execute(blockedSlug+"--pong"), "pong from pong")
	described := call(progressive, "describe_tools", map[string]any{"tools": []string{allowedSlug + "--ping", blockedSlug + "--pong", hosted.slug + "--beta"}})
	require.Contains(t, described, allowedSlug+"--ping")
	require.Contains(t, described, `"not_found":[`)
	require.NotContains(t, described, `\"name\":\"`+blockedSlug+`--pong\"`)
}

// TestGatewayToolSelectionRefusesReplacedMember pins the member binding: a
// grant approved for one server does not carry over to a different server
// later attached under the same slug.
func TestGatewayToolSelectionRefusesReplacedMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	memberSlug := "remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Replacement", memberSlug, 1, newRecordingUpstream(t, "ping").url)

	resource := "meta_mcp_server:" + gateway.ID.String()
	selection, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(memberSlug, "ping"), Mode: nil, Tools: nil},
	})
	require.NoError(t, err)
	direct := metamcp.DiscoveryModeDirect
	consentedServer := uuid.New()
	token := mintGatewaySelectionToken(t, ti, issuerID, slug, authCtx.UserID, &toolfilter.SessionPolicy{Resource: resource, Selection: selection, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &direct}, Members: map[string]uuid.UUID{memberSlug: consentedServer}})

	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), memberSlug+"--ping")
	require.NotContains(t, callGatewayTool(t, ti, slug, token, memberSlug+"--ping", map[string]any{}), "pong from ping")
}

func mintGatewaySelectionToken(t *testing.T, ti *testInstance, issuerID uuid.UUID, slug, userID string, policy *toolfilter.SessionPolicy) string {
	t.Helper()
	var raw []byte
	if policy != nil {
		var err error
		raw, err = json.Marshal(policy)
		require.NoError(t, err)
	}
	subject := urn.NewUserSubject(userID)
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSession(t.Context(), usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText("gateway-selection-test-" + uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: raw})
	require.NoError(t, err)
	return token
}

func callGatewayTool(t *testing.T, ti *testInstance, slug, token, name string, arguments map[string]any) string {
	t.Helper()
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": arguments}), token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	return w.Body.String()
}

// TestGatewayLiveAnnotationGrantCallsStatefulMember pins live grants on a
// member that opens a new MCP session per connection: the call session must
// carry its own listing witness, so a tool shown under the live grant stays
// callable in Direct and Progressive modes while others remain refused.
func TestGatewayLiveAnnotationGrantCallsStatefulMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, issuerID)
	upstream := newMockExternalMCPServer(t, externalmcp_types.TransportTypeStreamableHTTP, []testmcp.Tool{
		{Name: "read_docs", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object"}, Response: testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "read ok"}}}},
		{Name: "delete_docs", InputSchema: map[string]any{"type": "object"}, Response: testmcp.ToolResponse{Content: []map[string]any{{"type": "text", "text": "delete ok"}}}},
	})
	t.Cleanup(upstream.Close)
	memberSlug := "docs-" + uuid.NewString()[:8]
	memberID := seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Docs", memberSlug, 1, upstream.URL)

	resource := "meta_mcp_server:" + gateway.ID.String()
	live := toolfilter.AnnotationModeLive
	selection, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeAnnotation, Name: toolfilter.AnnotationReadOnly, Mode: &live, Tools: nil},
	})
	require.NoError(t, err)
	mint := func(mode metamcp.DiscoveryMode) string {
		return mintGatewaySelectionToken(t, ti, issuerID, slug, authCtx.UserID, &toolfilter.SessionPolicy{Resource: resource, Selection: selection, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}, Members: map[string]uuid.UUID{memberSlug: memberID}})
	}

	direct := mint(metamcp.DiscoveryModeDirect)
	w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), direct, nil)
	require.NoError(t, err)
	require.Contains(t, w.Body.String(), memberSlug+"--read_docs")
	require.NotContains(t, w.Body.String(), memberSlug+"--delete_docs")
	require.Contains(t, callGatewayTool(t, ti, slug, direct, memberSlug+"--read_docs", map[string]any{}), "read ok")
	refused := callGatewayTool(t, ti, slug, direct, memberSlug+"--delete_docs", map[string]any{})
	require.NotContains(t, refused, "delete ok")
	require.Contains(t, refused, remotemcp.ToolNotApprovedMessage)
	require.NotContains(t, refused, "rejected the call", "the gateway refused the call, not the upstream")

	progressive := mint(metamcp.DiscoveryModeProgressive)
	execute := func(name string) string {
		return callGatewayTool(t, ti, slug, progressive, "execute_tool", map[string]any{"name": name, "arguments": map[string]any{}})
	}
	require.Contains(t, execute(memberSlug+"--read_docs"), "read ok")
	require.NotContains(t, execute(memberSlug+"--delete_docs"), "delete ok")
}
