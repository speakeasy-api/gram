package auth_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// infoWithDefaultHost stores defaultHost on the active organization (nil keeps
// it NULL) and calls Info for a session that impersonatorEmail started (empty
// for an ordinary session) on a request from origin (nil for none).
func infoWithDefaultHost(t *testing.T, defaultHost *string, origin *requestorigin.Origin, impersonatorEmail string) *gen.InfoResult {
	t.Helper()

	userInfo := defaultMockUserInfo()
	ctx, instance := newTestAuthService(t, userInfo)
	require.NoError(t, instance.createTestUser(ctx, userInfo))
	org := userInfo.Organizations[0]
	require.NoError(t, instance.createTestOrganization(ctx, org, userInfo.UserID))
	require.NoError(t, orgRepo.New(instance.conn).SetOrganizationDefaultHostForTest(ctx, orgRepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: conv.PtrToPGText(defaultHost),
		ID:          org.ID,
	}))

	session := sessions.Session{
		SessionID:            t.Name(),
		UserID:               userInfo.UserID,
		ActiveOrganizationID: org.ID,
		WorkOSSessionID:      "",
		ImpersonatorEmail:    impersonatorEmail,
	}
	require.NoError(t, instance.sessionManager.StoreSession(ctx, session))

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		SessionID:            &session.SessionID,
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		AccountType:          "test",
		Email:                &userInfo.Email,
	})
	if origin != nil {
		ctx = requestorigin.WithContext(ctx, *origin)
	}

	result, err := instance.service.Info(ctx, &gen.InfoPayload{})
	require.NoError(t, err)
	return result
}

func platformOrigin(baseURL string) *requestorigin.Origin {
	return &requestorigin.Origin{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          baseURL,
		OrganizationID:   "",
		NetworkIngressID: uuid.Nil,
		NetworkIdentity:  nil,
	}
}

func TestService_Info_ActiveOrganizationDashboardURL(t *testing.T) {
	t.Parallel()

	extraHost := "https://" + testExtraPlatformHost

	tests := []struct {
		name        string
		defaultHost *string
		origin      *requestorigin.Origin
		want        *string
	}{
		{
			name:        "stored platform host",
			defaultHost: new(extraHost),
			origin:      platformOrigin(testServerURL.String()),
			want:        new(extraHost),
		},
		{
			name:        "stored platform host requested from that host",
			defaultHost: new(extraHost),
			origin:      platformOrigin(extraHost),
			want:        nil,
		},
		{
			name:        "stored server host moves to the dashboard URL",
			defaultHost: new(testServerURL.String()),
			origin:      platformOrigin(extraHost),
			want:        new(testSiteURL.String()),
		},
		{
			name:        "stored host that is not a platform host stays",
			defaultHost: new("https://elsewhere.example.net"),
			origin:      platformOrigin(extraHost),
			want:        nil,
		},
		{
			name:        "null host leaves the dashboard where it is",
			defaultHost: nil,
			origin:      platformOrigin(extraHost),
			want:        nil,
		},
		{
			name:        "custom domain request",
			defaultHost: new(extraHost),
			origin: &requestorigin.Origin{
				Surface:          requestorigin.SurfaceCustomDomain,
				BaseURL:          "https://mcp.customer.example",
				OrganizationID:   "",
				NetworkIngressID: uuid.Nil,
				NetworkIdentity:  nil,
			},
			want: nil,
		},
		{
			name:        "private network request",
			defaultHost: new(extraHost),
			origin: &requestorigin.Origin{
				Surface:          requestorigin.SurfacePrivateNetwork,
				BaseURL:          "https://gram.internal.example",
				OrganizationID:   "",
				NetworkIngressID: uuid.Nil,
				NetworkIdentity:  nil,
			},
			want: nil,
		},
		{
			name:        "request without a classified origin",
			defaultHost: new(extraHost),
			origin:      nil,
			want:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := infoWithDefaultHost(t, tt.defaultHost, tt.origin, "")
			require.Equal(t, tt.want, result.ActiveOrganizationDashboardURL)
		})
	}
}

func TestService_Info_ActiveOrganizationDashboardURL_OmittedForImpersonation(t *testing.T) {
	t.Parallel()

	extraHost := "https://" + testExtraPlatformHost
	result := infoWithDefaultHost(t, new(extraHost), platformOrigin(testServerURL.String()), "operator@example.com")
	require.Nil(t, result.ActiveOrganizationDashboardURL)
}
