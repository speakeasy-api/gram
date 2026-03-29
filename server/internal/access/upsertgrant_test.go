package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestUpsertGrants_CreatesNewGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
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

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpsertGrants_BatchCreatesMultipleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "mcp:connect", Resource: "*"},
			{PrincipalUrn: mustParsePrincipal(t, "role:admin"), Scope: "org:admin", Resource: "*"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 3)

	require.Equal(t, "build:read", result.Grants[0].Scope)
	require.Equal(t, "mcp:connect", result.Grants[1].Scope)
	require.Equal(t, "role:admin", result.Grants[2].PrincipalUrn)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+3, afterCount)
}

func TestUpsertGrants_IdempotentForSameTuple(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	result1, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	result2, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	require.Equal(t, result1.Grants[0].ID, result2.Grants[0].ID)
	require.Equal(t, result1.Grants[0].CreatedAt, result2.Grants[0].CreatedAt)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
}

func TestUpsertGrants_DifferentScopesCreateSeparateGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:write", Resource: "*"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.NotEqual(t, result.Grants[0].ID, result.Grants[1].ID)
}

func TestUpsertGrants_DifferentResourcesCreateSeparateGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "project-1"},
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "project-2"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.NotEqual(t, result.Grants[0].ID, result.Grants[1].ID)
}

func TestUpsertGrants_DifferentPrincipalsCreateSeparateGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
			{PrincipalUrn: mustParsePrincipal(t, "user:user_def"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)
	require.NotEqual(t, result.Grants[0].ID, result.Grants[1].ID)
}

func TestUpsertGrants_RolePrincipalType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "role:project:admin"), Scope: "org:admin", Resource: "*"},
		},
	})
	require.NoError(t, err)

	require.Equal(t, "role:project:admin", result.Grants[0].PrincipalUrn)
	require.Equal(t, "role", result.Grants[0].PrincipalType)
}

// TestUpsertGrants_InvalidPrincipalURN verifies that an invalid URN (zero-value
// Principal) is rejected by the database layer. URN format validation now
// happens during JSON deserialization at the HTTP layer via
// urn.Principal.UnmarshalJSON, so this test exercises the service with a
// zero-value Principal to confirm the database rejects it.
func TestUpsertGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	_, err = ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		SessionToken: nil,
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: urn.Principal{}, Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestUpsertGrants_FailsOnFirstInvalidURNInBatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	_, err = ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:valid"), Scope: "build:read", Resource: "*"},
			{PrincipalUrn: urn.Principal{}, Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	// Verify the first valid grant was rolled back (atomic batch)
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Grants)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestUpsertGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	_, err = ti.service.UpsertGrants(t.Context(), &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestUpsertGrants_UnauthorizedWithoutActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  "",
		UserID:                authCtx.UserID,
		ExternalUserID:        authCtx.ExternalUserID,
		APIKeyID:              authCtx.APIKeyID,
		SessionID:             authCtx.SessionID,
		ProjectID:             authCtx.ProjectID,
		OrganizationSlug:      authCtx.OrganizationSlug,
		Email:                 authCtx.Email,
		AccountType:           authCtx.AccountType,
		HasActiveSubscription: authCtx.HasActiveSubscription,
		ProjectSlug:           authCtx.ProjectSlug,
		APIKeyScopes:          authCtx.APIKeyScopes,
	})

	_, err = ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"}},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestUpsertGrants_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessGrantUpsert), record.Action)
	require.Equal(t, "access_grant", record.SubjectType)
	require.Equal(t, result.Grants[0].PrincipalUrn, record.SubjectDisplay)
	require.Nil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	require.Nil(t, record.Metadata)
	require.Contains(t, string(record.AfterSnapshot), result.Grants[0].PrincipalUrn)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpsertGrants_AuditLogForExistingGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	grant := upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)

	result, err := ti.service.UpsertGrants(ctx, &gen.UpsertGrantsPayload{
		Grants: []*gen.GrantEntry{{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"}},
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, grant.PrincipalUrn, record.SubjectDisplay)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)
	require.Contains(t, string(record.BeforeSnapshot), "build:read")
	require.Contains(t, string(record.AfterSnapshot), "build:read")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
