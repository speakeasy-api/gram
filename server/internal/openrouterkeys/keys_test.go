package openrouterkeys_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
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
	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
	orgmetarepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
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

func TestStubProvisionerRemoveLastDisableCauseRestoresOrganizationDefault(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := seedKey(t, ctx, ti, "stub-default-limit", string(openrouter.KeyTypeChat), "sk-or-stub-default-limit")
	require.NoError(t, orgmetarepo.New(ti.conn).SetAccountType(ctx, orgmetarepo.SetAccountTypeParams{
		GramAccountType: string(billing.TierPayg),
		ID:              orgID,
	}))
	require.NoError(t, orgrepo.New(ti.conn).UpdateOpenRouterKeyMonthlyCredits(ctx, orgrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
		MonthlyCredits: 0,
	}))
	require.NoError(t, testrepo.New(ti.conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
		Disabled:       true,
		DisableCauses:  []string{string(openrouter.DisableCauseAdminLock)},
	}))

	expected, ok := openrouter.ResolveDefaultCreditLimit(ctx, testenv.NewLogger(t), ti.conn, orgID, billing.TierPayg)
	require.True(t, ok)
	limit, change, err := ti.provisioner.RemoveAPIKeyDisableCause(ctx, orgID, openrouter.KeyTypeChat, openrouter.DisableCauseAdminLock, nil)
	require.NoError(t, err)
	require.Equal(t, expected, limit)
	require.Equal(t, openrouter.DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)

	row, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	require.NoError(t, err)
	require.EqualValues(t, expected, row.MonthlyCredits)
	require.Empty(t, row.DisableCauses)
	require.False(t, row.Disabled)
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
	setDisableCauses(t, ctx, ti, orgID, "chat", []string{"admin_lock"})

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
	setDisableCauses(t, ctx, ti, orgID, string(openrouter.KeyTypeChat), []string{"admin_lock"})
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
	require.InDelta(t, 7, patch.limit, 0, "reconciliation reapplies the durable local limit")

	// The durable local intent and its audit/outbox record must commit before
	// reconciliation waits on OpenRouter. A crash or ambiguous HTTP result can
	// then be retried without losing the requested admin state.
	require.Empty(t, readDisableCauses(t, ctx, ti, orgID, string(openrouter.KeyTypeChat)))
	committedAudit, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOpenRouterAPIKeyEnable)
	require.NoError(t, err)
	require.Equal(t, before+1, committedAudit)
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

func TestEnableKey_ReinstatesZeroSecurityKeyAtPaygPolicy(t *testing.T) {
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
	setDisableCauses(t, ctx, ti, orgID, string(openrouter.KeyTypeInternal), []string{"admin_lock"})

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
	require.Empty(t, view.DisableCauses)
}

func TestEnableKey_ReinstatesZeroTrialKeyAtTrialPolicy(t *testing.T) {
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
	setDisableCauses(t, ctx, ti, orgID, string(openrouter.KeyTypeInternal), []string{"admin_lock"})

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

func TestAdminMutationDeadlineIncludesDurableBegin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithAdminMutationTimeout(t, 50*time.Millisecond)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-begin-deadline", "chat", "sk-or-admin-begin-deadline")
	requested := make(chan struct{})
	release := make(chan struct{})
	ti.coordinator.begin = func(context.Context, openrouterkeys.AdminReconciliationScope) error {
		close(requested)
		<-release
		return nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
		done <- err
	}()
	<-requested
	require.Never(t, func() bool {
		return len(readDisableCauses(t, ctx, ti, orgID, "chat")) > 0
	}, 75*time.Millisecond, 10*time.Millisecond)
	close(release)

	err := <-done
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Empty(t, readDisableCauses(t, ctx, ti, orgID, "chat"), "a late Begin response must not receive a fresh mutation deadline")
	require.EqualValues(t, 0, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
}

func TestAdminLocalHardTimeoutCannotCommitAfterCrashGuardBecomesEligible(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithAdminMutationTimeout(t, 50*time.Millisecond)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-hard-timeout", "chat", "sk-or-admin-hard-timeout")
	lockConn, err := ti.conn.Acquire(ctx)
	require.NoError(t, err)
	defer lockConn.Release()
	queries := activitiesrepo.New(lockConn)
	params := activitiesrepo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: "chat"}
	require.NoError(t, queries.AcquireOpenRouterKeyBillingLock(ctx, params))
	executor := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: orgID, KeyType: "chat"}
	baseline, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)

	_, err = ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Zero(t, ti.provisioner.ReconcileCalls(), "Begin and a failed local mutation must not PATCH upstream")
	unlocked, err := queries.ReleaseOpenRouterKeyBillingLock(ctx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams(params))
	require.NoError(t, err)
	require.True(t, unlocked)
	require.Never(t, func() bool {
		return len(readDisableCauses(t, ctx, ti, orgID, "chat")) > 0
	}, 100*time.Millisecond, 10*time.Millisecond, "timed-out mutation must never commit later")
	require.EqualValues(t, 0, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	after, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, baseline, after, "rolled-back transaction must not advance commit proof")
}

func TestAdminReconciliationBaselineIsOrganizationWideSequence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := seedKey(t, ctx, ti, "admin-org-baseline", "chat", "sk-or-admin-org-baseline")
	want, err := testrepo.New(ti.conn).SeedAuditLogFixture(ctx, testrepo.SeedAuditLogFixtureParams{
		OrganizationID: orgID,
		Action:         "unrelated:action",
		KeyType:        "internal",
	})
	require.NoError(t, err)

	executor := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)
	got, err := executor.CaptureCursor(ctx, openrouterkeys.AdminReconciliationScope{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.Equal(t, want, got, "Begin must capture the indexed organization watermark, not scan for a matching historical event")
}

func TestAdminReconciliationRangeIsBoundedAndUsesOrganizationSequenceIndex(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	orgID := seedKey(t, ctx, ti, "admin-bounded-audit", "chat", "sk-or-admin-bounded-audit")
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: orgID, KeyType: "chat"}
	executor := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)
	baseline, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)

	err = testrepo.New(ti.conn).SeedUnrelatedAuditHistoryFixture(ctx, testrepo.SeedUnrelatedAuditHistoryFixtureParams{
		OrganizationID: orgID,
		KeyType:        "internal",
		EventCount:     200,
	})
	require.NoError(t, err)
	target, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)

	before := ti.provisioner.ReconcileCalls()
	advanced, err := executor.ReconcileSince(ctx, openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: baseline})
	require.NoError(t, err)
	require.Equal(t, target, advanced)
	require.Equal(t, before, ti.provisioner.ReconcileCalls(), "a bounded no-match range must not PATCH")

	matching, err := testrepo.New(ti.conn).SeedAuditLogFixture(ctx, testrepo.SeedAuditLogFixtureParams{
		OrganizationID: orgID,
		Action:         string(audit.ActionOpenRouterAPIKeyDisable),
		KeyType:        "chat",
	})
	require.NoError(t, err)
	advanced, err = executor.ReconcileSince(ctx, openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: target})
	require.NoError(t, err)
	require.Equal(t, matching, advanced)
	require.Equal(t, before+1, ti.provisioner.ReconcileCalls(), "a recent matching event must PATCH")

	//nolint:glint // This regression intentionally inspects PostgreSQL's plan for the production query.
	rows, err := ti.conn.Query(ctx, `
EXPLAIN (COSTS OFF)
SELECT seq
FROM audit_logs
WHERE organization_id = $1
  AND seq > $2
  AND seq <= $3
  AND action IN ('openrouter-key:disable', 'openrouter-key:enable')
  AND metadata->>'key_type' = $4
ORDER BY seq DESC
LIMIT 1`, orgID, target, matching, "chat")
	require.NoError(t, err)
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		planLines = append(planLines, line)
	}
	require.NoError(t, rows.Err())
	plan := strings.Join(planLines, "\n")
	require.Contains(t, plan, "audit_logs_organization_id_seq_idx")
	require.Contains(t, plan, "seq >")
	require.Contains(t, plan, "seq <=")
}

func TestAdminReconciliationAuditCursorAdvancesOnlyForCommittedMutation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-audit-cursor", "chat", "sk-or-admin-audit-cursor")
	scope := openrouterkeys.AdminReconciliationScope{OrganizationID: orgID, KeyType: "chat"}
	executor := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)

	baseline, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)
	_, err = ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	committed, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)
	require.Greater(t, committed, baseline)

	beforeRepair := ti.provisioner.ReconcileCalls()
	advanced, err := executor.ReconcileSince(ctx, openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: baseline})
	require.NoError(t, err)
	require.Equal(t, committed, advanced)
	require.Equal(t, beforeRepair+1, ti.provisioner.ReconcileCalls(), "postcommit cursor proof must PATCH")

	_, err = ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	noOpCursor, err := executor.CaptureCursor(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, committed, noOpCursor, "no-op mutation must not advance commit proof")
	beforeNoProofRepair := ti.provisioner.ReconcileCalls()
	_, err = executor.ReconcileSince(ctx, openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: committed})
	require.NoError(t, err)
	require.Equal(t, beforeNoProofRepair, ti.provisioner.ReconcileCalls(), "unchanged cursor must not PATCH")
}

func TestEnableKeyWaitsForPerKeyBillingLock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "enablelocked", "internal", "sk-or-enable-locked")
	setDisableCauses(t, ctx, ti, orgID, string(openrouter.KeyTypeInternal), []string{"admin_lock"})

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

func TestAdminDisableCauseSingleAndOverlappingTransitions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-causes", "chat", "sk-or-admin-causes")

	view, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, view.Disabled)
	require.Equal(t, []string{"admin_lock"}, view.DisableCauses)

	view, err = ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.False(t, view.Disabled)
	require.Empty(t, view.DisableCauses)

	setDisableCauses(t, ctx, ti, orgID, "chat", []string{"billing_inactive"})
	view, err = ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, view.Disabled)
	require.ElementsMatch(t, []string{"billing_inactive", "admin_lock"}, view.DisableCauses)

	view, err = ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, view.Disabled)
	require.Equal(t, []string{"billing_inactive"}, view.DisableCauses)
}

func TestAdminCauseMutationNULLFailsClosedAndMissing404s(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-null", "chat", "sk-or-admin-null")
	setDisableCauses(t, ctx, ti, orgID, "chat", nil)

	_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnexpected)
	_, err = ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnexpected)

	_, err = ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: "missing-org", KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: "missing-org", KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeNotFound)
	completes, aborts := ti.coordinator.Counts()
	require.Zero(t, completes, "permanent local failures must not occupy reconciliation")
	require.Equal(t, 4, aborts)
}

func TestAdminCauseRetriesAreIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-retry", "chat", "sk-or-admin-retry")
	auditBefore := auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable)
	outboxBefore := outboxCount(t, ctx, ti)

	first, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	second, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.Equal(t, first.DisableCauses, second.DisableCauses)
	require.Equal(t, auditBefore+1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.Equal(t, outboxBefore+1, outboxCount(t, ctx, ti))
	require.Equal(t, []string{orgID + "/chat/admin_lock"}, ti.provisioner.AddCauseCalls())

	enableAuditBefore := auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyEnable)
	first, err = ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	second, err = ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.Equal(t, first.DisableCauses, second.DisableCauses)
	require.Empty(t, second.DisableCauses)
	require.Equal(t, enableAuditBefore+1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyEnable))
	require.Equal(t, outboxBefore+2, outboxCount(t, ctx, ti))
	require.Equal(t, []string{orgID + "/chat/admin_lock"}, ti.provisioner.RemoveCauseCalls())
}

func TestAdminCauseAuditFailureRollsBackAndRetryCommitsOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-audit-rollback", "chat", "sk-or-admin-audit-rollback")
	auditBefore := auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable)
	outboxBefore := outboxCount(t, ctx, ti)

	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.InstallOpenRouterAdminDisableAuditFailureFixture(ctx))
	require.NoError(t, fixtures.EnableOpenRouterAdminDisableAuditFailureFixture(ctx))

	_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.Empty(t, readDisableCauses(t, ctx, ti, orgID, "chat"))
	require.Equal(t, auditBefore, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.Equal(t, outboxBefore, outboxCount(t, ctx, ti))

	require.NoError(t, fixtures.DisableOpenRouterAdminDisableAuditFailureFixture(ctx))
	view, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.Equal(t, []string{"admin_lock"}, view.DisableCauses)
	require.Equal(t, auditBefore+1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.Equal(t, outboxBefore+1, outboxCount(t, ctx, ti))
}

func TestAdminAmbiguousCommitKeepsDurableReconciliationArmed(t *testing.T) {
	t.Parallel()

	commitReplyLost := errors.New("commit reply lost")
	ctx, ti := newTestServiceWithOptions(t, nil, openrouterkeys.WithAdminMutationCommitForTest(func(ctx context.Context, tx pgx.Tx) error {
		require.NoError(t, tx.Commit(ctx))
		return commitReplyLost
	}))
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-ambiguous-commit", "chat", "sk-or-admin-ambiguous-commit")
	executor := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)
	var checkpoint openrouterkeys.AdminReconciliationCheckpoint
	ti.coordinator.begin = func(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) error {
		cursor, err := executor.CaptureCursor(ctx, scope)
		checkpoint = openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: cursor}
		if err != nil {
			return fmt.Errorf("capture cursor: %w", err)
		}
		return nil
	}
	ti.coordinator.complete = func(ctx context.Context, _ openrouterkeys.AdminReconciliationScope) error {
		_, err := executor.ReconcileSince(ctx, checkpoint)
		if err != nil {
			return fmt.Errorf("reconcile checkpoint: %w", err)
		}
		return nil
	}

	_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.ErrorContains(t, err, "admin disable openrouter key")
	completes, aborts := ti.coordinator.Counts()
	require.Equal(t, 1, completes, "an ambiguous outcome must request evidence-based durable completion")
	require.Zero(t, aborts, "an ambiguous outcome must not disarm repair")
	completeTokens, abortTokens := ti.coordinator.Tokens()
	require.Equal(t, []int64{1}, completeTokens)
	require.Empty(t, abortTokens)
	require.Equal(t, []string{"admin_lock"}, readDisableCauses(t, ctx, ti, orgID, "chat"))
	require.EqualValues(t, 1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.Equal(t, 1, ti.provisioner.ReconcileCalls(), "completion must reread committed audit evidence and converge upstream")
}

func TestAdminAmbiguousCommitErrorAfterRollbackCompletesWithoutRepair(t *testing.T) {
	t.Parallel()

	commitReplyLost := errors.New("commit outcome unavailable")
	commitAttempts := 0
	ctx, ti := newTestServiceWithOptions(t, nil, openrouterkeys.WithAdminMutationCommitForTest(func(ctx context.Context, tx pgx.Tx) error {
		commitAttempts++
		if commitAttempts == 1 {
			require.NoError(t, tx.Rollback(ctx))
			return commitReplyLost
		}
		return tx.Commit(ctx)
	}))
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-ambiguous-rollback", "chat", "sk-or-admin-ambiguous-rollback")
	executor := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)
	var checkpoint openrouterkeys.AdminReconciliationCheckpoint
	evidenceChecks := 0
	ti.coordinator.begin = func(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) error {
		cursor, err := executor.CaptureCursor(ctx, scope)
		checkpoint = openrouterkeys.AdminReconciliationCheckpoint{Scope: scope, Cursor: cursor}
		if err != nil {
			return fmt.Errorf("capture cursor: %w", err)
		}
		return nil
	}
	ti.coordinator.complete = func(ctx context.Context, _ openrouterkeys.AdminReconciliationScope) error {
		evidenceChecks++
		_, err := executor.ReconcileSince(ctx, checkpoint)
		if err != nil {
			return fmt.Errorf("reconcile checkpoint: %w", err)
		}
		return nil
	}
	outboxBefore := outboxCount(t, ctx, ti)

	_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnexpected)
	completes, aborts := ti.coordinator.Counts()
	require.Equal(t, 1, completes, "ambiguous rollback must route through durable completion")
	require.Zero(t, aborts)
	require.Equal(t, 1, evidenceChecks, "completion must perform a bounded durable evidence check")
	require.Zero(t, ti.provisioner.ReconcileCalls(), "rolled-back evidence must not PATCH upstream")
	require.Empty(t, readDisableCauses(t, ctx, ti, orgID, "chat"))
	require.EqualValues(t, 0, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.Equal(t, outboxBefore, outboxCount(t, ctx, ti))

	view, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err, "the ambiguous API error must remain safe to retry")
	require.Equal(t, []string{"admin_lock"}, view.DisableCauses)
	require.EqualValues(t, 1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.Equal(t, outboxBefore+1, outboxCount(t, ctx, ti))
	require.Equal(t, 1, ti.provisioner.ReconcileCalls())
}

func TestAdminDefinitelyUncommittedCommitFailureAbortsReconciliation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestServiceWithOptions(t, nil, openrouterkeys.WithAdminMutationCommitForTest(func(ctx context.Context, tx pgx.Tx) error {
		require.NoError(t, tx.Rollback(ctx))
		return pgx.ErrTxCommitRollback
	}))
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-rolled-back-commit", "chat", "sk-or-admin-rolled-back-commit")

	_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnexpected)
	completes, aborts := ti.coordinator.Counts()
	require.Zero(t, completes)
	require.Equal(t, 1, aborts, "a definite rollback may disarm durable repair")
	completeTokens, abortTokens := ti.coordinator.Tokens()
	require.Empty(t, completeTokens)
	require.Equal(t, []int64{1}, abortTokens)
	require.Empty(t, readDisableCauses(t, ctx, ti, orgID, "chat"))
	require.EqualValues(t, 0, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
}

func TestEnableWithoutAdminLockSkipsPolicyUpstreamAndAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-enable-noop", "chat", "sk-or-admin-enable-noop")
	setDisableCauses(t, ctx, ti, orgID, "chat", []string{"billing_inactive"})
	require.NoError(t, orgrepo.New(ti.conn).UpdateOpenRouterKeyMonthlyCredits(ctx, orgrepo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        "chat",
		MonthlyCredits: 0,
	}))
	require.NoError(t, orgmetarepo.New(ti.conn).SetAccountType(ctx, orgmetarepo.SetAccountTypeParams{
		ID:              orgID,
		GramAccountType: "unsupported-account",
	}))
	auditBefore := auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyEnable)

	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, view.Disabled)
	require.Equal(t, []string{"billing_inactive"}, view.DisableCauses)
	require.Empty(t, ti.provisioner.RefreshCalls())
	require.Empty(t, ti.provisioner.RemoveCauseCalls())
	require.Equal(t, auditBefore, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyEnable))
}

func TestAdminViewExposesFutureDisableCausesWithoutChangingCompatibilityBoolean(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-future-cause", "chat", "sk-or-admin-future-cause")
	setDisableCauses(t, ctx, ti, orgID, "chat", []string{"future_cause"})

	result, err := ti.service.ListKeys(adminCtx, &gen.ListKeysPayload{})
	require.NoError(t, err)
	var found *gen.AdminOpenRouterKey
	for _, key := range result.Keys {
		if key.OrganizationID == orgID && key.KeyType == "chat" {
			found = key
		}
	}
	require.NotNil(t, found)
	require.True(t, found.Disabled)
	require.Equal(t, []string{"future_cause"}, found.DisableCauses)
}

func TestAdminReconciliationClassifiesOnlyPermanentLocalState(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	reconciler := openrouterkeys.NewAdminReconciliationExecutor(testenv.NewLogger(t), ti.conn, ti.provisioner)

	err := reconciler.Reconcile(ctx, openrouterkeys.AdminReconciliationScope{OrganizationID: "missing-org", KeyType: "chat"})
	require.Error(t, err)
	require.True(t, openrouterkeys.IsPermanentAdminReconciliationError(err))

	orgID := seedKey(t, ctx, ti, "admin-reconcile-classify", "chat", "sk-or-admin-reconcile-classify")
	ti.provisioner.reconcileErr = errors.New("upstream temporarily unavailable")
	err = reconciler.Reconcile(ctx, openrouterkeys.AdminReconciliationScope{OrganizationID: orgID, KeyType: "chat"})
	require.Error(t, err)
	require.False(t, openrouterkeys.IsPermanentAdminReconciliationError(err))
}

func TestAdminMutationStartsDurablyBeforeChangingLocalState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-durable-first", "chat", "sk-or-admin-durable-first")

	requested := make(chan struct{})
	release := make(chan struct{})
	ti.coordinator.begin = func(ctx context.Context, _ openrouterkeys.AdminReconciliationScope) error {
		close(requested)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	done := make(chan error, 1)
	go func() {
		_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
		done <- err
	}()
	<-requested
	require.Empty(t, readDisableCauses(t, ctx, ti, orgID, "chat"), "the handler must not mutate before the durable operation starts")
	close(release)
	require.NoError(t, <-done)
	require.Equal(t, []string{"admin_lock"}, readDisableCauses(t, ctx, ti, orgID, "chat"))
}

func TestAdminMutationTimeoutDoesNotCancelLaterCompletion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-timeout-later", "chat", "sk-or-admin-timeout-later")
	reconcile := ti.coordinator.complete
	laterDone := make(chan error, 1)
	ti.coordinator.complete = func(ctx context.Context, scope openrouterkeys.AdminReconciliationScope) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Greater(t, time.Until(deadline), time.Second, "completion gets a cleanup deadline independent of the caller")
		go func() {
			timer := time.NewTimer(75 * time.Millisecond)
			defer timer.Stop()
			<-timer.C
			laterDone <- reconcile(context.Background(), scope)
		}()
		return context.DeadlineExceeded
	}

	waitCtx, cancel := context.WithTimeout(adminCtx, 20*time.Millisecond)
	defer cancel()
	_, err := ti.service.DisableKey(waitCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnavailable)
	require.Eventually(t, func() bool {
		return slices.Equal(readDisableCauses(t, ctx, ti, orgID, "chat"), []string{"admin_lock"})
	}, time.Second, 10*time.Millisecond)
	require.NoError(t, <-laterDone)
	require.EqualValues(t, 1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
}

func TestAdminOverlappingFailedWaitersConvergeLatestStateWithoutDuplicateAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-overlap-latest", "chat", "sk-or-admin-overlap-latest")
	reconcile := ti.coordinator.complete
	ti.coordinator.complete = func(context.Context, openrouterkeys.AdminReconciliationScope) error {
		return context.DeadlineExceeded
	}

	for _, call := range []func(context.Context) error{
		func(callCtx context.Context) error {
			_, err := ti.service.DisableKey(callCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
			return fmt.Errorf("disable key: %w", err)
		},
		func(callCtx context.Context) error {
			_, err := ti.service.EnableKey(callCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
			return fmt.Errorf("enable key: %w", err)
		},
	} {
		err := call(adminCtx)
		requireOopsCode(t, err, oops.CodeUnavailable)
	}

	ti.coordinator.complete = reconcile
	view, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, view.Disabled)
	require.Equal(t, []string{"admin_lock"}, view.DisableCauses)
	require.EqualValues(t, 2, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyDisable))
	require.EqualValues(t, 1, auditCount(t, ctx, ti, audit.ActionOpenRouterAPIKeyEnable))
}

func TestAdminCoordinatorInputContainsOnlyOrganizationAndKeyType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "admin-safe-input", "chat", "sk-or-history-secret")

	_, err := ti.service.DisableKey(adminCtx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.Len(t, ti.coordinator.Begins(), 1)
	scope := ti.coordinator.Begins()[0]
	encoded, err := json.Marshal(scope)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sk-or-history-secret")
	require.NotContains(t, string(encoded), "hash-admin-safe-input")
	require.NotContains(t, string(encoded), "admin_lock")
	require.NotContains(t, string(encoded), "disable_causes")
	require.JSONEq(t, fmt.Sprintf(`{"organization_id":%q,"key_type":"chat"}`, orgID), string(encoded))
}

func TestAdminCauseUpstreamFailureCommitsAndRetryReconciles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		initialCauses []string
		action        audit.Action
		wantDisabled  bool
		call          func(context.Context, *testInstance, string) (*gen.AdminOpenRouterKey, error)
	}{
		{
			name: "disable", initialCauses: []string{}, action: audit.ActionOpenRouterAPIKeyDisable, wantDisabled: true,
			call: func(ctx context.Context, ti *testInstance, orgID string) (*gen.AdminOpenRouterKey, error) {
				return ti.service.DisableKey(ctx, &gen.DisableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
			},
		},
		{
			name: "enable", initialCauses: []string{"admin_lock"}, action: audit.ActionOpenRouterAPIKeyEnable, wantDisabled: false,
			call: func(ctx context.Context, ti *testInstance, orgID string) (*gen.AdminOpenRouterKey, error) {
				return ti.service.EnableKey(ctx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			attempts := 0
			failUpstream := true
			var disabledRequests []bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Disabled bool `json:"disabled"`
				}
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				mu.Lock()
				attempts++
				shouldFail := failUpstream
				disabledRequests = append(disabledRequests, body.Disabled)
				mu.Unlock()
				if shouldFail {
					http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"hash": strings.TrimPrefix(r.URL.Path, "/v1/keys/")}})
			}))
			t.Cleanup(upstream.Close)

			ctx, ti := newRealOpenRouterTestService(t, upstream.URL)
			adminCtx := withAdmin(t, ctx)
			secret := "sk-or-reconcile-" + tc.name
			orgID := seedKey(t, ctx, ti, "reconcile-"+tc.name, "chat", secret)
			setDisableCauses(t, ctx, ti, orgID, "chat", tc.initialCauses)
			auditBefore := auditCount(t, ctx, ti, tc.action)
			outboxBefore := outboxCount(t, ctx, ti)

			_, err := tc.call(adminCtx, ti, orgID)
			requireOopsCode(t, err, oops.CodeUnavailable)
			mu.Lock()
			failUpstream = false
			failedAttempts := attempts
			mu.Unlock()
			require.NotContains(t, err.Error(), secret)
			row, readErr := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: "chat"})
			require.NoError(t, readErr)
			require.Equal(t, tc.wantDisabled, openrouter.EffectiveDisabled(row.Disabled, row.DisableCauses))
			require.Equal(t, auditBefore+1, auditCount(t, ctx, ti, tc.action))
			require.Equal(t, outboxBefore+1, outboxCount(t, ctx, ti))

			view, err := tc.call(adminCtx, ti, orgID)
			require.NoError(t, err)
			require.Equal(t, tc.wantDisabled, view.Disabled)
			require.Equal(t, auditBefore+1, auditCount(t, ctx, ti, tc.action), "retry must not duplicate audit")
			require.Equal(t, outboxBefore+1, outboxCount(t, ctx, ti), "retry must not duplicate outbox")

			mu.Lock()
			defer mu.Unlock()
			require.GreaterOrEqual(t, failedAttempts, 1)
			require.Equal(t, failedAttempts+1, attempts)
			for _, disabled := range disabledRequests {
				require.Equal(t, tc.wantDisabled, disabled)
			}
		})
	}
}

func TestAdminCauseReconcileUsesReplacementHashAndOverlappingDesiredState(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	failUpstream := true
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Disabled bool `json:"disabled"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.True(t, body.Disabled, "billing_inactive keeps the replacement key disabled after admin enable")
		mu.Lock()
		paths = append(paths, r.URL.Path)
		shouldFail := failUpstream
		mu.Unlock()
		if shouldFail {
			// The provider accepted and applied the idempotent request, but a
			// gateway timeout leaves the caller unable to know that result.
			http.Error(w, "ambiguous result", http.StatusGatewayTimeout)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"hash": "replacement-hash"}})
	}))
	t.Cleanup(upstream.Close)

	ctx, ti := newRealOpenRouterTestService(t, upstream.URL)
	adminCtx := withAdmin(t, ctx)
	orgID := seedKey(t, ctx, ti, "replacement", "chat", "sk-or-replacement-secret")
	setDisableCauses(t, ctx, ti, orgID, "chat", []string{"admin_lock", "billing_inactive"})

	_, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	requireOopsCode(t, err, oops.CodeUnavailable)
	mu.Lock()
	failUpstream = false
	failedAttempts := len(paths)
	mu.Unlock()
	require.Equal(t, []string{"billing_inactive"}, readDisableCauses(t, ctx, ti, orgID, "chat"))
	require.NoError(t, testrepo.New(ti.conn).SetOpenRouterAPIKeyHashFixture(ctx, testrepo.SetOpenRouterAPIKeyHashFixtureParams{
		OrganizationID: orgID, KeyType: "chat", KeyHash: "replacement-hash",
	}))

	view, err := ti.service.EnableKey(adminCtx, &gen.EnableKeyPayload{OrganizationID: orgID, KeyType: "chat"})
	require.NoError(t, err)
	require.True(t, view.Disabled)
	require.Equal(t, []string{"billing_inactive"}, view.DisableCauses)
	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, failedAttempts, 1)
	for _, path := range paths[:failedAttempts] {
		require.Equal(t, "/v1/keys/hash-replacement", path)
	}
	require.Equal(t, "/v1/keys/replacement-hash", paths[len(paths)-1])
}

func newRealOpenRouterTestService(t *testing.T, baseURL string) (context.Context, *testInstance) {
	t.Helper()
	return newTestServiceWithProvisioner(t, func(logger *slog.Logger, tracerProvider trace.TracerProvider, conn *pgxpool.Pool, enc *encryption.Client) openrouter.Provisioner {
		policy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
		require.NoError(t, err)
		testBaseURL, err := openrouter.WithTestBaseURL(baseURL)
		require.NoError(t, err)
		return openrouter.New(logger, tracerProvider, policy, conn, "test", "provisioning-secret", nil, nil, nil, enc, testBaseURL)
	})
}

func setDisableCauses(t *testing.T, ctx context.Context, ti *testInstance, orgID, keyType string, causes []string) {
	t.Helper()
	require.NoError(t, testrepo.New(ti.conn).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: keyType, Disabled: len(causes) > 0, DisableCauses: causes,
	}))
}

func readDisableCauses(t *testing.T, ctx context.Context, ti *testInstance, orgID, keyType string) []string {
	t.Helper()
	row, err := orgrepo.New(ti.conn).GetOpenRouterAPIKey(ctx, orgrepo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: keyType})
	require.NoError(t, err)
	return row.DisableCauses
}

func auditCount(t *testing.T, ctx context.Context, ti *testInstance, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
	require.NoError(t, err)
	return count
}

func outboxCount(t *testing.T, ctx context.Context, ti *testInstance) int64 {
	t.Helper()
	count, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.OpenRouterAPIKeyV1.EventType()))
	require.NoError(t, err)
	return count
}
