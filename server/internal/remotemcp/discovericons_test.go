package remotemcp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
)

func newInitializeServer(t *testing.T, contentType string, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverRemoteMcpIcons_JSONWithIcons(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"acme","icons":[{"src":"https://cdn.example.com/icon.png","mimeType":"image/png"},{"src":"/relative-icon.png"}]}}}`
	upstream := newInitializeServer(t, "application/json", http.StatusOK, body)

	icons := remotemcp.DiscoverRemoteMcpIcons(t.Context(), newPermissivePolicy(t), upstream.URL+"/mcp")

	require.Equal(t, []string{
		"https://cdn.example.com/icon.png",
		upstream.URL + "/relative-icon.png",
	}, icons)
}

func TestDiscoverRemoteMcpIcons_SSEWithIcons(t *testing.T) {
	t.Parallel()

	body := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"serverInfo\":{\"icons\":[{\"src\":\"https://cdn.example.com/sse-icon.png\"}]}}}\n\n"
	upstream := newInitializeServer(t, "text/event-stream", http.StatusOK, body)

	icons := remotemcp.DiscoverRemoteMcpIcons(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.Equal(t, []string{"https://cdn.example.com/sse-icon.png"}, icons)
}

func TestDiscoverRemoteMcpIcons_NoIcons(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"acme"}}}`
	upstream := newInitializeServer(t, "application/json", http.StatusOK, body)

	icons := remotemcp.DiscoverRemoteMcpIcons(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.Empty(t, icons)
}

func TestDiscoverRemoteMcpIcons_AuthRequired(t *testing.T) {
	t.Parallel()

	upstream := newInitializeServer(t, "application/json", http.StatusUnauthorized, `{}`)

	icons := remotemcp.DiscoverRemoteMcpIcons(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.Empty(t, icons)
}

func TestDiscoverRemoteMcpIcons_DropsUnsafeSchemes(t *testing.T) {
	t.Parallel()

	body := `{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"icons":[{"src":"data:image/png;base64,AAAA"},{"src":"javascript:alert(1)"},{"src":"https://cdn.example.com/ok.png"}]}}}`
	upstream := newInitializeServer(t, "application/json", http.StatusOK, body)

	icons := remotemcp.DiscoverRemoteMcpIcons(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.Equal(t, []string{"https://cdn.example.com/ok.png"}, icons)
}

func TestDiscoverRemoteMcpIcons_InvalidBody(t *testing.T) {
	t.Parallel()

	upstream := newInitializeServer(t, "application/json", http.StatusOK, "not json")

	icons := remotemcp.DiscoverRemoteMcpIcons(t.Context(), newPermissivePolicy(t), upstream.URL)

	require.Empty(t, icons)
}

func TestDiscoverServerIcons_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	_, err := ti.service.DiscoverServerIcons(ctx, &gen.DiscoverServerIconsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})

	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDiscoverServerIcons_InvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPWrite})

	_, err := ti.service.DiscoverServerIcons(ctx, &gen.DiscoverServerIconsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		URL:              "ftp://example.com",
		TransportType:    "streamable-http",
	})

	requireOopsCode(t, err, oops.CodeBadRequest)
}
