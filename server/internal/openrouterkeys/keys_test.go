package openrouterkeys_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/admin_open_router_keys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgmetarepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orgrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestStubProvisionerDisableCausesMirrorLocalState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := seedKey(t, ctx, ti, "stub-causes", string(openrouter.KeyTypeChat), "sk-or-stub-causes")
	queries := orgrepo.New(ti.conn)
	require.NoError(t, testrepo.New(ti.conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), Disabled: false, DisableCauses: nil,
	}))

	_, err := ti.provisioner.AddAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock)
	require.ErrorContains(t, err, "unclassified")
	_, _, err = ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock, nil)
	require.ErrorContains(t, err, "unclassified")
	change, err := ti.provisioner.AddAPIKeyDisableCause(ctx, "missing-org", openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, openrouter.DisableCauseChange{}, change)
	require.NoError(t, testrepo.New(ti.conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat), Disabled: false, DisableCauses: []string{},
	}))

	change, err = ti.provisioner.AddAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseTrialDemotion)
	require.NoError(t, err)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	row, err := queries.GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
	require.True(t, row.Disabled)

	change, err = ti.provisioner.AddAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseTrialDemotion)
	require.NoError(t, err)
	require.Equal(t, openrouter.DisableCauseChange{}, change)
	change, err = ti.provisioner.AddAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true}, change)

	limit, change, err := ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive, new(99))
	require.NoError(t, err)
	require.Equal(t, 5, limit)
	require.Equal(t, openrouter.DisableCauseChange{}, change)
	limit, change, err = ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseTrialDemotion, new(99))
	require.NoError(t, err)
	require.Equal(t, 5, limit)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true}, change)
	row, err = queries.GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, []string{"admin_lock"}, row.DisableCauses)
	require.EqualValues(t, 5, row.MonthlyCredits)
	require.True(t, row.Disabled)

	limit, change, err = ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock, new(42))
	require.NoError(t, err)
	require.Equal(t, 42, limit)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	row, err = queries.GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, err)
	require.Empty(t, row.DisableCauses)
	require.EqualValues(t, 42, row.MonthlyCredits)
	require.False(t, row.Disabled)

	change, err = ti.provisioner.AddAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive)
	require.NoError(t, err)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	limit, change, err = ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive, nil)
	require.NoError(t, err)
	require.Equal(t, 42, limit)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	row, err = queries.GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(openrouter.KeyTypeChat)})
	require.NoError(t, err)
	require.Empty(t, row.DisableCauses)
	require.EqualValues(t, 42, row.MonthlyCredits)
	require.False(t, row.Disabled)

	_, _, err = ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock, new(0))
	require.ErrorContains(t, err, "must be positive")
	limit, change, err = ti.provisioner.RemoveAPIKeyDisableCause(ctx, "missing-org", openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock, nil)
	require.NoError(t, err)
	require.Zero(t, limit)
	require.Equal(t, openrouter.DisableCauseChange{}, change)
}

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

func TestListKeysUsesEffectiveDisabledCompatibility(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	classifiedEnabled := seedKey(t, ctx, ti, "compat-enabled", "chat", "sk-or-compat-enabled")
	classifiedDisabled := seedKey(t, ctx, ti, "compat-disabled", "internal", "sk-or-compat-disabled")
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: classifiedEnabled, KeyType: string(openrouter.KeyTypeChat), Disabled: true, DisableCauses: []string{}}))
	require.NoError(t, fixtures.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: classifiedDisabled, KeyType: "internal", Disabled: false, DisableCauses: []string{"trial_demotion"}}))

	res, err := ti.service.ListKeys(adminCtx, &gen.ListKeysPayload{SessionToken: nil})
	require.NoError(t, err)
	states := make(map[string]bool)
	for _, key := range res.Keys {
		states[key.OrganizationID] = key.Disabled
	}
	enabled, enabledPresent := states[classifiedEnabled]
	require.True(t, enabledPresent, "classified enabled fixture must be returned")
	require.False(t, enabled)
	disabled, disabledPresent := states[classifiedDisabled]
	require.True(t, disabledPresent, "classified disabled fixture must be returned")
	require.True(t, disabled)
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

func TestEnableKey_RealOpenRouterCompletesOnLockedSession(t *testing.T) {
	t.Parallel()

	type patchRequest struct {
		method string
		path   string
		limit  float64
		err    error
	}
	patches := make(chan patchRequest, 1)
	releasePatch := make(chan struct{})
	var releasePatchOnce sync.Once
	release := func() { releasePatchOnce.Do(func() { close(releasePatch) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Limit float64 `json:"limit"`
		}
		err := json.NewDecoder(r.Body).Decode(&body)
		patches <- patchRequest{method: r.Method, path: r.URL.Path, limit: body.Limit, err: err}
		<-releasePatch
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"limit":7,"hash":"hash-enablereal"}}`))
	}))
	t.Cleanup(upstream.Close)
	// Cleanup runs in LIFO order, releasing a blocked handler before httptest
	// waits for active connections to finish.
	t.Cleanup(release)

	ctx, ti := newTestServiceWithProvisioner(t, func(logger *slog.Logger, tracerProvider trace.TracerProvider, conn *pgxpool.Pool, enc *encryption.Client) openrouter.Provisioner {
		policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
		require.NoError(t, err)
		testBaseURL, err := openrouter.WithTestBaseURL(upstream.URL)
		require.NoError(t, err)
		return openrouter.New(logger, tracerProvider, policy, conn, "test", "provisioning-key", nil, nil, nil, enc, testBaseURL)
	})
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "enablereal", "chat", "sk-or-enable-real")
	require.NoError(t, orgrepo.New(ti.conn).UpdateOpenRouterKeyMonthlyCredits(ctx, orgrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
		MonthlyCredits: 7,
	}))
	require.NoError(t, orgrepo.New(ti.conn).DisableOpenRouterAPIKey(ctx, orgrepo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	}))
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyEnable)
	require.NoError(t, err)

	boundedCtx, cancel := context.WithTimeout(adminCtx, time.Second)
	defer cancel()
	type enableResult struct {
		view *gen.AdminOpenRouterKey
		err  error
	}
	results := make(chan enableResult, 1)
	go func() {
		view, err := ti.service.EnableKey(boundedCtx, &gen.EnableKeyPayload{
			OrganizationID: orgID,
			KeyType:        string(openrouter.KeyTypeChat),
		})
		results <- enableResult{view: view, err: err}
	}()

	var patch patchRequest
	select {
	case patch = <-patches:
	case <-time.After(time.Second):
		require.FailNow(t, "OpenRouter PATCH was not intercepted")
	}
	require.NoError(t, patch.err)
	require.Equal(t, http.MethodPatch, patch.method)
	require.Equal(t, "/v1/keys/hash-enablereal", patch.path)
	require.InDelta(t, 7, patch.limit, 0)
	release()

	var result enableResult
	select {
	case result = <-results:
	case <-time.After(time.Second):
		require.FailNow(t, "admin enable did not complete after the PATCH response was released")
	}
	require.NoError(t, result.err)
	require.False(t, result.view.Disabled)
	require.EqualValues(t, 7, result.view.MonthlyCredits)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyEnable)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}

func TestEnableKey_ReinstatesLegacyZeroSecurityKeyAtPaygPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	orgID := seedKey(t, ctx, ti, "enablelegacyzero", "internal", "sk-or-enable-legacy-zero")
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

	orgID := seedKey(t, ctx, ti, "enabletrialzero", "internal", "sk-or-enable-trial-zero")
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
	orgID := seedKey(t, ctx, ti, "enablelocked", "internal", "sk-or-enable-locked")
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
	acquiredBeforeEnable := ti.conn.Stat().AcquiredConns()
	go func() {
		_, enableErr := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{
			SessionToken:   nil,
			OrganizationID: orgID,
			KeyType:        string(openrouter.KeyTypeInternal),
		})
		result <- enableErr
	}()
	require.Eventually(t, func() bool {
		// EnableKey holds its acquired session while pg_advisory_lock waits on
		// lockConn. Seeing the additional held connection proves the operation
		// reached the contested lock before the negative assertion starts.
		return ti.conn.Stat().AcquiredConns() > acquiredBeforeEnable
	}, 5*time.Second, 10*time.Millisecond)
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
	orgID := seedKey(t, ctx, ti, "locktimeout", "internal", "sk-or-lock-timeout")
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
	orgID := seedKey(t, ctx, ti, "callerdeadline", "internal", "sk-or-caller-deadline")
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
	orgID := seedKey(t, ctx, ti, "enablebusy", "internal", "sk-or-enable-busy")
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
