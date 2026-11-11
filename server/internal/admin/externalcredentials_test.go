package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestPlatformCredentials_UnavailableWithoutService(t *testing.T) {
	t.Parallel()

	svc := &Service{externalCredentials: nil}
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
		SessionID:   "admin-session",
		Email:       "operator@example.com",
		OIDCSubject: "sub-operator",
		Name:        "Test Operator",
		HD:          "example.com",
	})

	_, err := svc.ListPlatformExternalCredentials(ctx, &gen.ListPlatformExternalCredentialsPayload{AdminSessionToken: nil, Provider: nil})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnavailable, oopsErr.Code)
}

func TestPlatformCredentials_RequireStaffIdentity(t *testing.T) {
	t.Parallel()

	svc := &Service{externalCredentials: nil}

	err := svc.DeleteGcpIamPlatformCredential(t.Context(), &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: "00000000-0000-0000-0000-000000000000"})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
