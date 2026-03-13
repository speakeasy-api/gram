package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpsertGrant_CreatesNewGrant(t *testing.T) {
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
}

func TestUpsertGrant_IdempotentForSameTuple(t *testing.T) {
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
}

func TestUpsertGrant_UniqueTuplesCreateSeparateGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		grant1 gen.UpsertGrantPayload
		grant2 gen.UpsertGrantPayload
	}{
		{
			name: "different scopes",
			grant1: gen.UpsertGrantPayload{
				PrincipalUrn: "user:user_abc",
				Scope:        "build:read",
				Resource:     "*",
			},
			grant2: gen.UpsertGrantPayload{
				PrincipalUrn: "user:user_abc",
				Scope:        "build:write",
				Resource:     "*",
			},
		},
		{
			name: "different resources",
			grant1: gen.UpsertGrantPayload{
				PrincipalUrn: "user:user_abc",
				Scope:        "build:read",
				Resource:     "project-1",
			},
			grant2: gen.UpsertGrantPayload{
				PrincipalUrn: "user:user_abc",
				Scope:        "build:read",
				Resource:     "project-2",
			},
		},
		{
			name: "different principals",
			grant1: gen.UpsertGrantPayload{
				PrincipalUrn: "user:user_abc",
				Scope:        "build:read",
				Resource:     "*",
			},
			grant2: gen.UpsertGrantPayload{
				PrincipalUrn: "user:user_def",
				Scope:        "build:read",
				Resource:     "*",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestAccessService(t)

			g1, err := ti.service.UpsertGrant(ctx, &tt.grant1)
			require.NoError(t, err)

			g2, err := ti.service.UpsertGrant(ctx, &tt.grant2)
			require.NoError(t, err)

			require.NotEqual(t, g1.ID, g2.ID)
		})
	}
}

func TestUpsertGrant_RolePrincipalType(t *testing.T) {
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
}

func TestUpsertGrant_InvalidPrincipalURN(t *testing.T) {
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
}

func TestUpsertGrant_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.UpsertGrant(t.Context(), &gen.UpsertGrantPayload{
		PrincipalUrn: "user:user_abc",
		Scope:        "build:read",
		Resource:     "*",
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
