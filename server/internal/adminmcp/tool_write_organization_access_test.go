package adminmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func newOrganizationAccessWriter(f writeFixture, operation WriteOperation) *organizationAccessWriter {
	writes := WriteConfig{Enabled: true}
	return &organizationAccessWriter{runner: f.runner, audit: audit.NewLogger(), writes: writes, operation: operation, keys: nil, logger: nil}
}

// recordingInferenceKeys captures the key calls an access change makes and can
// be made to fail them.
type recordingInferenceKeys struct {
	added   []openrouter.KeyType
	removed []openrouter.KeyType
	fail    error
}

func (r *recordingInferenceKeys) AddAPIKeyDisableCause(_ context.Context, _ string, keyType openrouter.KeyType, cause openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	if cause == openrouter.DisableCauseOrganizationDisabled {
		r.added = append(r.added, keyType)
	}
	return openrouter.DisableCauseChange{CauseChanged: r.fail == nil, KeyAccessChanged: r.fail == nil}, r.fail
}

func (r *recordingInferenceKeys) RemoveAPIKeyDisableCause(_ context.Context, _ string, keyType openrouter.KeyType, cause openrouter.DisableCause, _ *int) (int, openrouter.DisableCauseChange, error) {
	if cause == openrouter.DisableCauseOrganizationDisabled {
		r.removed = append(r.removed, keyType)
	}
	return 0, openrouter.DisableCauseChange{CauseChanged: r.fail == nil, KeyAccessChanged: r.fail == nil}, r.fail
}

func TestOrganizationAccessLocksAndReleasesInferenceKeys(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_organization_access_keys")
	keys := &recordingInferenceKeys{added: nil, removed: nil, fail: nil}
	ctx := writeContext(t, f)

	disable := newOrganizationAccessWriter(f, OperationDisableOrganization)
	disable.keys, disable.logger = keys, testenv.NewLogger(t)
	receipt, err := disable.apply(ctx, OrganizationAccessInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, inferenceKeysSynced, receipt.InferenceKeys)
	require.Equal(t, openrouter.AllKeyTypes, keys.added)
	require.Empty(t, keys.removed)

	enable := newOrganizationAccessWriter(f, OperationEnableOrganization)
	enable.keys, enable.logger = keys, testenv.NewLogger(t)
	receipt, err = enable.apply(ctx, OrganizationAccessInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, inferenceKeysSynced, receipt.InferenceKeys)
	require.Equal(t, openrouter.AllKeyTypes, keys.removed)
}

func TestOrganizationAccessDisableStandsWhenInferenceKeysFail(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_organization_access_keys_fail")
	keys := &recordingInferenceKeys{added: nil, removed: nil, fail: errors.New("openrouter is down")}
	writer := newOrganizationAccessWriter(f, OperationDisableOrganization)
	writer.keys, writer.logger = keys, testenv.NewLogger(t)

	receipt, err := writer.apply(writeContext(t, f), OrganizationAccessInput{OrganizationID: f.orgA})
	require.NoError(t, err, "a key failure must not fail the disable")
	require.Equal(t, inferenceKeysFailed, receipt.InferenceKeys)
	require.Equal(t, openrouter.AllKeyTypes, keys.added, "every key type is attempted even after one fails")
	require.NotNil(t, organizationAccessState(t, f, f.orgA).DisabledAt)
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
