package openrouter

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

func TestPrepareEnterpriseTrialConversionKeyWithDB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		keyType        KeyType
		beforeCauses   []string
		beforeDisabled bool
		beforeCredits  int64
		floor          int64
		wantCauses     []string
		wantDisabled   bool
		wantCredits    int64
		wantChanged    bool
	}{
		{name: "chat removes trial and billing while preserving layered causes", keyType: KeyTypeChat, beforeCauses: []string{"admin_lock", "trial_demotion", "billing_inactive", "unknown_policy"}, beforeDisabled: true, beforeCredits: 10, floor: 50, wantCauses: []string{"admin_lock", "unknown_policy"}, wantDisabled: true, wantCredits: 50, wantChanged: true},
		{name: "internal removes only trial and preserves billing", keyType: KeyTypeInternal, beforeCauses: []string{"trial_demotion", "billing_inactive"}, beforeDisabled: true, beforeCredits: 10, floor: 50, wantCauses: []string{"billing_inactive"}, wantDisabled: true, wantCredits: 50, wantChanged: true},
		{name: "chat billing absent", keyType: KeyTypeChat, beforeCauses: []string{"trial_demotion"}, beforeDisabled: true, beforeCredits: 50, floor: 50, wantCauses: []string{}, wantDisabled: false, wantCredits: 50, wantChanged: true},
		{name: "chat removes billing independently when trial is absent", keyType: KeyTypeChat, beforeCauses: []string{"billing_inactive"}, beforeDisabled: true, beforeCredits: 50, floor: 50, wantCauses: []string{}, wantDisabled: false, wantCredits: 50, wantChanged: true},
		{name: "internal billing absent", keyType: KeyTypeInternal, beforeCauses: []string{"admin_lock"}, beforeDisabled: false, beforeCredits: 75, floor: 50, wantCauses: []string{"admin_lock"}, wantDisabled: true, wantCredits: 75, wantChanged: true},
		{name: "classified empty below floor repairs stale disabled", keyType: KeyTypeChat, beforeCauses: []string{}, beforeDisabled: true, beforeCredits: 10, floor: 50, wantCauses: []string{}, wantDisabled: false, wantCredits: 50, wantChanged: true},
		{name: "classified empty equal floor no change", keyType: KeyTypeInternal, beforeCauses: []string{}, beforeDisabled: false, beforeCredits: 50, floor: 50, wantCauses: []string{}, wantDisabled: false, wantCredits: 50, wantChanged: false},
		{name: "classified unknown above floor preserved", keyType: KeyTypeChat, beforeCauses: []string{"security_hold"}, beforeDisabled: true, beforeCredits: 75, floor: 50, wantCauses: []string{"security_hold"}, wantDisabled: true, wantCredits: 75, wantChanged: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			created, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
				OrganizationID: orgID, KeyType: string(tt.keyType), KeyEncrypted: pgtype.Text{String: "ciphertext", Valid: true}, KeyHash: "hash-" + orgID, MonthlyCredits: tt.beforeCredits,
			})
			require.NoError(t, err)
			require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
				OrganizationID: orgID, KeyType: string(tt.keyType), Disabled: tt.beforeDisabled, DisableCauses: tt.beforeCauses,
			}))

			tx := testenv.BeginTx(t, ctx, provisioner.db)
			change, err := provisioner.PrepareEnterpriseTrialConversionKeyWithDB(ctx, tx, orgID, tt.keyType, tt.floor)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))

			require.True(t, change.Exists)
			require.Equal(t, tt.wantChanged, change.Changed)
			require.Equal(t, EnterpriseTrialConversionKeyState{KeyType: tt.keyType, MonthlyCredits: tt.beforeCredits, Disabled: tt.beforeDisabled, DisableCauses: tt.beforeCauses}, change.Before)
			require.Equal(t, EnterpriseTrialConversionKeyState{KeyType: tt.keyType, MonthlyCredits: tt.wantCredits, Disabled: tt.wantDisabled, DisableCauses: tt.wantCauses}, change.After)

			after, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(tt.keyType)})
			require.NoError(t, err)
			require.Equal(t, created.KeyHash, after.KeyHash, "local preparation must preserve key identity")
			require.Equal(t, tt.wantCauses, after.DisableCauses)
			require.Equal(t, tt.wantDisabled, after.Disabled)
			require.Equal(t, tt.wantCredits, after.MonthlyCredits)
			require.Zero(t, upstream.requestCount(), "local preparation must not contact OpenRouter")
		})
	}
}

func TestPrepareEnterpriseTrialConversionKeyWithDBMissingAndDeletedAreNoOps(t *testing.T) {
	t.Parallel()

	for _, deleted := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "deleted"}[deleted], func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			if deleted {
				_, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), KeyEncrypted: pgtype.Text{String: "ciphertext", Valid: true}, KeyHash: "hash-" + orgID, MonthlyCredits: 5})
				require.NoError(t, err)
				require.NoError(t, testrepo.New(provisioner.db).SoftDeleteOpenRouterAPIKeyFixture(ctx, testrepo.SoftDeleteOpenRouterAPIKeyFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)}))
			}

			tx := testenv.BeginTx(t, ctx, provisioner.db)
			change, err := provisioner.PrepareEnterpriseTrialConversionKeyWithDB(ctx, tx, orgID, KeyTypeChat, 50)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			require.Equal(t, EnterpriseTrialConversionKeyChange{}, change)
			require.Zero(t, upstream.requestCount())
		})
	}
}

func TestPrepareEnterpriseTrialConversionKeyWithDBNullFailsClosed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	orgID := "org-" + uuid.NewString()[:8]
	provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
	_, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), KeyEncrypted: pgtype.Text{String: "ciphertext", Valid: true}, KeyHash: "hash-" + orgID, MonthlyCredits: 5})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), Disabled: true, DisableCauses: nil}))

	tx := testenv.BeginTx(t, ctx, provisioner.db)
	_, err = provisioner.PrepareEnterpriseTrialConversionKeyWithDB(ctx, tx, orgID, KeyTypeChat, 50)
	require.ErrorContains(t, err, "disable causes are unclassified")
	inTransaction, err := repo.New(tx).GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Nil(t, inTransaction.DisableCauses)
	require.True(t, inTransaction.Disabled)
	require.EqualValues(t, 5, inTransaction.MonthlyCredits, "unclassified preparation must perform no write even before rollback")
	require.NoError(t, tx.Rollback(ctx))

	after, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
	require.NoError(t, err)
	require.Nil(t, after.DisableCauses)
	require.True(t, after.Disabled)
	require.EqualValues(t, 5, after.MonthlyCredits)
	require.Zero(t, upstream.requestCount())
}
