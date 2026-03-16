package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpsertGrants_CreatesNewGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)

	grant := result.Grants[0]
	require.NotEmpty(t, grant.ID)
	require.Equal(t, "user:user_abc", grant.PrincipalUrn)
	require.Equal(t, "user", grant.PrincipalType)
	require.Equal(t, "build:read", grant.Scope)
	require.Equal(t, "*", grant.Resource)
	require.NotEmpty(t, grant.CreatedAt)
	require.NotEmpty(t, grant.UpdatedAt)
	require.NotEmpty(t, grant.OrganizationID)
}

func TestUpsertGrants_BatchCreatesMultipleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
			{PrincipalUrn: "user:user_abc", Scope: "mcp:connect", Resource: "*"},
			{PrincipalUrn: "role:admin", Scope: "org:admin", Resource: "*"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 3)

	require.Equal(t, "build:read", result.Grants[0].Scope)
	require.Equal(t, "mcp:connect", result.Grants[1].Scope)
	require.Equal(t, "role:admin", result.Grants[2].PrincipalUrn)
}

func TestUpsertGrants_IdempotentForSameTuple(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result1, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	result2, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	require.Equal(t, result1.Grants[0].ID, result2.Grants[0].ID)
	require.Equal(t, result1.Grants[0].CreatedAt, result2.Grants[0].CreatedAt)
}

func TestUpsertGrants_UniqueTuplesCreateSeparateGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		grant1 gen.UpsertGrantForm
		grant2 gen.UpsertGrantForm
	}{
		{
			name:   "different scopes",
			grant1: gen.UpsertGrantForm{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
			grant2: gen.UpsertGrantForm{PrincipalUrn: "user:user_abc", Scope: "build:write", Resource: "*"},
		},
		{
			name:   "different resources",
			grant1: gen.UpsertGrantForm{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "project-1"},
			grant2: gen.UpsertGrantForm{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "project-2"},
		},
		{
			name:   "different principals",
			grant1: gen.UpsertGrantForm{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
			grant2: gen.UpsertGrantForm{PrincipalUrn: "user:user_def", Scope: "build:read", Resource: "*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestAccessService(t)

			result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
				Grants: []*gen.UpsertGrantForm{&tt.grant1, &tt.grant2},
			})
			require.NoError(t, err)
			require.Len(t, result.Grants, 2)
			require.NotEqual(t, result.Grants[0].ID, result.Grants[1].ID)
		})
	}
}

func TestUpsertGrants_RolePrincipalType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "role:project:admin", Scope: "org:admin", Resource: "*"},
		},
	})
	require.NoError(t, err)

	require.Equal(t, "role:project:admin", result.Grants[0].PrincipalUrn)
	require.Equal(t, "role", result.Grants[0].PrincipalType)
}

func TestUpsertGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "invalid", Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestUpsertGrants_FailsOnFirstInvalidURNInBatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "user:valid", Scope: "build:read", Resource: "*"},
			{PrincipalUrn: "invalid", Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestUpsertGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)

	_, err := ti.service.UpsertGrants(t.Context(), &gen.UpsertGrantsPayload{
		Grants: []*gen.UpsertGrantForm{
			{PrincipalUrn: "user:user_abc", Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
