package externalcredentials_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

// newPlatformTestService builds the service the standalone admin server uses:
// no dashboard sessions, RBAC or audit logger, only the platform handlers.
func newPlatformTestService(t *testing.T) *externalcredentials.Service {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return externalcredentials.NewPlatformService(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, gcpauth.NewIdentity(gcpauth.NewStubResolver()))
}

func withStaffIdentity(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "admin-session",
		Email:       "operator@example.com",
		OIDCSubject: "sub-operator",
		Name:        "Test Operator",
		HD:          "example.com",
	})
}

func TestPlatformCredentials_StaffIdentityManagesLifecycle(t *testing.T) {
	t.Parallel()
	svc := newPlatformTestService(t)
	ctx := withStaffIdentity(t.Context())

	target := gramProjectServiceAccount("platform-signer")
	created, err := svc.CreateGcpIamPlatformCredential(ctx, &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "Platform signer",
		ImpersonateServiceAccount: &target,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Platform signer", created.Name)
	require.Equal(t, "gcp_iam", created.Provider)

	listed, err := svc.ListPlatformExternalCredentials(ctx, &adminecgen.ListPlatformExternalCredentialsPayload{SessionToken: nil, Provider: nil})
	require.NoError(t, err)
	require.Len(t, listed.Credentials, 1)
	require.Equal(t, created.ID, listed.Credentials[0].ID)

	updated, err := svc.UpdateGcpIamPlatformCredential(ctx, &adminecgen.UpdateGcpIamPlatformCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "Renamed signer",
		ImpersonateServiceAccount: &target,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed signer", updated.Name)

	got, err := svc.GetGcpIamPlatformCredential(ctx, &adminecgen.GetGcpIamPlatformCredentialPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "Renamed signer", got.Name)
	require.Equal(t, &target, got.ImpersonateServiceAccount)

	verified, err := svc.VerifyGcpIamPlatformCredential(ctx, &adminecgen.VerifyGcpIamPlatformCredentialPayload{ID: created.ID, SessionToken: nil})
	require.NoError(t, err)
	require.True(t, verified.Verified)

	require.NoError(t, svc.DeleteGcpIamPlatformCredential(ctx, &adminecgen.DeleteGcpIamPlatformCredentialPayload{ID: created.ID, SessionToken: nil}))

	_, err = svc.GetGcpIamPlatformCredential(ctx, &adminecgen.GetGcpIamPlatformCredentialPayload{ID: created.ID, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// An admin context without a verified session is not a staff identity, and must
// not fall through to the dashboard session gates either.
func TestPlatformCredentials_RejectsUnverifiedStaffIdentity(t *testing.T) {
	t.Parallel()
	svc := newPlatformTestService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
		SessionID:   "",
		Email:       "operator@example.com",
		OIDCSubject: "sub-operator",
		Name:        "Test Operator",
		HD:          "example.com",
	})

	_, createErr := svc.CreateGcpIamPlatformCredential(ctx, &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "x",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, createErr, oops.CodeUnauthorized)

	_, listErr := svc.ListPlatformExternalCredentials(ctx, &adminecgen.ListPlatformExternalCredentialsPayload{SessionToken: nil, Provider: nil})
	requireOopsCode(t, listErr, oops.CodeUnauthorized)
}

// The admin-server service has no dashboard session store, so a dashboard
// session reaching it can never satisfy the fresh-session gate on a write.
func TestPlatformCredentials_PlatformServiceRefusesDashboardWrites(t *testing.T) {
	t.Parallel()
	svc := newPlatformTestService(t)
	ctx := contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{
		UserID:    "user-1",
		Email:     new("operator@example.com"),
		SessionID: new("session-1"),
		IsAdmin:   true,
	}, false)

	_, err := svc.CreateGcpIamPlatformCredential(ctx, &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "x",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeUnavailable)
}

func TestPlatformCredentials_RejectsNonNumericWifProjectNumber(t *testing.T) {
	t.Parallel()
	svc := newPlatformTestService(t)

	_, err := svc.CreateGcpIamPlatformCredential(withStaffIdentity(t.Context()), &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "wif",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 new("pool"),
		WifProviderID:             new("provider"),
		WifProjectNumber:          new("abc"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
