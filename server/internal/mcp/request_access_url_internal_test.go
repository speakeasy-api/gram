package mcp

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// The request-access link in an MCP 403 names the platform host the request
// arrived on, because session cookies are host-only. Canonical, custom-domain,
// private and origin-less requests keep the configured site URL.
func TestRequestAccessURLFollowsRequestPlatformHost(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	svc := &Service{serverURL: serverURL, siteURL: siteURL}

	tests := []struct {
		name     string
		origin   *requestorigin.Origin
		wantBase string
	}{
		{
			name:     "extra platform host",
			origin:   &requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://ai.example.test", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil},
			wantBase: "https://ai.example.test",
		},
		{
			name:     "canonical host",
			origin:   &requestorigin.Origin{Surface: requestorigin.SurfacePlatform, BaseURL: "https://app.example.test", OrganizationID: "", NetworkIngressID: uuid.Nil, NetworkIdentity: nil},
			wantBase: "https://app.example.test",
		},
		{
			name:     "custom domain",
			origin:   &requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: "https://mcp.customer.example", OrganizationID: "org", NetworkIngressID: uuid.Nil, NetworkIdentity: nil},
			wantBase: "https://app.example.test",
		},
		{
			name:     "private network",
			origin:   &requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example.test", OrganizationID: "org", NetworkIngressID: uuid.New(), NetworkIdentity: nil},
			wantBase: "https://app.example.test",
		},
		{
			name:     "no origin",
			origin:   nil,
			wantBase: "https://app.example.test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{OrganizationSlug: "acme"})
			if tc.origin != nil {
				ctx = requestorigin.WithContext(ctx, *tc.origin)
			}

			link, err := url.Parse(svc.requestAccessURL(ctx, "server-id", "Server"))
			require.NoError(t, err)
			require.Equal(t, tc.wantBase+"/acme/request-access", link.Scheme+"://"+link.Host+link.Path)
			require.Equal(t, "mcp:connect", link.Query().Get("scope"))
			require.Equal(t, "server-id", link.Query().Get("resource_id"))
		})
	}
}
