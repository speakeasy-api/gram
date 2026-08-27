package openrouter

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

func TestRemoveOpenRouterAPIKeyDisableCauseQueryNoopDoesNotTouchUpdatedAt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)

	after, err := queries.RemoveOpenRouterAPIKeyDisableCause(ctx, repo.RemoveOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID, KeyType: string(KeyTypeChat), KeyHash: before.KeyHash,
		DisableCause: string(DisableCauseAdminLock), MonthlyCredits: 999, UpdateMonthlyCredits: true,
	})
	require.NoError(t, err)
	require.Equal(t, before.DisableCauses, after.DisableCauses)
	require.Equal(t, before.MonthlyCredits, after.MonthlyCredits)
	require.Equal(t, before.UpdatedAt, after.UpdatedAt)
}

func TestRemoveAPIKeyDisableCauseMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		seed        []DisableCause
		remove      DisableCause
		limit       *int
		wantChange  DisableCauseChange
		wantCauses  []string
		wantPatches []string
		wantLimit   int64
	}{
		{name: "absent cause is a no-op", seed: []DisableCause{DisableCauseAdminLock}, remove: DisableCauseTrialDemotion, wantCauses: []string{"admin_lock"}},
		{name: "non-last removal is local only and ignores limit", seed: []DisableCause{DisableCauseBillingInactive, DisableCauseTrialDemotion, DisableCauseAdminLock}, remove: DisableCauseTrialDemotion, limit: new(42), wantChange: DisableCauseChange{CauseChanged: true}, wantCauses: []string{"admin_lock", "billing_inactive"}},
		{name: "last removal enables without changing a healthy limit", seed: []DisableCause{DisableCauseAdminLock}, remove: DisableCauseAdminLock, wantChange: DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, wantCauses: []string{}, wantPatches: []string{`{"disabled":false}`}},
		{name: "last removal applies an explicit recovered limit", seed: []DisableCause{DisableCauseBillingInactive}, remove: DisableCauseBillingInactive, limit: new(42), wantChange: DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, wantCauses: []string{}, wantPatches: []string{`{"limit":42,"limit_reset":"monthly","disabled":false}`}, wantLimit: 42},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
			require.NoError(t, err)
			for _, cause := range tt.seed {
				_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, cause)
				require.NoError(t, err)
			}
			before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
			require.NoError(t, err)
			wantLimit := tt.wantLimit
			if wantLimit == 0 {
				wantLimit = before.MonthlyCredits
			}
			patchesBefore := len(upstream.recorded())

			gotLimit, change, err := provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, tt.remove, tt.limit)
			require.NoError(t, err)
			require.Equal(t, tt.wantChange, change)
			require.Equal(t, int(wantLimit), gotLimit)
			patches := upstream.recorded()[patchesBefore:]
			require.Len(t, patches, len(tt.wantPatches))
			for i := range tt.wantPatches {
				require.JSONEq(t, tt.wantPatches[i], patches[i])
			}
			row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
			require.NoError(t, err)
			require.Equal(t, tt.wantCauses, row.DisableCauses)
			require.Equal(t, wantLimit, row.MonthlyCredits)
			require.Equal(t, len(tt.wantCauses) > 0, row.Disabled)

			if tt.wantChange.KeyAccessChanged {
				patchCount := len(upstream.recorded())
				retryLimit, retryChange, retryErr := provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, tt.remove, tt.limit)
				require.NoError(t, retryErr)
				require.Equal(t, int(wantLimit), retryLimit)
				require.Equal(t, DisableCauseChange{}, retryChange)
				require.Len(t, upstream.recorded(), patchCount, "an idempotent retry must not PATCH upstream")
			}
		})
	}
}

func TestRemoveAPIKeyDisableCauseRecoversLegacyZeroLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion)
	require.NoError(t, err)
	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	_, err = queries.UpdateOpenRouterKey(ctx, repo.UpdateOpenRouterKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), KeyHash: row.KeyHash, MonthlyCredits: 0, ReinstateLegacy: false})
	require.NoError(t, err)
	patchesBefore := len(upstream.recorded())

	gotLimit, change, err := provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion, nil)
	require.NoError(t, err)
	require.Positive(t, gotLimit)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	patches := upstream.recorded()[patchesBefore:]
	require.Len(t, patches, 1)
	require.JSONEq(t, fmt.Sprintf(`{"limit":%d,"limit_reset":"monthly","disabled":false}`, gotLimit), patches[0])
	row, err = queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.EqualValues(t, gotLimit, row.MonthlyCredits)
	require.Empty(t, row.DisableCauses)
	require.False(t, row.Disabled)
}

func TestDisableCauseWithDBUsesCallerLockedConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, _, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	conn, err := provisioner.db.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	lockParams := repo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)}
	require.NoError(t, repo.New(conn).AcquireOpenRouterKeyBillingLock(ctx, lockParams))
	defer func() {
		unlocked, releaseErr := repo.New(conn).ReleaseOpenRouterKeyBillingLock(ctx, repo.ReleaseOpenRouterKeyBillingLockParams(lockParams))
		require.NoError(t, releaseErr)
		require.True(t, unlocked)
	}()

	change, err := provisioner.AddAPIKeyDisableCauseWithDB(ctx, conn, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	_, change, err = provisioner.RemoveAPIKeyDisableCauseWithDB(ctx, conn, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Empty(t, row.DisableCauses)
	require.False(t, row.Disabled)
}

func TestRemoveAPIKeyDisableCauseMissingAndUnclassifiedFailClosed(t *testing.T) {
	t.Parallel()

	t.Run("missing key is a no-op", func(t *testing.T) {
		ctx := t.Context()
		orgID := "org-" + uuid.NewString()[:8]
		provisioner, upstream, _ := newDisableTestProvisioner(t, orgID)
		limit, change, err := provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
		require.NoError(t, err)
		require.Zero(t, limit)
		require.Equal(t, DisableCauseChange{}, change)
		require.Empty(t, upstream.recorded())
	})

	t.Run("NULL classification fails closed", func(t *testing.T) {
		ctx := t.Context()
		orgID := "org-" + uuid.NewString()[:8]
		provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
		_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
		require.NoError(t, err)
		require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), Disabled: true, DisableCauses: nil}))

		_, _, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
		require.ErrorContains(t, err, "unclassified")
		require.Empty(t, upstream.recorded())
		row, getErr := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
		require.NoError(t, getErr)
		require.Nil(t, row.DisableCauses)
		require.True(t, row.Disabled)
	})
}

func TestRemoveAPIKeyDisableCauseUpstreamFailureAndHashMismatchLeaveLocalUnchanged(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		configure func(*disableTestUpstream)
	}{
		{name: "upstream failure", configure: func(u *disableTestUpstream) { u.respondToPatchesWith(http.StatusBadGateway) }},
		{name: "hash mismatch", configure: func(u *disableTestUpstream) { u.respondWithPatchHash("wrong-hash") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
			require.NoError(t, err)
			_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
			require.NoError(t, err)
			before, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
			require.NoError(t, err)
			tt.configure(upstream)

			_, _, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
			require.Error(t, err)
			row, getErr := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
			require.NoError(t, getErr)
			require.Equal(t, []string{"admin_lock"}, row.DisableCauses)
			require.True(t, row.Disabled)
			require.Equal(t, before.MonthlyCredits, row.MonthlyCredits)
		})
	}
}

func TestRemoveAPIKeyDisableCauseRotationConflictRetryConverges(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)
	upstream.interceptPatch(func() {
		upstream.interceptPatch(nil)
		_, rotateErr := provisioner.db.Exec(ctx, `UPDATE openrouter_api_keys SET key_hash = 'hash-2' WHERE organization_id = $1 AND key_type = $2`, orgID, string(KeyTypeChat))
		require.NoError(t, rotateErr)
	})

	_, _, err = provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
	require.ErrorContains(t, err, "changed concurrently")
	row, getErr := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, getErr)
	require.Equal(t, []string{"admin_lock"}, row.DisableCauses)
	require.True(t, row.Disabled)

	upstream.respondWithPatchHash("hash-2")
	_, change, err := provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	row, err = queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Empty(t, row.DisableCauses)
	require.False(t, row.Disabled)
}

func TestConcurrentAddAndRemoveDoesNotLoseCause(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock)
	require.NoError(t, err)

	added := make(chan error, 1)
	var once sync.Once
	upstream.interceptPatch(func() {
		once.Do(func() {
			go func() {
				_, addErr := provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion)
				added <- addErr
			}()
		})
	})

	_, change, err := provisioner.RemoveAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseAdminLock, nil)
	require.NoError(t, err)
	require.Equal(t, DisableCauseChange{CauseChanged: true, KeyAccessChanged: true}, change)
	require.NoError(t, <-added)
	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
	require.True(t, row.Disabled)
}

func TestRefreshAPIKeyLimitPreservesDisableCauses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := provisioner.ProvisionAPIKey(ctx, orgID, KeyTypeChat)
	require.NoError(t, err)
	_, err = provisioner.AddAPIKeyDisableCause(ctx, orgID, KeyTypeChat, DisableCauseTrialDemotion)
	require.NoError(t, err)
	patchesBefore := len(upstream.recorded())
	limit := 77

	got, err := provisioner.RefreshAPIKeyLimit(ctx, orgID, KeyTypeChat, &limit)
	require.NoError(t, err)
	require.Equal(t, limit, got)
	patches := upstream.recorded()[patchesBefore:]
	require.Len(t, patches, 1)
	require.JSONEq(t, `{"limit":77,"limit_reset":"monthly"}`, patches[0])
	row, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Equal(t, []string{"trial_demotion"}, row.DisableCauses)
	require.True(t, row.Disabled)
	require.EqualValues(t, 77, row.MonthlyCredits)
}
