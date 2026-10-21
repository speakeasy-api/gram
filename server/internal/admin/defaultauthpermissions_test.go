package admin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDefaultAuthPermissions_GrantsAnyOrganizationIdempotently(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	// No server signs in through a user session issuer, so the upstream OAuth
	// migration never lists this organization.
	org := newMigrationOrg(t, ctx, db, "defaults-only")

	before, err := svc.GetDefaultAuthPermissionStatus(ctx, &gen.GetDefaultAuthPermissionStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.False(t, before.Complete)
	require.Equal(t, int64(1), before.PendingGrants)
	pending := map[string]int64{}
	for _, kind := range before.Kinds {
		pending[kind.Kind] = kind.Pending
	}
	require.Equal(t, map[string]int64{"personal_default": 1, "shared_client": 0, "api_key": 0}, pending)

	run, err := svc.RunDefaultAuthPermissions(ctx, &gen.RunDefaultAuthPermissionsPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, int64(1), run.GrantsWritten)
	require.True(t, run.Status.Complete)

	again, err := svc.RunDefaultAuthPermissions(ctx, &gen.RunDefaultAuthPermissionsPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, again.GrantsWritten)
}

func TestDefaultAuthPermissions_RunRequiresStaff(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	org := newMigrationOrg(t, ctx, db, "defaults-unauthorized")

	_, err := svc.RunDefaultAuthPermissions(ctx, &gen.RunDefaultAuthPermissionsPayload{OrganizationID: org.id})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
}

func TestDefaultAuthPermissions_ReportsARunInProgress(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	org := newMigrationOrg(t, ctx, db, "defaults-in-progress")

	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary only; the lock uses a generated SQLc method.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, authclientbackfill.New(tx).LockOrganizationBackfill(ctx, org.id))

	status, err := svc.GetDefaultAuthPermissionStatus(ctx, &gen.GetDefaultAuthPermissionStatusPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.True(t, status.InProgress)

	_, err = svc.RunDefaultAuthPermissions(ctx, &gen.RunDefaultAuthPermissionsPayload{OrganizationID: org.id})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
}
