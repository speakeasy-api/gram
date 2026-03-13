package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestAccessService_ListGrants(t *testing.T) {
	t.Parallel()

	t.Run("empty when no grants exist", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Empty(t, result.Grants)
	})

	t.Run("returns all grants for org", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		_, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.NoError(t, err)

		_, err = ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "role:admin",
			Scope:        "org:admin",
			Resource:     "*",
		})
		require.NoError(t, err)

		result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
			SessionToken: nil,
		})
		require.NoError(t, err)
		require.Len(t, result.Grants, 2)
	})

	t.Run("filters by principal URN", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		_, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.NoError(t, err)

		_, err = ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "role:admin",
			Scope:        "org:admin",
			Resource:     "*",
		})
		require.NoError(t, err)

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
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestAccessService(t)

		ctxWithoutAuth := t.Context()

		_, err := ti.service.ListGrants(ctxWithoutAuth, &gen.ListGrantsPayload{
			SessionToken: nil,
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})
}
