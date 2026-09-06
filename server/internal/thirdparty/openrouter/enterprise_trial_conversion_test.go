package openrouter

import (
	"context"
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
			testQueries := testrepo.New(provisioner.db)
			require.NoError(t, testQueries.SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{
				OrganizationID: orgID, KeyType: string(tt.keyType), Disabled: tt.beforeDisabled, DisableCauses: tt.beforeCauses,
			}))

			siblingType := KeyTypeInternal
			if tt.keyType == KeyTypeInternal {
				siblingType = KeyTypeChat
			}
			sibling, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(siblingType), KeyEncrypted: pgtype.Text{String: "sibling-ciphertext", Valid: true}, KeyHash: "sibling-hash-" + orgID, MonthlyCredits: 17})
			require.NoError(t, err)
			decoyOrgID := orgID + "-decoy"
			decoy, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{OrganizationID: decoyOrgID, KeyType: string(tt.keyType), KeyEncrypted: pgtype.Text{String: "decoy-ciphertext", Valid: true}, KeyHash: "decoy-hash-" + orgID, MonthlyCredits: 23})
			require.NoError(t, err)

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
			siblingAfter, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(siblingType)})
			require.NoError(t, err)
			require.Equal(t, sibling, siblingAfter, "conversion preparation must not touch the sibling key")
			decoyAfter, err := queries.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: decoyOrgID, KeyType: string(tt.keyType)})
			require.NoError(t, err)
			require.Equal(t, decoy, decoyAfter, "conversion preparation must not touch another organization")
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

func TestPrepareEnterpriseTrialConversionKeyWithDBConcurrentGuardFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		hook        func(context.Context, DBTX, string) error
		wantHash    func(string) string
		wantCauses  []string
		wantDeleted bool
	}{
		{
			name: "stale snapshot after key hash replacement",
			hook: func(ctx context.Context, db DBTX, orgID string) error {
				return testrepo.New(db).SetOpenRouterAPIKeyHashFixture(ctx, testrepo.SetOpenRouterAPIKeyHashFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), KeyHash: "replacement-hash-" + orgID})
			},
			wantHash:   func(orgID string) string { return "replacement-hash-" + orgID },
			wantCauses: []string{"trial_demotion"},
		},
		{
			name: "current causes become unclassified",
			hook: func(ctx context.Context, db DBTX, orgID string) error {
				return testrepo.New(db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), Disabled: true, DisableCauses: nil})
			},
			wantHash:   func(orgID string) string { return "hash-" + orgID },
			wantCauses: nil,
		},
		{
			name: "current row becomes soft deleted",
			hook: func(ctx context.Context, db DBTX, orgID string) error {
				return testrepo.New(db).SoftDeleteOpenRouterAPIKeyFixture(ctx, testrepo.SoftDeleteOpenRouterAPIKeyFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
			},
			wantHash:    func(orgID string) string { return "hash-" + orgID },
			wantCauses:  []string{"trial_demotion"},
			wantDeleted: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			orgID := "org-" + uuid.NewString()[:8]
			provisioner, upstream, queries := newDisableTestProvisioner(t, orgID)
			_, err := queries.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), KeyEncrypted: pgtype.Text{String: "ciphertext", Valid: true}, KeyHash: "hash-" + orgID, MonthlyCredits: 5})
			require.NoError(t, err)
			require.NoError(t, testrepo.New(provisioner.db).SetOpenRouterAPIKeyClassificationFixture(ctx, testrepo.SetOpenRouterAPIKeyClassificationFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat), Disabled: true, DisableCauses: []string{"trial_demotion"}}))

			tx := testenv.BeginTx(t, ctx, provisioner.db)
			_, err = provisioner.prepareEnterpriseTrialConversionKeyWithDB(ctx, tx, orgID, KeyTypeChat, 50, func(ctx context.Context, db DBTX) error {
				return tt.hook(ctx, db, orgID)
			})
			require.ErrorIs(t, err, errEnterpriseTrialConversionKeyChangedConcurrently)

			current, err := testrepo.New(tx).GetOpenRouterAPIKeyStateFixture(ctx, testrepo.GetOpenRouterAPIKeyStateFixtureParams{OrganizationID: orgID, KeyType: string(KeyTypeChat)})
			require.NoError(t, err)
			require.Equal(t, tt.wantHash(orgID), current.KeyHash)
			require.EqualValues(t, 5, current.MonthlyCredits, "guard failure must not raise credits")
			require.True(t, current.Disabled)
			require.Equal(t, tt.wantCauses, current.DisableCauses, "guard failure must not remove causes")
			require.Equal(t, tt.wantDeleted, current.Deleted)
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
