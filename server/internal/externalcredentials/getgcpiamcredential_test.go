package externalcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestGetGcpIamCredential_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-get")

	got, err := ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *got.ImpersonateServiceAccount)
}

func TestGetGcpIamCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetGcpIamCredential_InvalidID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           "not-a-uuid",
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetGcpIamCredential_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-get-no-entitlement")
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.GetGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpIamCredentialPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
