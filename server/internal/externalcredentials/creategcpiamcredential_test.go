package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateGcpIamCredential_Impersonation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-impersonation",
		ImpersonateServiceAccount: new("gram@customer.iam.gserviceaccount.com"),
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)

	require.Equal(t, "gcp_iam", cred.Provider)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *cred.ImpersonateServiceAccount)
	require.Nil(t, cred.WifPoolID)
}

func TestCreateGcpIamCredential_WorkloadIdentityFederation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-wif",
		ImpersonateServiceAccount: new("gram@customer.iam.gserviceaccount.com"),
		WifPoolID:                 new("gram-pool"),
		WifProviderID:             new("gram-provider"),
		WifProjectNumber:          new("123456789012"),
	})
	require.NoError(t, err)

	require.Equal(t, "gram-pool", *cred.WifPoolID)
	require.Equal(t, "gram-provider", *cred.WifProviderID)
	require.Equal(t, "123456789012", *cred.WifProjectNumber)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *cred.ImpersonateServiceAccount, "impersonate_service_account is allowed as the WIF hop")
}

func TestCreateGcpIamCredential_Ambient(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-ambient",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	require.NoError(t, err)

	require.Nil(t, cred.ImpersonateServiceAccount)
	require.Nil(t, cred.WifPoolID)
}

func TestCreateGcpIamCredential_PartialWifRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-wif-incomplete",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 new("gram-pool"),
		WifProviderID:             new("gram-provider"),
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateGcpIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-forbidden",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
