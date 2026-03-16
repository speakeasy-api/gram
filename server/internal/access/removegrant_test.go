package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestRemoveGrant_RemovesSingleGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create two grants for the same principal
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "mcp:connect", "*")

	// Remove only the build:read grant
	err := ti.service.RemoveGrant(ctx, &gen.RemoveGrantPayload{
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.NoError(t, err)

	// Verify only one grant remains
	urn := "user:user_abc"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "mcp:connect", result.Grants[0].Scope)
}

func TestRemoveGrant_ReturnsNotFoundWhenGrantDoesNotExist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemoveGrant(ctx, &gen.RemoveGrantPayload{
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestRemoveGrant_DoesNotAffectOtherPrincipals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create identical scopes for different principals
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_def", "build:read", "*")

	// Remove only user_abc's grant
	err := ti.service.RemoveGrant(ctx, &gen.RemoveGrantPayload{
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.NoError(t, err)

	// Verify user_def's grant is untouched
	urn := "user:user_def"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "user:user_def", result.Grants[0].PrincipalUrn)
}

func TestRemoveGrant_MatchesExactResourceScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create grants with different resources for same principal+scope
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "project-1")

	// Remove only the project-specific grant
	err := ti.service.RemoveGrant(ctx, &gen.RemoveGrantPayload{
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "project-1",
	})
	require.NoError(t, err)

	// Verify the wildcard grant remains
	urn := "user:user_abc"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "*", result.Grants[0].Resource)
}

func TestRemoveGrant_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemoveGrant(ctx, &gen.RemoveGrantPayload{
		PrincipalUrn: "invalid",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestRemoveGrant_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	err := ti.service.RemoveGrant(t.Context(), &gen.RemoveGrantPayload{
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
