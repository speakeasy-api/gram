package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

func TestCreateGcpIamCredential_Impersonation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-impersonation",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	require.NoError(t, err)

	require.Equal(t, "gcp_iam", cred.Provider)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *cred.ImpersonateServiceAccount)
	require.Nil(t, cred.WifPoolID, "the organization tier never writes WIF columns")
	require.Nil(t, cred.WifProviderID)
	require.Nil(t, cred.WifProjectNumber)
}

// The organization tier is impersonation-only: ambient mode would resolve Gram's
// own identity, which says nothing about the customer's configuration, so a
// blank target is rejected rather than silently recorded as ambient.
func TestCreateGcpIamCredential_BlankImpersonationRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-blank-target",
		ImpersonateServiceAccount: "   ",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// getGcpSetupInfo publishes Gram's own service account by design, so without
// this guard an organization member could point a credential at an internal
// service account and use verify to probe Gram's own project.
func TestCreateGcpIamCredential_TargetInGramProjectRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-self-project",
		ImpersonateServiceAccount: gramProjectServiceAccount("someone-else"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateGcpIamCredential_ForbiddenForReadOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-forbidden",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateGcpIamCredential_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-no-entitlement",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
