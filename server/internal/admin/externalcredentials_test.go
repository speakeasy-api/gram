package admin

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
)

func withPlatformCredentialStaff(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID:   "admin-session",
		Email:       "operator@example.com",
		OIDCSubject: "sub-operator",
		Name:        "Test Operator",
		HD:          "example.com",
	})
}

// Each handler maps its result field by field, so a round trip through all six
// proves the conversions as well as the dispatch.
func TestPlatformCredentials_RoundTripThroughAdminHandlers(t *testing.T) {
	t.Parallel()

	_, svc, db := newTestAdminService(t)
	svc.SetExternalCredentialService(externalcredentials.NewPlatformService(testenv.NewLogger(t), testenv.NewTracerProvider(t), db, gcpauth.NewIdentity(gcpauth.NewStubResolver())))
	ctx := withPlatformCredentialStaff(t.Context())

	// The stub resolver's own project, where platform credentials are expected.
	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	target := "signer@" + domain

	created, err := svc.CreateGcpIamPlatformCredential(ctx, &gen.CreateGcpIamPlatformCredentialPayload{
		AdminSessionToken: nil, Name: "Platform signer", ImpersonateServiceAccount: &target,
		WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Platform signer", created.Name)
	require.Equal(t, "gcp_iam", created.Provider)
	require.Equal(t, &target, created.ImpersonateServiceAccount)
	require.NotEmpty(t, created.CreatedAt)

	listed, err := svc.ListPlatformExternalCredentials(ctx, &gen.ListPlatformExternalCredentialsPayload{AdminSessionToken: nil, Provider: nil})
	require.NoError(t, err)
	require.Len(t, listed.Credentials, 1)
	require.Equal(t, gen.ExternalCredentialSummary{
		ID: created.ID, OrganizationID: created.OrganizationID, Provider: "gcp_iam", Name: "Platform signer",
		CreatedAt: created.CreatedAt, UpdatedAt: created.UpdatedAt,
	}, *listed.Credentials[0])

	updated, err := svc.UpdateGcpIamPlatformCredential(ctx, &gen.UpdateGcpIamPlatformCredentialPayload{
		ID: created.ID, AdminSessionToken: nil, Name: "Renamed", ImpersonateServiceAccount: &target,
		WifPoolID: nil, WifProviderID: nil, WifProjectNumber: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)

	got, err := svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, updated, got)

	verified, err := svc.VerifyGcpIamPlatformCredential(ctx, &gen.VerifyGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	require.NoError(t, err)
	require.True(t, verified.Verified)
	require.NotNil(t, verified.Principal)

	require.NoError(t, svc.DeleteGcpIamPlatformCredential(ctx, &gen.DeleteGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID}))

	_, err = svc.GetGcpIamPlatformCredential(ctx, &gen.GetGcpIamPlatformCredentialPayload{AdminSessionToken: nil, ID: created.ID})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

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
