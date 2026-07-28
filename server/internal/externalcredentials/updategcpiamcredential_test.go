package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateGcpIamCredential_SwitchToWorkloadIdentityFederation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-switch")

	updated, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-switch-renamed",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 new("gram-pool"),
		WifProviderID:             new("gram-provider"),
		WifProjectNumber:          new("123456789012"),
	})
	require.NoError(t, err)

	require.Equal(t, "gcp-switch-renamed", updated.Name)
	require.Equal(t, "gram-pool", *updated.WifPoolID)
	require.Nil(t, updated.ImpersonateServiceAccount, "switching to WIF without a hop must clear the impersonation account")
}

func TestUpdateGcpIamCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        uuid.NewString(),
		SessionToken:              nil,
		Name:                      "missing",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateGcpIamCredential_WrongProviderNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	aws := createAWSExternalIDCredential(t, ctx, ti, "aws-for-gcp-update")

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        aws.ID,
		SessionToken:              nil,
		Name:                      "wrong-provider",
		ImpersonateServiceAccount: new("gram@customer.iam.gserviceaccount.com"),
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateGcpIamCredential_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        "not-a-uuid",
		SessionToken:              nil,
		Name:                      "bad-id",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateGcpIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-update-forbidden")

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-update-forbidden",
		ImpersonateServiceAccount: nil,
		WifPoolID:                 nil,
		WifProviderID:             nil,
		WifProjectNumber:          nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
