package auth_test

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// callbackWithOrgHost seeds the two-organization user, stores defaultHost on
// the other-org organization (nil keeps it NULL), and runs a login callback
// for destination on a request from origin.
func callbackWithOrgHost(t *testing.T, defaultHost *string, origin requestorigin.Origin, destination string) *gen.CallbackResult {
	t.Helper()

	userInfo := speakeasyMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	for _, org := range userInfo.Organizations {
		require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
	}
	require.NoError(t, orgRepo.New(instance.conn).SetOrganizationDefaultHostForTest(ctx, orgRepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: conv.PtrToPGText(defaultHost),
		ID:          "other-org-123",
	}))

	ctx = requestorigin.WithContext(ctx, origin)
	ctx, stateParam := instance.stateWithNonce(ctx, t, destination)
	result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
		Code:  "mock_code",
		State: &stateParam,
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.SessionToken)
	return result
}

func originAt(surface requestorigin.Surface, baseURL string) requestorigin.Origin {
	return requestorigin.Origin{
		Surface:          surface,
		BaseURL:          baseURL,
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}
}

func extraHostLogin(redirect string) string {
	return "https://" + testExtraPlatformHost + "/rpc/auth.login?" + url.Values{"redirect": {redirect}}.Encode()
}

func TestService_Callback_OrganizationHost(t *testing.T) {
	t.Parallel()

	extraHost := "https://" + testExtraPlatformHost
	serverOrigin := originAt(requestorigin.SurfacePlatform, testServerURL.String())

	tests := []struct {
		name        string
		defaultHost *string
		origin      requestorigin.Origin
		destination string
		want        string
	}{
		{
			name:        "moves to the organization's platform host keeping the destination",
			defaultHost: new(extraHost),
			origin:      serverOrigin,
			destination: "http://localhost:3000/other-org/projects/default?tab=logs#recent",
			want:        extraHostLogin("/other-org/projects/default?tab=logs#recent"),
		},
		{
			name:        "keeps a relative destination",
			defaultHost: new(extraHost),
			origin:      serverOrigin,
			destination: "/other-org/mcp",
			want:        extraHostLogin("/other-org/mcp"),
		},
		{
			name:        "refuses a destination on another origin",
			defaultHost: new(extraHost),
			origin:      serverOrigin,
			destination: "https://evil.example.net/other-org/mcp",
			want:        "http://localhost:3000/dashboard",
		},
		{
			name:        "already on the organization's host",
			defaultHost: new(extraHost),
			origin:      originAt(requestorigin.SurfacePlatform, extraHost),
			destination: "/other-org/mcp",
			want:        "/other-org/mcp",
		},
		{
			name:        "null host stays",
			defaultHost: nil,
			origin:      originAt(requestorigin.SurfacePlatform, extraHost),
			destination: "/other-org/mcp",
			want:        "/other-org/mcp",
		},
		{
			name:        "stored host that is not a platform host stays",
			defaultHost: new("https://elsewhere.example.net"),
			origin:      serverOrigin,
			destination: "/other-org/mcp",
			want:        "/other-org/mcp",
		},
		{
			name:        "http target from an https request stays",
			defaultHost: new(testServerURL.String()),
			origin:      originAt(requestorigin.SurfacePlatform, extraHost),
			destination: "/other-org/mcp",
			want:        "/other-org/mcp",
		},
		{
			name:        "custom domain request stays",
			defaultHost: new(extraHost),
			origin:      originAt(requestorigin.SurfaceCustomDomain, "https://mcp.customer.example"),
			destination: "/other-org/mcp",
			want:        "/other-org/mcp",
		},
		{
			name:        "private network request stays",
			defaultHost: new(extraHost),
			origin:      originAt(requestorigin.SurfacePrivateNetwork, "https://gram.internal.example"),
			destination: "/other-org/mcp",
			want:        "/other-org/mcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := callbackWithOrgHost(t, tt.defaultHost, tt.origin, tt.destination)
			require.Equal(t, tt.want, result.Location)
		})
	}
}

// A destination that cannot select the organization is replaced by the
// organization's root, so the login on the other host selects the same
// organization and does not move the browser again.
func TestService_Callback_OrganizationHostNamesTheOrganization(t *testing.T) {
	t.Parallel()

	extraHost := "https://" + testExtraPlatformHost
	userInfo := speakeasyMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	for _, org := range userInfo.Organizations {
		require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
	}
	// The first membership is selected when the destination names no
	// organization the user belongs to.
	require.NoError(t, orgRepo.New(instance.conn).SetOrganizationDefaultHostForTest(ctx, orgRepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: conv.ToPGText(extraHost),
		ID:          "speakeasy-team-123",
	}))

	for _, destination := range []string{"", "/not-a-member/mcp", "//evil.example.net/x"} {
		ctx := requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, testServerURL.String()))
		ctx, stateParam := instance.stateWithNonce(ctx, t, destination)
		result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
			Code:  "mock_code",
			State: &stateParam,
		})
		require.NoError(t, err)
		require.Equal(t, extraHostLogin("/speakeasy-team"), result.Location, "destination %q", destination)
	}
}

func TestService_Callback_OrganizationHostSkipsImpersonation(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	org := userInfo.Organizations[0]
	require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
	require.NoError(t, orgRepo.New(instance.conn).SetOrganizationDefaultHostForTest(ctx, orgRepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: conv.ToPGText("https://" + testExtraPlatformHost),
		ID:          org.ID,
	}))

	ctx = requestorigin.WithContext(ctx, originAt(requestorigin.SurfacePlatform, testServerURL.String()))
	result, err := instance.service.Callback(ctx, &gen.CallbackPayload{
		Code: "impersonation_code",
	})
	require.NoError(t, err)
	require.Equal(t, instance.authConfigs.SignInRedirectURL, result.Location)
}
