package modelkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/model_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	modelkeysrepo "github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func newSetKeyEnabledPayload(id string, enabled bool) *gen.SetKeyEnabledPayload {
	return &gen.SetKeyEnabledPayload{
		ID:               id,
		Enabled:          enabled,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func TestSetKeyEnabled_DisablesKeyInPlaceAndAuditsChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)
	require.Equal(t, 1, ti.provisioner.usageCalls)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	beforeRows, err := modelkeysrepo.New(ti.conn).ListKeysByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, beforeRows, 1)

	updated, err := ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(created.ID, false))
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.False(t, updated.Enabled)
	require.Equal(t, 1, ti.provisioner.usageCalls)
	afterRows, err := modelkeysrepo.New(ti.conn).ListKeysByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, afterRows, 1)
	require.True(t, afterRows[0].UpdatedAt.Time.After(beforeRows[0].UpdatedAt.Time))

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionModelProviderKeyUpsert)
	require.NoError(t, err)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, true, beforeSnapshot["enabled"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, false, afterSnapshot["enabled"])
}

func TestSetKeyEnabled_ReenablesKeyInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, func(payload *gen.UpsertKeyPayload) {
		payload.Enabled = false
	}))
	require.NoError(t, err)

	updated, err := ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(created.ID, true))
	require.NoError(t, err)
	require.Equal(t, created.ID, updated.ID)
	require.True(t, updated.Enabled)
	require.Equal(t, 1, ti.provisioner.usageCalls)
}

func TestSetKeyEnabled_DisableAllowedWithoutProductFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithRedisDB(t, 2)
	enableCustomModelKeys(t, ctx, ti.conn)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)
	disableCustomModelKeys(t, ctx, ti)

	updated, err := ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(created.ID, false))
	require.NoError(t, err)
	require.False(t, updated.Enabled)

	_, err = ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(created.ID, true))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSetKeyEnabled_UnknownKeyReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithRedisDB(t, 3)

	_, err := ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(uuid.NewString(), true))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetKeyEnabled_DeletedKeyReturnsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithRedisDB(t, 4)
	enableCustomModelKeys(t, ctx, ti.conn)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteKey(ctx, &gen.DeleteKeyPayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	disableCustomModelKeys(t, ctx, ti)

	_, err = ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(created.ID, true))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSetKeyEnabled_UnchangedStateIsNoOp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, func(payload *gen.UpsertKeyPayload) {
		payload.Enabled = false
	}))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	beforeRows, err := modelkeysrepo.New(ti.conn).ListKeysByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	beforeAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionModelProviderKeyUpsert)
	require.NoError(t, err)

	unchanged, err := ti.service.SetKeyEnabled(ctx, newSetKeyEnabledPayload(created.ID, false))
	require.NoError(t, err)
	require.Equal(t, created.ID, unchanged.ID)
	require.False(t, unchanged.Enabled)

	afterRows, err := modelkeysrepo.New(ti.conn).ListKeysByProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Equal(t, beforeRows[0].UpdatedAt, afterRows[0].UpdatedAt)
	afterAuditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionModelProviderKeyUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount, afterAuditCount)
}

func TestSetKeyEnabled_RequiresProjectWriteScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	created, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	_, err = ti.service.SetKeyEnabled(readOnlyCtx, newSetKeyEnabledPayload(created.ID, false))
	requireOopsCode(t, err, oops.CodeForbidden)
}
