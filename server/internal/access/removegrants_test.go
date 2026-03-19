package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRemoveGrants_RemovesSingleGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	userURN := "user:user_abc"

	// Create two grants for the same principal
	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, userURN, "mcp:connect", "*")

	// Remove only the build:read grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, userURN), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	// Verify only one grant remains
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(userURN),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "mcp:connect", result.Grants[0].Scope)
}

func TestRemoveGrants_BatchRemovesMultipleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	userURN := "user:user_abc"

	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, userURN, "mcp:connect", "*")
	upsertGrant(t, ctx, ti.service, userURN, "org:admin", "*")

	// Remove two of three grants in a single batch call
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, userURN), Scope: "build:read", Resource: "*"},
			{PrincipalUrn: mustParsePrincipal(t, userURN), Scope: "mcp:connect", Resource: "*"},
		},
	})
	require.NoError(t, err)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(userURN),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "org:admin", result.Grants[0].Scope)
}

func TestRemoveGrants_DoesNotAffectOtherPrincipals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	abcURN := "user:user_abc"
	defURN := "user:user_abc"

	// Create identical scopes for different principals
	upsertGrant(t, ctx, ti.service, abcURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, defURN, "build:read", "*")

	// Remove only user_abc's grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, abcURN), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	// Verify user_def's grant is untouched
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(defURN),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, defURN, result.Grants[0].PrincipalUrn)
}

func TestRemoveGrants_MatchesExactResourceScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	userURN := "user:user_abc"

	// Create grants with different resources for same principal+scope
	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, userURN, "build:read", "project-1")

	// Remove only the project-specific grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, userURN), Scope: "build:read", Resource: "project-1"},
		},
	})
	require.NoError(t, err)

	// Verify the wildcard grant remains
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(userURN),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "*", result.Grants[0].Resource)
}

func TestRemoveGrants_NoOpForNonExistentGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Removing a grant that doesn't exist is a silent no-op (batch semantics)
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)
}

// TestRemoveGrants_InvalidPrincipalURN verifies that a zero-value Principal
// (invalid URN) is rejected. URN format validation now happens during JSON
// deserialization at the HTTP layer via urn.Principal.UnmarshalJSON.
func TestRemoveGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: urn.Principal{}, Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)
}

func TestRemoveGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	err := ti.service.RemoveGrants(t.Context(), &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
