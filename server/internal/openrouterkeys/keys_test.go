package openrouterkeys_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin_open_router_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

func TestListKeys_RequiresPlatformAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Not-found rather than forbidden so a non-admin probe cannot confirm the
	// admin surface exists.
	_, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListKeys_ReturnsSeededKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "list", "chat", "sk-or-list")

	res, err := ti.service.ListKeys(adminCtx, &gen.ListKeysPayload{SessionToken: nil})
	require.NoError(t, err)

	var found *gen.AdminOpenRouterKey
	for _, key := range res.Keys {
		if key.OrganizationID == orgID {
			found = key
		}
	}
	require.NotNil(t, found)
	require.Equal(t, "chat", found.KeyType)
	require.Equal(t, int64(5), found.MonthlyCredits)
	require.False(t, found.Disabled)
}

func TestGetKeyUsage_DecryptsStoredCiphertext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "usage", "chat", "sk-or-usage")

	ti.provisioner.usage = 3.21
	limit := int64(5)
	ti.provisioner.usageLimit = &limit

	res, err := ti.service.GetKeyUsage(adminCtx, &gen.GetKeyUsagePayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.InDelta(t, 3.21, res.CreditsUsed, 0.001)
	require.Equal(t, int64(5), res.MonthlyCredits)
	require.NotNil(t, res.UpstreamLimit)
	require.Equal(t, int64(5), *res.UpstreamLimit)

	// The upstream call must use the decrypted key material.
	require.Equal(t, []string{"sk-or-usage"}, ti.provisioner.UsageCalls())
}

func TestGetKeyUsage_MissingCiphertextErrors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "nomaterial", "chat", "")

	_, err := ti.service.GetKeyUsage(adminCtx, &gen.GetKeyUsagePayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Empty(t, ti.provisioner.UsageCalls())
}

func TestGetKeyUsage_DisabledKeyRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "disabledusage", "chat", "sk-or-disabled")
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	}))

	_, err := ti.service.GetKeyUsage(adminCtx, &gen.GetKeyUsagePayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Empty(t, ti.provisioner.UsageCalls())
}

func TestDisableKey_MarksDisabledAndAudits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "disable", "chat", "sk-or-disable")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyDisable)
	require.NoError(t, err)

	view, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.True(t, view.Disabled)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyDisable)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestEnableKey_ReinstatesWithRecordedLimit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "enable", "chat", "sk-or-enable")
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	}))

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyEnable)
	require.NoError(t, err)

	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.False(t, view.Disabled)
	require.Equal(t, int64(5), view.MonthlyCredits, "recorded ceiling must be kept on reinstatement")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyEnable)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestEnableKey_AlreadyEnabledSkipsUpstream(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "noopenable", "chat", "sk-or-noop")

	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.False(t, view.Disabled)
	require.Empty(t, ti.provisioner.refreshCalls)
}
