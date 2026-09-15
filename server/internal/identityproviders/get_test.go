package identityproviders_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetReturnsConnectionWithOrganizationRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createConnection(t, ctx, ti, "https://acme.okta.com")
	ctx = withExactScope(t, ctx, ti, authz.ScopeOrgRead)

	result, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, created, result.Connection)
}

func TestGetReturnsNilWhenAbsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withExactScope(t, ctx, ti, authz.ScopeOrgRead)

	result, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.Connection)
}

func TestGetRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.Get(ctx, &gen.GetPayload{SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}
