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

func TestGetGcpSetupInfo_ReportsGramPrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	info, err := ti.service.GetGcpSetupInfo(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpSetupInfoPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.Equal(t, gcpauth.StubResolverPrincipal, *info.ServiceAccountEmail)
	require.Equal(t, "roles/iam.serviceAccountTokenCreator", info.RequiredRole)
}

// Gram's own identity is fixed for the process lifetime, so it is resolved once
// rather than on every read.
func TestGetGcpSetupInfo_MemoizesResolution(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	calls := 0
	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		calls++
		return gcpauth.Principal{Email: "gram@gram-platform.iam.gserviceaccount.com", Source: gcpauth.SourceMetadataServer}, nil
	})

	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource))
	for range 3 {
		_, err := ti.service.GetGcpSetupInfo(readCtx, &gen.GetGcpSetupInfoPayload{SessionToken: nil})
		require.NoError(t, err)
	}

	require.Equal(t, 1, calls)
}

// The role to grant is still useful without the email, and the page needs to
// render in environments that carry no service-account identity, so an
// unresolvable principal is reported as absent rather than as an error.
func TestGetGcpSetupInfo_UnresolvablePrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	ti.gcpResolver.SetResolve(func(_ context.Context, _ gcpauth.Credential) (gcpauth.Principal, error) {
		return gcpauth.Principal{Email: "", Source: ""}, errors.New("no application default credentials")
	})

	info, err := ti.service.GetGcpSetupInfo(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpSetupInfoPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)

	require.Nil(t, info.ServiceAccountEmail)
	require.Equal(t, "roles/iam.serviceAccountTokenCreator", info.RequiredRole)
}

func TestGetGcpSetupInfo_ForbiddenWithoutGrants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetGcpSetupInfo(authztest.WithExactGrants(t, ctx), &gen.GetGcpSetupInfoPayload{
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetGcpSetupInfo_ForbiddenWithoutEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureCustomerManagedEncryptionKeys)

	_, err := ti.service.GetGcpSetupInfo(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authz.WildcardResource)), &gen.GetGcpSetupInfoPayload{
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
