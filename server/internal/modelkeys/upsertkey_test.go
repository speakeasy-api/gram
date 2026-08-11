package modelkeys_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/model_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func newUpsertPayload(slot string, opts func(*gen.UpsertKeyPayload)) *gen.UpsertKeyPayload {
	payload := &gen.UpsertKeyPayload{
		Slot:             slot,
		Provider:         modelkeys.ProviderOpenRouter,
		APIKey:           "sk-or-test-key",
		Enabled:          true,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}

	if opts != nil {
		opts(payload)
	}

	return payload
}

func TestUpsertKey_CreatesKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionModelProviderKeyUpsert)
	require.NoError(t, err)

	key, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.Equal(t, modelkeys.SlotDefault, key.Slot)
	require.Equal(t, modelkeys.ProviderOpenRouter, key.Provider)
	require.Equal(t, authCtx.ProjectID.String(), key.ProjectID)
	require.True(t, key.Enabled)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionModelProviderKeyUpsert)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestUpsertKey_ReplacesExistingSlotKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	first, err := ti.service.UpsertKey(ctx, newUpsertPayload("assistants", nil))
	require.NoError(t, err)

	second, err := ti.service.UpsertKey(ctx, newUpsertPayload("assistants", func(p *gen.UpsertKeyPayload) {
		p.APIKey = "sk-or-replacement-key"
	}))
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)

	list, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, list.Keys, 1)
	require.Equal(t, second.ID, list.Keys[0].ID)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionModelProviderKeyUpsert)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "assistants", beforeSnapshot["slot"])
}

func TestUpsertKey_RejectsUnknownSlot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload("not-a-slot", nil))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpsertKey_RejectsRiskAnalysisSlot(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	// Risk-policy analysis stays on the platform's internal key until the
	// dedicated risk/PI BYOK slots ship.
	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(string(billing.ModelUsageSourceRiskAnalysis), nil))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpsertKey_RejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, func(p *gen.UpsertKeyPayload) {
		p.Provider = "anthropic"
	}))
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestUpsertKey_RejectsKeyTheProviderRejects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	ti.provisioner.usageErr = errors.New("401 unauthorized")

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpsertKey_RequiresProductFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithRedisDB(t, 1)

	_, err := ti.service.UpsertKey(ctx, newUpsertPayload(modelkeys.SlotDefault, nil))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpsertKey_RequiresProjectWriteScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	enableCustomModelKeys(t, ctx, ti.conn)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	_, err := ti.service.UpsertKey(readOnlyCtx, newUpsertPayload(modelkeys.SlotDefault, nil))
	requireOopsCode(t, err, oops.CodeForbidden)
}
