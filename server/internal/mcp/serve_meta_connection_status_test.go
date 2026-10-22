// The gateway's connection_status tool and the MCP Apps view it renders with:
// per-member sign-in state from the issuer gate's partially resolved tokens,
// the connect page link, the text fallback, and the ui:// resource.
package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

type connectionStatusResult struct {
	text       string
	connectURL string
	statuses   map[string]string
}

func callConnectionStatus(t *testing.T, ti *testInstance, ctx context.Context, metaSlug, bearer string) connectionStatusResult {
	t.Helper()

	body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": metamcp.ToolConnectionStatus, "arguments": map[string]any{}})
	w, err := servePublicHTTP(t, ctx, ti, metaSlug, body, bearer, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	rpc := decodeRPCResponse(t, w)

	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "connection_status must not fail: %s", text)

	var structured struct {
		StructuredContent metamcp.ConnectionStatusResult `json:"structuredContent"`
	}
	require.NoError(t, json.Unmarshal(rpc["result"], &structured))
	statuses := map[string]string{}
	for _, server := range structured.StructuredContent.Servers {
		statuses[server.Slug] = server.Status
	}
	return connectionStatusResult{text: text, connectURL: structured.StructuredContent.ConnectURL, statuses: statuses}
}

// A member bound to no sign-in provider needs no sign-in, and an ungated
// gateway has no connect page to link to.
func TestServePublic_MetaEndpoint_ConnectionStatus_UngatedMembersNeedNoSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	hosted := seedHostedMetaMember(t, ctx, ti, meta.ID, "hosted member", 1, mcpservers.VisibilityPublic, "alpha_tool")
	remoteSlug := "member-remote-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "remote member", remoteSlug, 2, mcpservers.VisibilityPublic)

	got := callConnectionStatus(t, ti, ctx, slug, "")
	require.Equal(t, metamcp.SignInNotRequired, got.statuses[hosted.slug])
	require.Equal(t, metamcp.SignInNotRequired, got.statuses[remoteSlug])
	require.Empty(t, got.connectURL, "an ungated gateway has no connect page")
	require.Contains(t, got.text, "2 of 2 servers ready")
}

// On an issuer-gated gateway the tool answers while members are unconnected:
// the member holding a routed credential reads connected, the one without
// reads not connected, and both the structured result and the text fallback
// carry the gateway's connect page.
func TestServePublic_MetaEndpoint_ConnectionStatus_GatedReportsPerMemberSignIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-status-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	_, connectedIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Connected member", "member-connected", 0, "")
	_, unconnectedIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Unconnected member", "member-unconnected", 1, "")

	connectedClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "status-connected", "", []uuid.UUID{sharedIssuerID, connectedIssuerID})
	createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "status-unconnected", "", []uuid.UUID{sharedIssuerID, unconnectedIssuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{connectedIssuerID, unconnectedIssuerID}))

	subject := createTestUser(t, ctx, ti, "status-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, connectedClient, subject, "token-connected", "")
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	got := callConnectionStatus(t, ti, context.Background(), metaSlug, bearer)
	require.Equal(t, metamcp.SignInConnected, got.statuses["member-connected"])
	require.Equal(t, metamcp.SignInNotConnected, got.statuses["member-unconnected"])

	wantSuffix := "/mcp/" + metaSlug + "/connect/first-party"
	require.True(t, strings.HasSuffix(got.connectURL, wantSuffix), "connectUrl %q must be the gateway's connect page", got.connectURL)
	require.Contains(t, got.text, "1 of 2 servers ready")
	require.Contains(t, got.text, "Unconnected member: not signed in")
	require.Contains(t, got.text, got.connectURL, "the text fallback must carry the same connect link")
}

// A remote member bound to no sign-in provider still receives a grant routed
// by its upstream resource, so it reads connected rather than not required.
func TestServePublic_MetaEndpoint_ConnectionStatus_RemoteMemberRoutedWithoutIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-status-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	routedURL := "https://routed.example.com/mcp/" + uuid.NewString()
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member-routed", routedURL, uuid.NullUUID{}, 0)
	createMetaMember(t, ctx, ti.conn, projectID, meta.ID, "member-open", "https://open.example.com/mcp/"+uuid.NewString(), uuid.NullUUID{}, 1)

	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "status-routed", "", []uuid.UUID{sharedIssuerID})
	subject := createTestUser(t, ctx, ti, "status-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, clientID, subject, "token-routed", routedURL)
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	got := callConnectionStatus(t, ti, context.Background(), metaSlug, bearer)
	require.Equal(t, metamcp.SignInConnected, got.statuses["member-routed"])
	require.Equal(t, metamcp.SignInNotRequired, got.statuses["member-open"])
}

// tools/list links connection_status to its view, and resources/list and
// resources/read serve that view as an MCP Apps resource.
func TestServePublic_MetaEndpoint_ConnectionStatus_ServesMCPAppView(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	slug := "meta-" + uuid.NewString()
	createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)

	post := func(method string, params map[string]any) json.RawMessage {
		w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, method, params), "", nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		envelope := decodeRPCResponse(t, w)
		require.NotContains(t, envelope, "error", "%s failed: %s", method, string(envelope["error"]))
		return envelope["result"]
	}

	var tools struct {
		Tools []struct {
			Name string `json:"name"`
			Meta struct {
				UI struct {
					ResourceURI string `json:"resourceUri"`
				} `json:"ui"`
			} `json:"_meta"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(post("tools/list", map[string]any{}), &tools))
	var viewURI string
	for _, tool := range tools.Tools {
		if tool.Name == metamcp.ToolConnectionStatus {
			viewURI = tool.Meta.UI.ResourceURI
		}
	}
	require.True(t, strings.HasPrefix(viewURI, "ui://"), "connection_status must name its view, got %q", viewURI)

	var listed struct {
		Resources []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(post("resources/list", map[string]any{}), &listed))
	require.Len(t, listed.Resources, 1)
	require.Equal(t, viewURI, listed.Resources[0].URI)
	require.Equal(t, metamcp.MCPAppMimeType, listed.Resources[0].MimeType)

	var read struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"contents"`
	}
	require.NoError(t, json.Unmarshal(post("resources/read", map[string]any{"uri": viewURI}), &read))
	require.Len(t, read.Contents, 1)
	require.Equal(t, metamcp.MCPAppMimeType, read.Contents[0].MimeType)
	require.Contains(t, read.Contents[0].Text, "ui/open-link")

	w, err := servePublicHTTP(t, ctx, ti, slug, makeMetaRPCBody(t, "resources/read", map[string]any{"uri": "ui://gateway/unknown"}), "", nil)
	require.NoError(t, err)
	require.Contains(t, decodeRPCResponse(t, w), "error", "an unknown resource must not be served")
}
