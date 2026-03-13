package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRemoveGrants_RemovesAllForPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create multiple grants for the same principal
	_, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
		SessionToken: nil,
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.NoError(t, err)

	_, err = ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
		SessionToken: nil,
		PrincipalUrn: "user:user_abc",
		Scope:        "mcp:connect",
		Resource:     "*",
	})
	require.NoError(t, err)

	// Create a grant for a different principal
	_, err = ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
		SessionToken: nil,
		PrincipalUrn: "role:admin",
		Scope:        "org:admin",
		Resource:     "*",
	})
	require.NoError(t, err)

	// Remove grants for user:user_abc
	err = ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: "user:user_abc",
	})
	require.NoError(t, err)

	// Verify user:user_abc grants are gone
	urn := "user:user_abc"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Empty(t, result.Grants)

	// Verify role:admin grant still exists
	roleUrn := "role:admin"
	result, err = ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: conv.PtrEmpty(roleUrn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "role:admin", result.Grants[0].PrincipalUrn)
}

func TestRemoveGrants_NoOpWhenPrincipalHasNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: "user:nonexistent",
	})
	require.NoError(t, err)
}

func TestRemoveGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: "invalid",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestRemoveGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	err := ti.service.RemoveGrants(t.Context(), &gen.RemoveGrantsPayload{
		PrincipalUrn: "user:user_abc",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
