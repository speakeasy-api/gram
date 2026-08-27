package openrouter

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

type disableTestUpstream struct {
	server      *httptest.Server
	mu          sync.Mutex
	patches     []string
	onPatch     func()
	patchStatus int
}

// recorded returns the raw patch bodies. They stay raw because the field a
// limit-only patch must NOT carry cannot be told apart from a null one after
// decoding.
func (u *disableTestUpstream) recorded() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]string(nil), u.patches...)
}

// interceptPatch runs fn while a patch is in flight.
func (u *disableTestUpstream) interceptPatch(fn func()) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.onPatch = fn
}

func (u *disableTestUpstream) respondToPatchesWith(status int) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.patchStatus = status
}

func newDisableTestProvisioner(t *testing.T, orgID string) (*OpenRouter, *disableTestUpstream, *repo.Queries) {
	t.Helper()

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "ordisablekey")
	require.NoError(t, err)

	_, err = orgRepo.New(conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Disable Key Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	upstream := &disableTestUpstream{server: nil, mu: sync.Mutex{}, patches: nil, onPatch: nil, patchStatus: 0}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodPost:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 100.0, "hash": "hash-1"},
				"key":  "sk-or-disable-1",
			})
		case http.MethodPatch:
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			upstream.mu.Lock()
			upstream.patches = append(upstream.patches, string(raw))
			onPatch := upstream.onPatch
			patchStatus := upstream.patchStatus
			upstream.mu.Unlock()

			if onPatch != nil {
				onPatch()
			}
			if patchStatus != 0 {
				w.WriteHeader(patchStatus)
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 100.0, "hash": "hash-1"},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.server.Close)

	guardianPolicy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	provisioner := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), guardianPolicy, conn, "test", "provisioning-key", nil, nil, nil, testenv.NewEncryptionClient(t))
	provisioner.baseURL = upstream.server.URL

	return provisioner, upstream, repo.New(conn)
}

func TestOpenRouterDisableCausesAreASet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)

	added, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		DisableCause:   string(DisableCauseAdminLock),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"admin_lock"}, added.DisableCauses)
	require.True(t, added.Disabled)

	addedAgain, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		DisableCause:   string(DisableCauseAdminLock),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"admin_lock"}, addedAgain.DisableCauses)

	withTrial, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		DisableCause:   string(DisableCauseTrialDemotion),
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"admin_lock", "trial_demotion"}, withTrial.DisableCauses)

	withoutTrial, err := queries.RemoveOpenRouterAPIKeyDisableCause(ctx, repo.RemoveOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		DisableCause:   string(DisableCauseTrialDemotion),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"admin_lock"}, withoutTrial.DisableCauses)
	require.True(t, withoutTrial.Disabled)
}

func TestAddAPIKeyDisableCause(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)

	change, err := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	require.Equal(t, []string{`{"disabled":true}`}, upstream.recorded())

	change, err = provisioner.AddAPIKeyDisableCauseWithDB(ctx, provisioner.db, orgID, KeyTypeChat, DisableCauseTrialDemotion)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true}, change)
	require.Len(t, upstream.recorded(), 1)

	change, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{}, change)
	require.Len(t, upstream.recorded(), 1)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"admin_lock", "trial_demotion"}, row.DisableCauses)
}

func TestAddAPIKeyDisableCause_UpstreamFailureAndRetry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	upstream.respondToPatchesWith(http.StatusBadGateway)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeInternal, DisableCauseAdminLock)
	require.Error(t, err)
	row, readErr := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeInternal)})
	require.NoError(t, readErr)
	require.Empty(t, row.DisableCauses)

	failedAttempts := len(upstream.recorded())
	upstream.respondToPatchesWith(0)
	change, err := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeInternal, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	require.Len(t, upstream.recorded(), failedAttempts+1)
	change, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeInternal, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{}, change)
	require.Len(t, upstream.recorded(), failedAttempts+1, "the successful retry must not patch again")
}

func TestAddAPIKeyDisableCause_ValidatesAndMissingKeyIsNoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, _ := newDisableTestProvisioner(t, orgID)
	change, err := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{}, change)
	require.Empty(t, upstream.recorded())

	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyType("invalid"), DisableCauseAdminLock)
	require.Error(t, err)
	_, err = provisioner.AddAPIKeyDisableCauseWithDB(ctx, provisioner.db, orgID, KeyTypeChat, DisableCause("invalid"))
	require.Error(t, err)
}

func TestRemoveAPIKeyDisableCause(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion)
	require.NoError(t, err)
	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)

	limit := int(before.MonthlyCredits)
	gotLimit, change, err := provisioner.RemoveAPIKeyDisableCauseWithDB(ctx, provisioner.db, orgID, KeyTypeChat, DisableCauseTrialDemotion, &limit)
	require.NoError(t, err)
	require.Equal(t, limit, gotLimit)
	require.Equal(t, DisableCauseChange{CauseChanged: true}, change)
	require.Len(t, upstream.recorded(), 1)

	gotLimit, change, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion, &limit)
	require.NoError(t, err)
	require.Zero(t, gotLimit)
	require.Equal(t, DisableCauseChange{}, change)
	require.Len(t, upstream.recorded(), 1)

	gotLimit, change, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, &limit)
	require.NoError(t, err)
	require.Equal(t, limit, gotLimit)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	require.Equal(t, []string{`{"disabled":true}`, `{"disabled":false}`}, upstream.recorded())

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Empty(t, row.DisableCauses)

	_, _, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyType("invalid"), DisableCauseAdminLock, nil)
	require.Error(t, err)
	_, _, err = provisioner.RemoveAPIKeyDisableCauseWithDB(ctx, provisioner.db, orgID, KeyTypeChat, DisableCause("invalid"), nil)
	require.Error(t, err)
}

func TestRemoveAPIKeyDisableCause_UpstreamFailurePreservesCause(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeInternal, DisableCauseBillingInactive)
	require.NoError(t, err)

	upstream.respondToPatchesWith(http.StatusBadGateway)
	_, _, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeInternal, DisableCauseBillingInactive, nil)
	require.Error(t, err)
	row, readErr := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeInternal)})
	require.NoError(t, readErr)
	require.Equal(t, []string{"billing_inactive"}, row.DisableCauses)
}

func TestRefreshAPIKeyLimit_PreservesDisableCauses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	_, err = queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{OrganizationID: orgID, KeyType: string(KeyTypeInternal), DisableCause: string(DisableCauseTrialDemotion)})
	require.NoError(t, err)

	limit := 42
	got, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)
	require.Equal(t, 42, got)
	patches := upstream.recorded()
	require.Len(t, patches, 1)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly"}`, patches[0])

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeInternal)})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
}

func TestDevelopmentDisableCauseMethodsAreNoops(t *testing.T) {
	t.Parallel()

	dev := NewDevelopment("dev-key")
	change, err := dev.AddAPIKeyDisableCause(t.Context(), "org", KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{}, change)

	change, err = dev.AddAPIKeyDisableCauseWithDB(t.Context(), nil, "org", KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{}, change)

	limit, change, err := dev.RemoveAPIKeyDisableCause(t.Context(), "org", KeyTypeChat, DisableCauseAdminLock, new(42))
	require.NoError(t, err)
	require.Zero(t, limit)
	require.Equal(t, DisableCauseChange{}, change)

	limit, change, err = dev.RemoveAPIKeyDisableCauseWithDB(t.Context(), nil, "org", KeyTypeChat, DisableCauseAdminLock, new(42))
	require.NoError(t, err)
	require.Zero(t, limit)
	require.Equal(t, DisableCauseChange{}, change)
}

func TestDisableAPIKey_DisablesKeyUpstream(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	// The patch carries the off switch and nothing else. Touching the ceiling
	// would lose the value a reinstatement restores.
	require.Equal(t, []string{`{"disabled":true}`}, upstream.recorded())

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.True(t, row.Disabled)
	require.Equal(t, before.MonthlyCredits, row.MonthlyCredits)
}

// The lockdown binds at key resolution, so a disabled key must never reach a
// completion. This is what turns a demotion into an error Gram can explain.
func TestProvisionAPIKey_RefusesDisabledKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, _ := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	key, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.ErrorIs(t, err, ErrPlatformKeyDisabled)
	require.Empty(t, key, "a refused resolution must not leak the key it refused")

	// Reinstatement makes resolution work again without minting a new key.
	limit := 42
	_, err = provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	key, err = provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.Equal(t, "sk-or-disable-1", key)
}

// A retried demotion must not fail on a key it already turned off, so
// disabling twice has to stay safe.
func TestDisableAPIKey_IsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	require.Len(t, upstream.recorded(), 2)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.True(t, row.Disabled)
}

// An organization that never provisioned a key of this type has nothing to
// lock down, and the sweeper must not fail on it.
func TestDisableAPIKey_NoKeyIsNoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, _ := newDisableTestProvisioner(t, orgID)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeChat))
	require.Empty(t, upstream.recorded())
}

// Sales reinstate a demoted organization by raising its limit, so the refresh
// path has to clear the flag on both sides.
func TestReinstateAPIKeyLimit_ReinstatesDisabledKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	limit := 42
	refreshed, err := provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)
	require.Equal(t, 42, refreshed)

	patches := upstream.recorded()
	require.Len(t, patches, 2)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly","disabled":false}`, patches[1],
		"a limit alone does not bring a disabled key back")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.False(t, row.Disabled, "a stale flag keeps key resolution failing after reinstatement")
	require.Equal(t, int64(42), row.MonthlyCredits)

	// Refreshing an enabled key must send the body it sent before the disabled
	// field existed. Carrying disabled=false on every refresh would revive a
	// key an operator turned off on the OpenRouter dashboard.
	_, err = provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	patches = upstream.recorded()
	require.Len(t, patches, 3)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly"}`, patches[2])
}

func TestReinstateAPIKeyLimit_RemovesOnlyLegacyAdminLock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	_, err = queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
		DisableCause:   string(DisableCauseTrialDemotion),
	})
	require.NoError(t, err)

	limit := 42
	_, err = provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
	require.True(t, row.Disabled)

	// A retry sees that admin_lock is already gone, preserves the remaining
	// cause, and reasserts the disabled upstream state.
	_, err = provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	patches := upstream.recorded()
	require.Len(t, patches, 3)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly","disabled":true}`, patches[1])
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly","disabled":true}`, patches[2])

	row, err = queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
	require.True(t, row.Disabled)
}

func TestReinstateAPIKeyLimit_RetryDisablesAfterConcurrentCause(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	injected := make(chan error, 1)
	var injectOnce sync.Once
	upstream.interceptPatch(func() {
		injectOnce.Do(func() {
			_, err := queries.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
				OrganizationID: orgID,
				KeyType:        string(KeyTypeInternal),
				DisableCause:   string(DisableCauseTrialDemotion),
			})
			injected <- err
		})
	})

	limit := 42
	_, err = provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)
	require.NoError(t, <-injected)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
	require.True(t, row.Disabled)

	_, err = provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	patches := upstream.recorded()
	require.Len(t, patches, 3)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly","disabled":false}`, patches[1])
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly","disabled":true}`, patches[2])
}

// A refresh reads the key row, patches upstream, then writes the row back. A
// lockdown that commits inside that window has to survive the write: the
// refresh never sent disabled=false, so clearing the local flag would hand out
// a key that OpenRouter refuses.
func TestRefreshAPIKeyLimit_KeepsLockdownThatLandsMidRefresh(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)

	locked := make(chan error, 1)
	upstream.interceptPatch(func() {
		locked <- queries.DisableOpenRouterAPIKey(ctx, repo.DisableOpenRouterAPIKeyParams{
			OrganizationID: orgID,
			KeyType:        string(KeyTypeInternal),
		})
	})

	limit := 42
	_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)
	require.NoError(t, <-locked)

	require.Equal(t, []string{`{"limit":42,"limit_reset":"monthly"}`}, upstream.recorded())

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.True(t, row.Disabled, "a refresh must not clear a lockdown it never saw")
	require.Equal(t, int64(42), row.MonthlyCredits)
}

func TestRefreshAPIKeyLimit_RejectsChangedUpstreamIdentity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	_, err = queries.UpdateOpenRouterKey(ctx, repo.UpdateOpenRouterKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		MonthlyCredits: 100,
		KeyHash:        "hash-stored",
	})
	require.NoError(t, err)

	limit := 42
	_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeChat, &limit)
	require.ErrorContains(t, err, "upstream key identity changed")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)
	require.Equal(t, "hash-stored", row.KeyHash)
	require.Equal(t, int64(100), row.MonthlyCredits)
}

func TestRefreshAPIKeyLimit_NilPreservesEachPaygInferenceCap(t *testing.T) {
	t.Parallel()

	for _, keyType := range []KeyType{KeyTypeChat, KeyTypeInternal} {
		t.Run(string(keyType), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			require.NoError(t, provisioner.orgRepo.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
				ID:              orgID,
				GramAccountType: string(billing.TierPayg),
			}))

			_, err := provisioner.ProvisionAPIKey(ctx, orgID, keyType)
			require.NoError(t, err)
			raisedCap := 321
			_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, keyType, &raisedCap)
			require.NoError(t, err)

			refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, keyType, nil)
			require.NoError(t, err)
			require.Equal(t, raisedCap, refreshed)

			patches := upstream.recorded()
			require.Len(t, patches, 1, "a nil PAYG refresh must not PATCH upstream")
			require.JSONEq(t, `{"limit":321,"limit_reset":"monthly"}`, patches[0])

			row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
				OrganizationID: orgID,
				KeyType:        string(keyType),
			})
			require.NoError(t, err)
			require.Equal(t, int64(raisedCap), row.MonthlyCredits)
		})
	}
}

func TestReconcileMonthlyCredits_UncappedUpstreamClearsMirror(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)

	effective, err := provisioner.ReconcileMonthlyCredits(ctx, orgID, KeyTypeChat, before.MonthlyCredits, before.UpdatedAt.Time.UnixMicro(), nil)
	require.NoError(t, err)
	require.Zero(t, effective, "an uncapped provider key has no enforceable alert denominator")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)
	require.Zero(t, row.MonthlyCredits, "the local display mirror must follow provider authority")
}

func TestReconcileMonthlyCredits_DoesNotOverwriteConcurrentCapChange(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)

	const newerCap = int64(250)
	require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		MonthlyCredits: newerCap,
	}))

	effective, err := provisioner.ReconcileMonthlyCredits(ctx, orgID, KeyTypeChat, before.MonthlyCredits, before.UpdatedAt.Time.UnixMicro(), nil)
	require.NoError(t, err)
	require.Equal(t, newerCap, effective)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)
	require.Equal(t, newerCap, row.MonthlyCredits)
}

func TestReconcileMonthlyCredits_DetectsSameValueCapOperation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)

	// A user can deliberately save the same numeric cap. The write generation,
	// not just the number, makes that operation newer than this poll.
	require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		MonthlyCredits: before.MonthlyCredits,
	}))

	effective, err := provisioner.ReconcileMonthlyCredits(ctx, orgID, KeyTypeChat, before.MonthlyCredits, before.UpdatedAt.Time.UnixMicro(), nil)
	require.NoError(t, err)
	require.Equal(t, before.MonthlyCredits, effective)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)
	require.Equal(t, before.MonthlyCredits, row.MonthlyCredits)
}

func TestRefreshAPIKeyLimit_NilPreservesDisabledKeyAfterTierTransition(t *testing.T) {
	t.Parallel()

	for _, keyType := range AllKeyTypes {
		t.Run(string(keyType), func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			require.NoError(t, provisioner.orgRepo.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
				ID:              orgID,
				GramAccountType: string(billing.TierPayg),
			}))

			_, err := provisioner.ProvisionAPIKey(ctx, orgID, keyType)
			require.NoError(t, err)
			raisedCap := 321
			_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, keyType, &raisedCap)
			require.NoError(t, err)
			require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, keyType))
			require.NoError(t, provisioner.orgRepo.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
				ID:              orgID,
				GramAccountType: string(billing.TierBase),
			}))
			patchesBeforeRefresh := upstream.recorded()

			refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, keyType, nil)
			require.NoError(t, err)
			require.Equal(t, raisedCap, refreshed)
			require.Equal(t, patchesBeforeRefresh, upstream.recorded(),
				"a generic refresh must not reinstate a disabled platform key")

			row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
				OrganizationID: orgID,
				KeyType:        string(keyType),
			})
			require.NoError(t, err)
			require.True(t, row.Disabled)
			require.Equal(t, int64(raisedCap), row.MonthlyCredits)
		})
	}
}

func TestRefreshAPIKeyLimit_NilRepairsLegacyEnabledZeroKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
		MonthlyCredits: 0,
	}))

	expected, ok := ResolveDefaultCreditLimit(ctx, provisioner.logger, provisioner.db, orgID, billing.TierBase)
	require.True(t, ok)
	refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, nil)
	require.NoError(t, err)
	require.Equal(t, expected, refreshed)
	require.Len(t, upstream.recorded(), 1, "generic reconciliation must repair a legacy zero key")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.EqualValues(t, expected, row.MonthlyCredits)
}

func TestRefreshAPIKeyLimit_NilPreservesPaygZeroSpendCap(t *testing.T) {
	t.Parallel()

	for _, keyType := range AllKeyTypes {
		t.Run(string(keyType), func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			require.NoError(t, provisioner.orgRepo.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
				ID:              orgID,
				GramAccountType: string(billing.TierPayg),
			}))

			_, err := provisioner.ProvisionAPIKey(ctx, orgID, keyType)
			require.NoError(t, err)
			require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
				OrganizationID: orgID,
				KeyType:        string(keyType),
				MonthlyCredits: 0,
			}))

			refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, keyType, nil)
			require.NoError(t, err)
			require.Zero(t, refreshed)
			require.Empty(t, upstream.recorded(), "generic reconciliation must preserve a PAYG zero spend cap")

			row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
				OrganizationID: orgID,
				KeyType:        string(keyType),
			})
			require.NoError(t, err)
			require.Zero(t, row.MonthlyCredits)
			require.False(t, row.Disabled)
		})
	}
}

func TestReinstateAPIKeyLimit_NilRevivesLegacyZeroChatKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
		MonthlyCredits: 0,
	}))
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeChat))

	refreshed, err := provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeChat, nil)
	require.NoError(t, err)
	require.Positive(t, refreshed)
	patches := upstream.recorded()
	require.Len(t, patches, 2, "trial rearm must explicitly PATCH the disabled legacy key")
	require.Contains(t, patches[1], `"disabled":false`)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeChat),
	})
	require.NoError(t, err)
	require.False(t, row.Disabled)
	require.EqualValues(t, refreshed, row.MonthlyCredits)
}

func TestRefreshAPIKeyLimit_ExplicitLimitRepairsLegacyEnabledZeroKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, queries.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
		MonthlyCredits: 0,
	}))

	limit := 100
	refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)
	require.Equal(t, limit, refreshed)
	require.Len(t, upstream.recorded(), 1, "explicit lifecycle repair must PATCH the legacy key")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.EqualValues(t, limit, row.MonthlyCredits)
}

func TestRefreshAPIKeyLimit_RejectsExplicitZeroLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)

	zero := 0
	_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &zero)
	require.ErrorContains(t, err, "monthly credits must be positive")
	require.Empty(t, upstream.recorded(), "an invalid ceiling must fail before PATCH")

	after, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(KeyTypeInternal),
	})
	require.NoError(t, err)
	require.Equal(t, before.MonthlyCredits, after.MonthlyCredits)
}
