package openrouterkeys_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin_open_router_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgmetarepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orgrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
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
	recordedLimit := 7
	require.NoError(t, orgrepo.New(ti.conn).UpdateOpenRouterKeyMonthlyCredits(ctx, orgrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
		MonthlyCredits: int64(recordedLimit),
	}))
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
	require.EqualValues(t, recordedLimit, view.MonthlyCredits, "recorded ceiling must be kept on reinstatement")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyEnable)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestEnableKey_ReinstatesLegacyZeroSecurityKeyAtPaygPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "enablelegacyzero", "internal", "sk-or-enable-legacy-zero", "")
	require.NoError(t, orgmetarepo.New(ti.conn).SetAccountType(ctx, orgmetarepo.SetAccountTypeParams{
		GramAccountType: string(billing.TierPayg),
		ID:              orgID,
	}))
	require.NoError(t, orgrepo.New(ti.conn).UpdateOpenRouterKeyMonthlyCredits(ctx, orgrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
		MonthlyCredits: 0,
	}))
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	}))

	expected, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	require.True(t, ok)
	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	require.False(t, view.Disabled)
	require.EqualValues(t, expected, view.MonthlyCredits)
	require.Equal(t, []string{orgID + "/" + string(openrouter.KeyTypeInternal)}, ti.provisioner.refreshCalls)
}

func TestEnableKey_ReinstatesLegacyZeroTrialKeyAtTrialPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "enabletrialzero", "internal", "sk-or-enable-trial-zero", "")
	require.NoError(t, orgmetarepo.New(ti.conn).SetAccountType(ctx, orgmetarepo.SetAccountTypeParams{
		GramAccountType: string(billing.TierEnterprise),
		ID:              orgID,
	}))
	require.NoError(t, trialsrepo.New(ti.conn).CreateTrial(ctx, trialsrepo.CreateTrialParams{
		OrganizationID: orgID,
		Tier:           string(billing.TierEnterprise),
		EndsAt:         conv.ToPGTimestamptz(time.Now().UTC().Add(14 * 24 * time.Hour)),
	}))
	require.NoError(t, orgrepo.New(ti.conn).UpdateOpenRouterKeyMonthlyCredits(ctx, orgrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
		MonthlyCredits: 0,
	}))
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	}))

	expected, ok := openrouter.DefaultCreditLimit(orgID, billing.TierEnterprise, true)
	require.True(t, ok)
	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	require.NoError(t, err)
	require.False(t, view.Disabled)
	require.EqualValues(t, expected, view.MonthlyCredits)
}

func TestEnableKeyWaitsForPerKeyBillingLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "enablelocked", "internal", "sk-or-enable-locked", "")
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	}))

	lockConn, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer lockConn.Release()
	lockQueries := activitiesrepo.New(lockConn)
	lockParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
		KeyType:        string(openrouter.KeyTypeInternal),
		OrganizationID: orgID,
	}
	require.NoError(t, lockQueries.AcquireOpenRouterKeyBillingLock(ctx, lockParams))
	lockHeld := true
	t.Cleanup(func() {
		if !lockHeld {
			return
		}
		unlocked, releaseErr := lockQueries.ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
		require.NoError(t, releaseErr)
		require.True(t, unlocked)
	})

	result := make(chan error, 1)
	go func() {
		_, enableErr := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
			SessionToken:   nil,
			OrganizationID: orgID,
			KeyType:        string(openrouter.KeyTypeInternal),
		})
		result <- enableErr
	}()
	require.Never(t, func() bool {
		return len(ti.provisioner.RefreshCalls()) > 0
	}, 150*time.Millisecond, 10*time.Millisecond)

	unlocked, err := lockQueries.ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
	require.NoError(t, err)
	require.True(t, unlocked)
	lockHeld = false
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "admin enable did not continue after the billing lock was released")
	}
}

func TestKeyBillingLockAcquireTimeoutDoesNotLeakSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := seedKey(t, ctx, ti, "locktimeout", "internal", "sk-or-lock-timeout", "")
	lockConn, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer lockConn.Release()
	lockQueries := activitiesrepo.New(lockConn)
	lockParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
		KeyType:        string(openrouter.KeyTypeInternal),
		OrganizationID: orgID,
	}
	require.NoError(t, lockQueries.AcquireOpenRouterKeyBillingLock(ctx, lockParams))
	lockHeld := true
	t.Cleanup(func() {
		if !lockHeld {
			return
		}
		unlocked, releaseErr := lockQueries.ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
		require.NoError(t, releaseErr)
		require.True(t, unlocked)
	})

	operationStarted := false
	err = keybillinglock.WithAcquireTimeout(ctx, testenv.NewLogger(t), ti.conn, orgID, openrouter.KeyTypeInternal, 50*time.Millisecond, func(_ *pgxpool.Conn) error {
		operationStarted = true
		return nil
	})
	require.ErrorIs(t, err, keybillinglock.ErrAcquireTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, operationStarted)

	unlocked, err := lockQueries.ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
	require.NoError(t, err)
	require.True(t, unlocked)
	lockHeld = false

	require.NoError(t, keybillinglock.WithAcquireTimeout(ctx, testenv.NewLogger(t), ti.conn, orgID, openrouter.KeyTypeInternal, time.Second, func(_ *pgxpool.Conn) error {
		operationStarted = true
		return nil
	}))
	require.True(t, operationStarted)
}

func TestKeyBillingLockPoolWaitUsesCallerContext(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	conns := make([]*pgxpool.Conn, 0, ti.conn.Config().MaxConns)
	for range ti.conn.Config().MaxConns {
		conn, err := ti.conn.Acquire(ctx)
		require.NoError(t, err)
		conns = append(conns, conn)
	}
	t.Cleanup(func() {
		for _, conn := range conns {
			conn.Release()
		}
	})

	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := keybillinglock.WithAcquireTimeout(shortCtx, testenv.NewLogger(t), ti.conn, "org-pool-wait", openrouter.KeyTypeInternal, time.Second, func(_ *pgxpool.Conn) error {
		require.FailNow(t, "operation started without a pooled connection")
		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, keybillinglock.ErrAcquireTimeout)
}

func TestKeyBillingLockQueryUsesCallerDeadline(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := seedKey(t, ctx, ti, "callerdeadline", "internal", "sk-or-caller-deadline", "")
	lockConn, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer lockConn.Release()
	lockQueries := activitiesrepo.New(lockConn)
	lockParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
		KeyType:        string(openrouter.KeyTypeInternal),
		OrganizationID: orgID,
	}
	require.NoError(t, lockQueries.AcquireOpenRouterKeyBillingLock(ctx, lockParams))
	defer func() {
		unlocked, releaseErr := lockQueries.ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
		require.NoError(t, releaseErr)
		require.True(t, unlocked)
	}()

	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err = keybillinglock.WithAcquireTimeout(shortCtx, testenv.NewLogger(t), ti.conn, orgID, openrouter.KeyTypeInternal, time.Second, func(_ *pgxpool.Conn) error {
		require.FailNow(t, "operation started before the caller deadline")
		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, keybillinglock.ErrAcquireTimeout)
}

func TestEnableKeyReportsLockContentionAsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "enablebusy", "internal", "sk-or-enable-busy", "")
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	}))

	lockConn, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer lockConn.Release()
	lockQueries := activitiesrepo.New(lockConn)
	lockParams := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
		KeyType:        string(openrouter.KeyTypeInternal),
		OrganizationID: orgID,
	}
	require.NoError(t, lockQueries.AcquireOpenRouterKeyBillingLock(ctx, lockParams))
	defer func() {
		unlocked, releaseErr := lockQueries.ReleaseOpenRouterKeyBillingLock(context.WithoutCancel(ctx), activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
		require.NoError(t, releaseErr)
		require.True(t, unlocked)
	}()

	// Keep the caller alive beyond the service's own lock budget so this
	// exercises genuine advisory-lock contention, not caller cancellation.
	lockWaitCtx, cancel := context.WithTimeout(adminCtx, 10*time.Second)
	defer cancel()
	_, err = ti.service.EnableKey(lockWaitCtx, &gen.EnableKeyPayload{
		SessionToken:   nil,
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Empty(t, ti.provisioner.RefreshCalls())
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
