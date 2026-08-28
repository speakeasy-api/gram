package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

type disableTestUpstream struct {
	server    *httptest.Server
	mu        sync.Mutex
	requests  int
	patches   []string
	onPatch   func()
	patchHash string
}

// recorded returns the raw patch bodies. They stay raw because the field a
// limit-only patch must NOT carry cannot be told apart from a null one after
// decoding.
func (u *disableTestUpstream) recorded() []string {
	u.mu.Lock()
	defer u.mu.Unlock()

	return append([]string(nil), u.patches...)
}

func (u *disableTestUpstream) requestCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.requests
}

// interceptPatch runs fn while a patch is in flight.
func (u *disableTestUpstream) interceptPatch(fn func()) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.onPatch = fn
}

func (u *disableTestUpstream) respondWithPatchHash(hash string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.patchHash = hash
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

	upstream := &disableTestUpstream{server: nil, mu: sync.Mutex{}, patches: nil, onPatch: nil, patchHash: "hash-1"}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.mu.Lock()
		upstream.requests++
		upstream.mu.Unlock()
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
			patchHash := upstream.patchHash
			upstream.mu.Unlock()

			if onPatch != nil {
				onPatch()
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"limit": 100.0, "hash": patchHash},
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

func TestEffectiveDisabledCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		legacy        bool
		disableCauses []string
		want          bool
	}{
		{name: "unclassified enabled", legacy: false, disableCauses: nil, want: false},
		{name: "unclassified disabled", legacy: true, disableCauses: nil, want: true},
		{name: "classified empty ignores stale legacy disabled", legacy: true, disableCauses: []string{}, want: false},
		{name: "classified cause ignores stale legacy enabled", legacy: false, disableCauses: []string{"trial_demotion"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, EffectiveDisabled(tt.legacy, tt.disableCauses))
		})
	}
}

func TestProvisionAPIKeyInitializesClassifiedEnabledRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.NotNil(t, row.DisableCauses)
	require.Empty(t, row.DisableCauses)
}

func TestProvisionAPIKeyUsesClassifiedEffectiveState(t *testing.T) {
	t.Parallel()

	t.Run("empty causes override stale legacy disabled", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		orgID := "org-" + uuid.NewString()[:8]
		provisioner, _, _ := newDisableTestProvisioner(t, orgID)

		wantKey, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
		require.NoError(t, err)
		err = testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), Disabled: true, DisableCauses: []string{}})
		require.NoError(t, err)

		gotKey, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
		require.NoError(t, err)
		require.Equal(t, wantKey, gotKey)
	})

	t.Run("causes override stale legacy enabled", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		orgID := "org-" + uuid.NewString()[:8]
		provisioner, _, _ := newDisableTestProvisioner(t, orgID)

		_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
		require.NoError(t, err)
		err = testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), Disabled: false, DisableCauses: []string{"trial_demotion"}})
		require.NoError(t, err)

		_, err = provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
		require.ErrorIs(t, err, ErrPlatformKeyDisabled)
	})
}

func TestAddAPIKeyDisableCauseRejectsMismatchedUpstreamIdentityWithoutLocalWrite(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	upstream.respondWithPatchHash("different-hash")

	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.ErrorContains(t, err, "identity mismatch")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.False(t, row.Disabled)
	require.Empty(t, row.DisableCauses)
}

func TestAddAPIKeyDisableCauseRejectsRotationAfterValidUpstreamResponse(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	upstream.interceptPatch(func() {
		_, updateErr := queries.UpdateOpenRouterKey(ctx, repo.UpdateOpenRouterKeyParams{
			MonthlyCredits: 100, KeyHash: "rotated-hash", Reinstate: false,
			OrganizationID: orgID, KeyType: string(KeyTypeChat),
		})
		require.NoError(t, updateErr)
	})

	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.ErrorContains(t, err, "changed concurrently")

	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, "rotated-hash", row.KeyHash)
	require.Empty(t, row.DisableCauses)
}

func TestAddAPIKeyDisableCauseSerializesConcurrentSameCause(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)

	firstPatch := make(chan struct{})
	releasePatch := make(chan struct{})
	var once sync.Once
	upstream.interceptPatch(func() {
		once.Do(func() {
			close(firstPatch)
			<-releasePatch
		})
	})

	results := make(chan DisableCauseChange, 2)
	errs := make(chan error, 2)
	go func() {
		result, addErr := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
		results <- result
		errs <- addErr
	}()
	<-firstPatch
	go func() {
		result, addErr := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
		results <- result
		errs <- addErr
	}()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePatch) }) }
	t.Cleanup(release)
	require.Never(t, func() bool { return len(upstream.recorded()) > 1 }, 50*time.Millisecond, 5*time.Millisecond)
	release()

	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	first, second := <-results, <-results
	require.NotEqual(t, first.CauseChanged, second.CauseChanged)
	require.NotEqual(t, first.KeyAccessChanged, second.KeyAccessChanged)
	require.Len(t, upstream.recorded(), 1)
	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, []string{string(DisableCauseAdminLock)}, row.DisableCauses)
}

func TestAddAPIKeyDisableCauseCanonicalizesConcurrentDifferentCauses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)

	firstPatch := make(chan struct{})
	releasePatch := make(chan struct{})
	var once sync.Once
	upstream.interceptPatch(func() {
		once.Do(func() {
			close(firstPatch)
			<-releasePatch
		})
	})
	type addition struct {
		change DisableCauseChange
		err    error
	}
	results := make(chan addition, 2)
	go func() {
		change, addErr := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion)
		results <- addition{change: change, err: addErr}
	}()
	<-firstPatch
	go func() {
		change, addErr := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
		results <- addition{change: change, err: addErr}
	}()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePatch) }) }
	t.Cleanup(release)
	require.Never(t, func() bool { return len(upstream.recorded()) > 1 }, 50*time.Millisecond, 5*time.Millisecond)
	release()

	first, second := <-results, <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.True(t, first.change.CauseChanged)
	require.True(t, second.change.CauseChanged)
	require.NotEqual(t, first.change.KeyAccessChanged, second.change.KeyAccessChanged)
	require.Len(t, upstream.recorded(), 2)
	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, []string{string(DisableCauseAdminLock), string(DisableCauseTrialDemotion)}, row.DisableCauses)
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
	_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
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

// A missing key is already effectively disabled, so disabling it is a no-op
// and must not issue an upstream mutation.
func TestDisableAPIKey_NoKeyIsNoop(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, _ := newDisableTestProvisioner(t, orgID)

	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeChat))
	require.Empty(t, upstream.recorded())
}

// Refreshing a classified key updates its limit without discarding the causes
// that still determine whether it is disabled.
func TestRefreshAPIKeyLimit_PreservesClassifiedDisableCauses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		disabled      bool
		disableCauses []string
	}{
		{name: "admin and trial", disabled: true, disableCauses: []string{"admin_lock", "trial_demotion"}},
		{name: "stale false admin and billing", disabled: false, disableCauses: []string{"admin_lock", "billing_inactive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
			require.NoError(t, err)
			require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
				OrganizationID: orgID,
				KeyType:        string(KeyTypeInternal),
				Disabled:       tt.disabled,
				DisableCauses:  tt.disableCauses,
			}))

			limit := 42
			refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
			require.NoError(t, err)
			require.Equal(t, 42, refreshed)

			patches := upstream.recorded()
			require.Len(t, patches, 1)
			require.JSONEq(t, `{"limit":42,"limit_reset":"monthly"}`, patches[0])

			row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
				OrganizationID: orgID,
				KeyType:        string(KeyTypeInternal),
			})
			require.NoError(t, err)
			require.Equal(t, tt.disabled, row.Disabled)
			require.Equal(t, tt.disableCauses, row.DisableCauses)
			require.Equal(t, int64(42), row.MonthlyCredits)
		})
	}
}

func TestReinstateAPIKeyLimit_DoesNotEnableAfterLegacyClassificationWins(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, _ := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: string(KeyTypeInternal), Disabled: true, DisableCauses: nil,
	}))

	classifier := testenv.BeginTx(t, ctx, provisioner.db)
	require.NoError(t, repo.New(classifier).AcquireOpenRouterBillingLock(ctx, repo.AcquireOpenRouterBillingLockParams{
		OrganizationID: orgID, KeyType: string(KeyTypeInternal),
	}))

	patchStarted := make(chan struct{})
	releasePatch := make(chan struct{})
	var patchOnce sync.Once
	upstream.interceptPatch(func() {
		patchOnce.Do(func() { close(patchStarted) })
		<-releasePatch
	})

	refreshDone := make(chan error, 1)
	go func() {
		_, refreshErr := provisioner.ReinstateAPIKeyLimit(ctx, orgID, KeyTypeInternal, nil)
		refreshDone <- refreshErr
	}()

	select {
	case <-patchStarted:
		// The legacy implementation reads before joining the classifier's lock.
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, testrepo.New(classifier).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: string(KeyTypeInternal), Disabled: true, DisableCauses: []string{string(DisableCauseAdminLock)},
	}))
	require.NoError(t, classifier.Commit(ctx))

	select {
	case <-patchStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "reinstate did not reach upstream after classification committed")
	}
	close(releasePatch)
	require.NoError(t, <-refreshDone)

	patches := upstream.recorded()
	require.Len(t, patches, 1)
	require.JSONEq(t, `{"limit":5,"limit_reset":"monthly"}`, patches[0],
		"a classified cause must prevent the stale legacy read from enabling upstream")
}

func TestReinstateAPIKeyLimitWithDB_ReentersTransactionBillingLock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, _ := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
		OrganizationID: orgID, KeyType: string(KeyTypeInternal), Disabled: true, DisableCauses: nil,
	}))

	tx := testenv.BeginTx(t, ctx, provisioner.db)
	require.NoError(t, repo.New(tx).AcquireOpenRouterBillingLock(ctx, repo.AcquireOpenRouterBillingLockParams{
		OrganizationID: orgID, KeyType: string(KeyTypeInternal),
	}))

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err = provisioner.ReinstateAPIKeyLimitWithDB(callCtx, tx, orgID, KeyTypeInternal, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

func TestRefreshAPIKeyLimit_ReinstatesDisabledKey(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)

	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeInternal)
	require.NoError(t, err)
	require.NoError(t, provisioner.DisableAPIKey(ctx, orgID, KeyTypeInternal))

	limit := 42
	refreshed, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
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
	_, err = provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeInternal, &limit)
	require.NoError(t, err)

	patches = upstream.recorded()
	require.Len(t, patches, 3)
	require.JSONEq(t, `{"limit":42,"limit_reset":"monthly"}`, patches[2])
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
		Reinstate:      false,
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
