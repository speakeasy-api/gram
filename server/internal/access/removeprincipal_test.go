package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRemovePrincipalGrants_RemovesAllForPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)

	userURN := "user:user_abc"

	// Create multiple grants for the same principal
	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, userURN, "mcp:connect", "*")

	// Create a grant for a different principal
	upsertGrant(t, ctx, ti.service, "role:admin", "org:admin", "*")

	// Remove all grants for user:user_abc
	err = ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: mustParsePrincipal(t, userURN),
	})
	require.NoError(t, err)

	// Verify user:user_abc grants are gone
	result, err := ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(userURN),
	})
	require.NoError(t, err)
	require.Empty(t, result.Grants)

	// Verify role:admin grant still exists
	roleUrn := "role:admin"
	result, err = ti.service.ListGrants(ctx, &gen.ListGrantsPayload{
		PrincipalUrn: conv.PtrEmpty(roleUrn),
	})
	require.NoError(t, err)
	require.Len(t, result.Grants, 1)
	require.Equal(t, "role:admin", result.Grants[0].PrincipalUrn)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
}

func TestRemovePrincipalGrants_NoOpWhenPrincipalHasNoGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)

	err = ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: mustParsePrincipal(t, "user:nonexistent"),
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

// TestRemovePrincipalGrants_InvalidPrincipalURN verifies that a zero-value
// Principal (invalid URN) is rejected. URN format validation now happens
// during JSON deserialization at the HTTP layer via urn.Principal.UnmarshalJSON.
func TestRemovePrincipalGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)

	err = ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: urn.Principal{},
	})
	require.Error(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestRemovePrincipalGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)

	err = ti.service.RemovePrincipalGrants(t.Context(), &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: mustParsePrincipal(t, "user:user_abc"),
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestRemovePrincipalGrants_UnauthorizedWithoutActiveOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
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

	err = ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: mustParsePrincipal(t, "user:user_abc"),
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestRemovePrincipalGrants_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)

	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")
	upsertGrant(t, ctx, ti.service, "user:user_abc", "mcp:connect", "*")

	err = ti.service.RemovePrincipalGrants(ctx, &gen.RemovePrincipalGrantsPayload{
		PrincipalUrn: mustParsePrincipal(t, "user:user_abc"),
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessGrantRemovePrincipal), record.Action)
	require.Equal(t, "access_grant", record.SubjectType)
	require.Equal(t, "user:user_abc", record.SubjectDisplay)
	require.NotNil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	require.Nil(t, record.Metadata)
	require.Contains(t, string(record.BeforeSnapshot), "mcp:connect")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemovePrincipal)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
}
