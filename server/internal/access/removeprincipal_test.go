package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRemovePrincipalGrants_RemovesAllForPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create multiple grants for the same principal
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "mcp:connect", "*")

	// Create a grant for a different principal
	upsertGrant(t, ctx, ti.service, "role:admin", "org:admin", "*")

	// Remove all grants for user:user_abc
	err := ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: "user:user_abc",
	})
	require.NoError(t, err)

	// Verify user:user_abc grants are gone
	urn := "user:user_abc"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Empty(t, result.Grants)

	// Verify role:admin grant still exists
	roleUrn := "role:admin"
	result, err = ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(roleUrn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "role:admin", result.Grants[0].PrincipalUrn)
}

func TestRemovePrincipalGrants_NoOpWhenPrincipalHasNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: "user:nonexistent",
	})
	require.NoError(t, err)
}

func TestRemovePrincipalGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: "invalid",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestRemovePrincipalGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	err := ti.service.RemovePrincipalGrants(t.Context(), &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: "user:user_abc",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
