package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRemoveGrants_RemovesSingleGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)

	userURN := "user:user_abc"

	// Create two grants for the same principal
	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, userURN, "mcp:connect", "*")

	// Remove only the build:read grant
	err = ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
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

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestRemoveGrants_BatchRemovesMultipleGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)

	userURN := "user:user_abc"

	upsertGrant(t, ctx, ti.service, userURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, userURN, "mcp:connect", "*")
	upsertGrant(t, ctx, ti.service, userURN, "org:admin", "*")

	// Remove two of three grants in a single batch call
	err = ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
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

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
}

func TestRemoveGrants_DoesNotAffectOtherPrincipals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	abcURN := "user:user_abc"
	defURN := "user:user_def"

	// Create identical scopes for different principals
	upsertGrant(t, ctx, ti.service, abcURN, "build:read", "*")
	upsertGrant(t, ctx, ti.service, defURN, "build:read", "*")

	// Remove only user_abc's grant
	err := ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
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
		Grants: []*gen.GrantEntry{
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
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)

	// Removing a grant that doesn't exist is a silent no-op (batch semantics)
	err = ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

// TestRemoveGrants_InvalidPrincipalURN verifies that a zero-value Principal
// (invalid URN) is rejected. URN format validation now happens during JSON
// deserialization at the HTTP layer via urn.Principal.UnmarshalJSON.
func TestRemoveGrants_InvalidPrincipalURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)

	err = ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: urn.Principal{}, Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestRemoveGrants_UnauthorizedWithoutAuthContext(t *testing.T) {
	t.Parallel()

	_, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)

	err = ti.service.RemoveGrants(t.Context(), &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestRemoveGrants_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)

	upsertGrant(t, ctx, ti.service, "user:user_abc", "build:read", "*")

	err = ti.service.RemoveGrants(ctx, &gen.RemoveGrantsPayload{
		Grants: []*gen.GrantEntry{
			{PrincipalUrn: mustParsePrincipal(t, "user:user_abc"), Scope: "build:read", Resource: "*"},
		},
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAccessGrantRemove), record.Action)
	require.Equal(t, "access_grant", record.SubjectType)
	require.Equal(t, "user:user_abc", record.SubjectDisplay)
	require.NotNil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	require.Nil(t, record.Metadata)
	require.Contains(t, string(record.BeforeSnapshot), "user:user_abc")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessGrantRemove)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
