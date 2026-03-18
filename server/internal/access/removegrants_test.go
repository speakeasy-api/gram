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

	// Create two grants for the same principal
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "mcp:connect", "*")

	// Remove only the build:read grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
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

func TestRemoveGrants_BatchRemovesMultipleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "mcp:connect", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "org:admin", "*")

	// Remove two of three grants in a single batch call
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "mcp:connect", Resource: "*"},
		},
	})
	require.NoError(t, err)

	urn := "user:user_abc"
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(urn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "org:admin", result.Grants[0].Scope)
}

func TestRemoveGrants_DoesNotAffectOtherPrincipals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create identical scopes for different principals
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_def", "build:read", "*")

	// Remove only user_abc's grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
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

func TestRemoveGrants_MatchesExactResourceScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	// Create grants with different resources for same principal+scope
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "project-1")

	// Remove only the project-specific grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.RemoveGrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "project-1"},
		},
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
