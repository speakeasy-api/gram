package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListGrants_EmptyWhenNoGrantsExist(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Grants)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestListGrants_ReturnsAllGrantsForOrg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "role:admin", "org:admin", "*")

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 2)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestListGrants_FiltersByPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	userURN := "user:user_abc"

	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, "role:admin", "org:admin", "*")

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		SessionToken: nil,
		PrincipalUrn: conv.PtrEmpty(userURN),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, userURN, result.Grants[0].PrincipalUrn)
	require.Equal(t, "user", result.Grants[0].PrincipalType)
	require.Equal(t, "build:read", result.Grants[0].Scope)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestListGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCount(t.Context(), ti.conn)
	require.NoError(t, err)

	_, err = ti.service.ListGrants(t.Context(), &gen.ListGrantsPayload{})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCount(t.Context(), ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestListGrants_UnauthorizedWithoutActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
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

	_, err = ti.service.ListGrants(ctx, &gen.ListGrantsPayload{})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}
