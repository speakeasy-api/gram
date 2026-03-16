package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListGrants_EmptyWhenNoGrantsExist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Grants)
}

func TestListGrants_ReturnsAllGrantsForOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "role:admin", "org:admin", "*")

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
}

func TestListGrants_FiltersByPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "role:admin", "org:admin", "*")

	urn := "user:user_abc"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "user:user_abc", result.Grants[0].PrincipalUrn)
	require.Equal(t, "user", result.Grants[0].PrincipalType)
	require.Equal(t, "build:read", result.Grants[0].Scope)
}

func TestListGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.ListGrants(t.Context(), &gen.ListGrantsPayload{})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
