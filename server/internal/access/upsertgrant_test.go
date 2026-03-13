package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestAccessService_UpsertGrant(t *testing.T) {
	t.Parallel()

	t.Run("creates a new grant", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		grant, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.NoError(t, err)
		require.NotNil(t, grant)

		require.NotEmpty(t, grant.ID)
		require.Equal(t, "user:user_abc", grant.PrincipalUrn)
		require.Equal(t, "user", grant.PrincipalType)
		require.Equal(t, "build:read", grant.Scope)
		require.Equal(t, "*", grant.Resource)
		require.NotEmpty(t, grant.CreatedAt)
		require.NotEmpty(t, grant.UpdatedAt)
		require.NotEmpty(t, grant.OrganizationID)
	})

	t.Run("idempotent for same tuple", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		grant1, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.NoError(t, err)

		grant2, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.NoError(t, err)

		require.Equal(t, grant1.ID, grant2.ID)
		require.Equal(t, grant1.CreatedAt, grant2.CreatedAt)
	})

	t.Run("different scopes create separate grants", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		grant1, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.NoError(t, err)

		grant2, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:write",
			Resource:     "*",
		})
		require.NoError(t, err)

		require.NotEqual(t, grant1.ID, grant2.ID)
		require.Equal(t, "build:read", grant1.Scope)
		require.Equal(t, "build:write", grant2.Scope)
	})

	t.Run("different resources create separate grants", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		grant1, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "project-1",
		})
		require.NoError(t, err)

		grant2, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "project-2",
		})
		require.NoError(t, err)

		require.NotEqual(t, grant1.ID, grant2.ID)
	})

	t.Run("role principal type", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		grant, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "role:project:admin",
			Scope:        "org:admin",
			Resource:     "*",
		})
		require.NoError(t, err)

		require.Equal(t, "role:project:admin", grant.PrincipalUrn)
		require.Equal(t, "role", grant.PrincipalType)
	})

	t.Run("invalid principal URN", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestAccessService(t)

		_, err := ti.service.UpsertGrant(ctx, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "invalid",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	})

	t.Run("unauthorized without auth context", func(t *testing.T) {
		t.Parallel()

		_, ti := newTestAccessService(t)

		ctxWithoutAuth := t.Context()

		_, err := ti.service.UpsertGrant(ctxWithoutAuth, &gen.UpsertGrantPayload{
			SessionToken: nil,
			PrincipalUrn: "user:user_abc",
			Scope:        "build:read",
			Resource:     "*",
		})
		require.Error(t, err)

		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
	})
}
