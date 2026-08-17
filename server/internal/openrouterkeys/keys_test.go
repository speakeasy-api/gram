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

func TestListKeys_ReportsEncryptionStatus(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	ciphertext, err := ti.enc.Encrypt([]byte("sk-or-dual"))
	require.NoError(t, err)
	ciphertextOnly, err := ti.enc.Encrypt([]byte("sk-or-scrubbed"))
	require.NoError(t, err)

	plainOrg := seedKey(t, ctx, ti, "plain", "chat", "sk-or-plain", "")
	dualOrg := seedKey(t, ctx, ti, "dual", "chat", "sk-or-dual", ciphertext)
	encOrg := seedKey(t, ctx, ti, "enc", "chat", "", ciphertextOnly)

	res, err := ti.service.ListKeys(adminCtx, &gen.ListKeysPayload{SessionToken: nil})
	require.NoError(t, err)

	statusByOrg := map[string]string{}
	for _, key := range res.Keys {
		statusByOrg[key.OrganizationID] = key.EncryptionStatus
		require.Equal(t, "chat", key.KeyType)
	}
	require.Equal(t, "plaintext", statusByOrg[plainOrg])
	require.Equal(t, "encrypted_with_plaintext", statusByOrg[dualOrg])
	require.Equal(t, "encrypted", statusByOrg[encOrg])
}

func TestEncryptKey_ScrubsPlaintext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "scrub", "chat", "sk-or-scrub-me", "")

	view, err := ti.service.EncryptKey(adminCtx, &gen.EncryptKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.Equal(t, "encrypted", view.EncryptionStatus)

	row, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.False(t, row.Key.Valid, "plaintext column must be cleared")
	require.True(t, row.KeyEncrypted.Valid)
	decrypted, err := ti.enc.Decrypt(row.KeyEncrypted.String)
	require.NoError(t, err)
	require.Equal(t, "sk-or-scrub-me", decrypted)
}

func TestEncryptKey_PrefersExistingCiphertext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	ciphertext, err := ti.enc.Encrypt([]byte("sk-or-dual-scrub"))
	require.NoError(t, err)
	orgID := seedKey(t, ctx, ti, "dualscrub", "chat", "sk-or-dual-scrub", ciphertext)

	view, err := ti.service.EncryptKey(adminCtx, &gen.EncryptKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.Equal(t, "encrypted", view.EncryptionStatus)

	row, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.False(t, row.Key.Valid)
	require.Equal(t, ciphertext, row.KeyEncrypted.String, "existing ciphertext must be kept, not re-minted")
}

func TestEncryptKey_MismatchedCiphertextRefusesScrub(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	// The stored ciphertext decrypts fine but to a different key than the
	// plaintext column: the scrub must refuse rather than destroy the only
	// trustworthy copy.
	wrongCiphertext, err := ti.enc.Encrypt([]byte("sk-or-something-else"))
	require.NoError(t, err)
	orgID := seedKey(t, ctx, ti, "mismatch", "chat", "sk-or-truth", wrongCiphertext)

	_, err = ti.service.EncryptKey(adminCtx, &gen.EncryptKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	requireOopsCode(t, err, oops.CodeUnexpected)

	row, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.True(t, row.Key.Valid, "plaintext must survive a refused scrub")
}

func TestEncryptKey_IdempotentOnScrubbedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "idem", "chat", "sk-or-idem", "")

	_, err := ti.service.EncryptKey(adminCtx, &gen.EncryptKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)

	firstRow, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)

	view, err := ti.service.EncryptKey(adminCtx, &gen.EncryptKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.Equal(t, "encrypted", view.EncryptionStatus)

	secondRow, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.Equal(t, firstRow.KeyEncrypted.String, secondRow.KeyEncrypted.String, "a no-op scrub must not re-mint the ciphertext")
}

func TestEncryptKey_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	_ = ctx

	_, err := ti.service.EncryptKey(adminCtx, &gen.EncryptKeyPayload{
		SessionToken:   nil,
		OrganizationID: "org-missing",
		KeyType:        "chat",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetKeyUsage_DecryptsPreferredCiphertext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	ciphertext, err := ti.enc.Encrypt([]byte("sk-or-usage"))
	require.NoError(t, err)
	orgID := seedKey(t, ctx, ti, "usage", "chat", "sk-or-stale-plain", ciphertext)

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

	// The upstream call must use the decrypted ciphertext, not the stale
	// plaintext column.
	require.Equal(t, []string{"sk-or-usage"}, ti.provisioner.UsageCalls())
}

func TestGetKeyUsage_DisabledKeyRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "disabledusage", "chat", "sk-or-disabled", "")
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

	orgID := seedKey(t, ctx, ti, "disable", "chat", "sk-or-disable", "")

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

	orgID := seedKey(t, ctx, ti, "enable", "chat", "sk-or-enable", "")
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

	orgID := seedKey(t, ctx, ti, "noopenable", "chat", "sk-or-noop", "")

	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        "chat",
	})
	require.NoError(t, err)
	require.False(t, view.Disabled)
	require.Empty(t, ti.provisioner.refreshCalls)
}
