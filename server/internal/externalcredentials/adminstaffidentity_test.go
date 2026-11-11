package externalcredentials_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	adminecgen "github.com/speakeasy-api/gram/server/gen/admin_external_credentials"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

// newPlatformTestService builds the service the standalone admin server uses:
// no dashboard sessions, RBAC or audit logger, only the platform handlers.
func newPlatformTestService(t *testing.T) (*externalcredentials.Service, *pgxpool.Pool) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return externalcredentials.NewPlatformService(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, gcpauth.NewIdentity(gcpauth.NewStubResolver())), conn
}

// seedStaffUser records a Speakeasy user for the staff identity's email, with
// or without the platform-admin entitlement, and returns its id.
func seedStaffUser(t *testing.T, conn *pgxpool.Pool, email string, admin bool) string {
	t.Helper()

	id := "user_" + uuid.NewString()
	q := testrepo.New(conn)
	require.NoError(t, q.InsertUserFixture(t.Context(), testrepo.InsertUserFixtureParams{ID: id, Email: email, DisplayName: "Test Operator"}))
	require.NoError(t, q.SetUserPlatformAdminFixture(t.Context(), testrepo.SetUserPlatformAdminFixtureParams{Admin: admin, ID: id}))
	return id
}

func createAmbientPlatformCredential(ctx context.Context, svc *externalcredentials.Service) error {
	_, err := svc.CreateGcpIamPlatformCredential(ctx, &adminecgen.CreateGcpIamPlatformCredentialPayload{
		SessionToken:              nil,
		Name:                      "x",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	return err //nolint:wrapcheck // Test helper.
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
	svc, conn := newPlatformTestService(t)
	seedStaffUser(t, conn, "operator@example.com", true)
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
	svc, _ := newPlatformTestService(t)
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
	svc, _ := newPlatformTestService(t)
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
	svc, conn := newPlatformTestService(t)
	seedStaffUser(t, conn, "operator@example.com", true)

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

// Signing in to the admin app is not enough: the staff email must belong to a
// live platform administrator, for reads and writes alike.
func TestPlatformCredentials_StaffIdentityRequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	t.Run("no Speakeasy user", func(t *testing.T) {
		t.Parallel()
		svc, _ := newPlatformTestService(t)
		ctx := withStaffIdentity(t.Context())

		requireOopsCode(t, createAmbientPlatformCredential(ctx, svc), oops.CodeForbidden)
		_, err := svc.ListPlatformExternalCredentials(ctx, &adminecgen.ListPlatformExternalCredentialsPayload{SessionToken: nil, Provider: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("user without the entitlement", func(t *testing.T) {
		t.Parallel()
		svc, conn := newPlatformTestService(t)
		seedStaffUser(t, conn, "operator@example.com", false)
		ctx := withStaffIdentity(t.Context())

		requireOopsCode(t, createAmbientPlatformCredential(ctx, svc), oops.CodeForbidden)
		_, err := svc.ListPlatformExternalCredentials(ctx, &adminecgen.ListPlatformExternalCredentialsPayload{SessionToken: nil, Provider: nil})
		requireOopsCode(t, err, oops.CodeForbidden)
	})

	t.Run("deleted administrator", func(t *testing.T) {
		t.Parallel()
		svc, conn := newPlatformTestService(t)
		id := seedStaffUser(t, conn, "operator@example.com", true)
		require.NoError(t, testrepo.New(conn).ForceSoftDeleteUser(t.Context(), id))

		requireOopsCode(t, createAmbientPlatformCredential(withStaffIdentity(t.Context()), svc), oops.CodeForbidden)
	})

	t.Run("email matched case-insensitively", func(t *testing.T) {
		t.Parallel()
		svc, conn := newPlatformTestService(t)
		seedStaffUser(t, conn, "Operator@Example.com", true)

		require.NoError(t, createAmbientPlatformCredential(withStaffIdentity(t.Context()), svc))
	})
}
