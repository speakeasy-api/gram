package externalcredentials_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
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

// The screening exists so verify cannot become an oracle for Gram's own project.
// If Gram's identity is unresolvable there is nothing to screen against, so the
// write is refused rather than accepted unscreened.
func TestCreateGcpIamCredential_FailsClosedWhenGramIdentityUnresolvable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "", Source: ""}, errors.New("no application default credentials")
	})

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-unresolvable-gram-identity",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

// Likewise when Gram resolves to an address that cannot be placed in a project,
// such as a default compute service account: comparing a project number against
// a project id would silently accept every target.
func TestCreateGcpIamCredential_FailsClosedWhenGramIdentityNotUserManaged(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "123456789012-compute@developer.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
	})

	_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-gram-default-compute",
		ImpersonateServiceAccount: "gram@customer.iam.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

// A target that is not user-managed is refused for the same reason: its address
// identifies its project by number, so it cannot be screened against Gram's.
func TestCreateGcpIamCredential_RejectsNonUserManagedTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	for _, target := range []string{
		"123456789012-compute@developer.gserviceaccount.com",
		"customer@appspot.gserviceaccount.com",
		"someone@example.com",
	} {
		_, err := ti.service.CreateGcpIamCredential(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)), &gen.CreateGcpIamCredentialPayload{
			SessionToken:              nil,
			Name:                      "gcp-non-user-managed-target",
			ImpersonateServiceAccount: target,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}
