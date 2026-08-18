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

func TestUpdateGcpIamCredential_RenameAndRetarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-update")

	updated, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-update-renamed",
		ImpersonateServiceAccount: "other@customer.iam.gserviceaccount.com",
	})
	require.NoError(t, err)

	require.Equal(t, "gcp-update-renamed", updated.Name)
	require.Equal(t, "other@customer.iam.gserviceaccount.com", *updated.ImpersonateServiceAccount)
	require.Nil(t, updated.WifPoolID)
}

// The organization form cannot express WIF, so an update always writes the
// impersonation-only shape. A credential that predates the impersonation-only
// rule converges to it rather than keeping columns the form cannot show.
func TestUpdateGcpIamCredential_ClearsLegacyWifColumns(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPWifCredentialDirect(t, ctx, ti, "gcp-legacy-wif")
	require.NotNil(t, created.WifPoolID, "fixture should start with WIF columns set")

	updated, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-legacy-wif",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	require.NoError(t, err)

	require.Nil(t, updated.WifPoolID)
	require.Nil(t, updated.WifProviderID)
	require.Nil(t, updated.WifProjectNumber)
	require.Equal(t, "gram@customer.iam.gserviceaccount.com", *updated.ImpersonateServiceAccount)
}

func TestUpdateGcpIamCredential_BlankImpersonationRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-update-blank")

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-update-blank",
		ImpersonateServiceAccount: "",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateGcpIamCredential_TargetInGramProjectRejected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-update-self-project")

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-update-self-project",
		ImpersonateServiceAccount: gramProjectServiceAccount("someone-else"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateGcpIamCredential_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        uuid.NewString(),
		SessionToken:              nil,
		Name:                      "missing",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
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
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
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
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
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
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateGcpIamCredential_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPImpersonationCredential(t, ctx, ti, "gcp-update-no-entitlement")
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.UpdateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-update-no-entitlement",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
