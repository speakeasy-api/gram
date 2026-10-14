package mcp_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
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
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Allowed remote", allowedSlug, 2, newRecordingUpstream(t, "ping").url)
	blockedSlug := "remote-" + uuid.NewString()[:8]
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Blocked remote", blockedSlug, 3, newRecordingUpstream(t, "pong").url)

	resource := "meta_mcp_server:" + gateway.ID.String()
	selection, err := toolfilter.NewSessionSelection(resource, uuid.New(), []toolfilter.AllowEntry{
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(hosted.slug, "alpha"), Mode: nil, Tools: nil},
		{Type: toolfilter.AllowTypeTool, Name: metamcp.QualifyName(allowedSlug, "ping"), Mode: nil, Tools: nil},
	})
	require.NoError(t, err)

	mint := func(mode metamcp.DiscoveryMode) string {
		policy, err := json.Marshal(&toolfilter.SessionPolicy{Resource: resource, Selection: selection, Gateway: &toolfilter.GatewayOptions{DiscoveryMode: &mode}})
		require.NoError(t, err)
		subject := urn.NewUserSubject(authCtx.UserID)
		token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(), Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour})
		require.NoError(t, err)
		_, err = usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuerID, SubjectUrn: subject, Jti: jti, RefreshTokenHash: conv.ToPGText("gateway-selection-test-" + uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ToolSelection: policy})
		require.NoError(t, err)
		return token
	}
	call := func(token, name string, arguments map[string]any) string {
		w, err := servePublicHTTP(t, t.Context(), ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": name, "arguments": arguments}), token, nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		return w.Body.String()
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
