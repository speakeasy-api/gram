package platformmcp

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// Platform MCP request-access links name the extra platform host the request
// arrived on, because session cookies are host-only. Everything else keeps the
// configured dashboard URL, including local dev where it differs from the
// server URL.
func TestRequestAccessLinksFollowPlatformHost(t *testing.T) {
	t.Parallel()

	serverURL := mustTestURL(t, "https://app.example.test")
	tests := []struct {
		name         string
		dashboardURL string
		serverURL    *url.URL
		origin       *requestorigin.Origin
		wantBase     string
	}{
		{name: "extra platform host", dashboardURL: "https://app.example.test", serverURL: serverURL, origin: &requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://ai.example.test", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil}, wantBase: "https://ai.example.test"},
		{name: "canonical host", dashboardURL: "https://app.example.test", serverURL: serverURL, origin: &requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://app.example.test", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil}, wantBase: "https://app.example.test"},
		{name: "custom domain", dashboardURL: "https://app.example.test", serverURL: serverURL, origin: &requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://mcp.customer.example", OrganizationID: "org", NetworkIngressID: uuid.Nil, NetworkIdentity: nil}, wantBase: "https://app.example.test"},
		{name: "no origin", dashboardURL: "https://app.example.test", serverURL: serverURL, origin: nil, wantBase: "https://app.example.test"},
		{name: "local dev dashboard port", dashboardURL: "https://localhost:5173", serverURL: mustTestURL(t, "https://localhost:8080"), origin: &requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://localhost:8080", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil}, wantBase: "https://localhost:5173"},
		{name: "no server URL configured", dashboardURL: "https://app.example.test", serverURL: nil, origin: &requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://ai.example.test", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil}, wantBase: "https://app.example.test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-1", OrganizationSlug: "acme"})
			if tc.origin != nil {
				ctx = requestorigin.WithContext(ctx, *tc.origin)
			}
			dashboardURL := mustTestURL(t, tc.dashboardURL)

			authorizer := (&LiveOrgAdminAuthorizer{}).WithDashboardURL(dashboardURL).WithServerURL(tc.serverURL)
			adminLink := parseLink(t, authorizer.requestAccessURL(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceID: "org-1"}))
			require.Equal(t, tc.wantBase+"/acme/request-access", adminLink)

			plugins := &PluginsService{dashboardURL: dashboardURL, serverURL: tc.serverURL}
			mcpLink := parseLink(t, plugins.requestMCPAccessURL(ctx, "org-1", "mcp-1", "Server"))
			require.Equal(t, tc.wantBase+"/acme/request-access", mcpLink)
		})
	}
}

func mustTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func parseLink(t *testing.T, raw string) string {
	t.Helper()
	require.NotEmpty(t, raw)
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u.Scheme + "://" + u.Host + u.Path
}
