package adminmcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func newOrganizationAccessWriter(f writeFixture, operation WriteOperation) *organizationAccessWriter {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{operation: true}} //nolint:exhaustive // Only the tested access operation is enabled.
	return &organizationAccessWriter{runner: f.runner, audit: audit.NewLogger(), writes: writes, operation: operation}
}

func organizationAccessState(t *testing.T, f writeFixture, organizationID string) admin.OrganizationAccessState {
	t.Helper()
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the shared access mutator's SQLc queries
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	state, err := admin.LockOrganizationAccessTx(t.Context(), tx, organizationID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return state
}

func TestOrganizationAccessDisablesExactTarget(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_organization_access")
	writer := newOrganizationAccessWriter(f, OperationDisableOrganization)
	ctx := writeContext(t, f)

	_, err := writer.apply(ctx, OrganizationAccessInput{OrganizationID: "synthetic-a"})
	require.ErrorIs(t, err, ErrStaleState, "a slug is not a canonical target")
	_, err = writer.apply(ctx, OrganizationAccessInput{OrganizationID: ""})
	require.Error(t, err)

	receipt, err := writer.apply(ctx, OrganizationAccessInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, f.orgA, receipt.OrganizationID)
	require.Equal(t, "Synthetic A", receipt.Name)
	require.True(t, receipt.EnabledBefore)
	require.False(t, receipt.EnabledAfter)
	require.NotNil(t, receipt.DisabledAt)
	require.Equal(t, organizationAccessSideEffects, receipt.SideEffects)
	require.NotNil(t, organizationAccessState(t, f, f.orgA).DisabledAt)
	require.Nil(t, organizationAccessState(t, f, f.orgB).DisabledAt, "a write cannot change an organization outside its exact target")

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationDisabled)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
	require.Equal(t, "test-client", *entry.ActingClientID)

	_, err = writer.apply(ctx, OrganizationAccessInput{OrganizationID: f.orgA})
	require.ErrorContains(t, err, "nothing would change", "a retry after success changes nothing")
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationDisabled))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationDisableOrganization, f.orgA, writeEventExecuted))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationDisableOrganization, f.orgA, writeEventFailed))
}

func TestOrganizationEnableClearsDisabledAt(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_organization_enable")
	_, err := repo.New(f.db).AdminDisableOrganization(t.Context(), f.orgA)
	require.NoError(t, err)
	writer := newOrganizationAccessWriter(f, OperationEnableOrganization)
	receipt, err := writer.apply(writeContext(t, f), OrganizationAccessInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.False(t, receipt.EnabledBefore)
	require.True(t, receipt.EnabledAfter)
	require.Nil(t, organizationAccessState(t, f, f.orgA).DisabledAt)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnabled))
}

func TestOrganizationAccessAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_organization_access_rollback")
	writer := newOrganizationAccessWriter(f, OperationDisableOrganization)
	ctx := writeContext(t, f)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationDisabled))
	_, err := writer.apply(ctx, OrganizationAccessInput{OrganizationID: f.orgA})
	require.Error(t, err)
	require.Nil(t, organizationAccessState(t, f, f.orgA).DisabledAt, "audit failure must roll back access state")
	require.Zero(t, countWriteEvents(t, f.db, OperationDisableOrganization, f.orgA, writeEventExecuted))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationDisableOrganization, f.orgA, writeEventFailed))
}
