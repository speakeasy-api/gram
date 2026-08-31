package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type fakeProOrganizationTx struct {
	isPro      bool
	seeded     bool
	committed  bool
	rolledBack bool
}

func (f *fakeProOrganizationTx) LockAndCheckPro(context.Context, string) (bool, error) {
	return f.isPro, nil
}
func (f *fakeProOrganizationTx) SeedEntitlements(context.Context, string) ([]productfeatures.Feature, error) {
	f.seeded = true
	return []productfeatures.Feature{productfeatures.FeatureLogs, productfeatures.FeatureSSO}, nil
}
func (f *fakeProOrganizationTx) Commit(context.Context) error   { f.committed = true; return nil }
func (f *fakeProOrganizationTx) Rollback(context.Context) error { f.rolledBack = true; return nil }

func TestParseProEntitlementsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseProEntitlementsFlags([]string{"-environment=staging"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.False(t, cfg.apply)
}

func TestMigrateProOrganizationDryRunSeedsThenRollsBack(t *testing.T) {
	t.Parallel()
	tx := &fakeProOrganizationTx{isPro: true}
	added, err := migrateProOrganization(t.Context(), tx, "org-a", false)
	require.NoError(t, err)
	require.Equal(t, 2, added)
	require.True(t, tx.seeded)
	require.True(t, tx.rolledBack)
	require.False(t, tx.committed)
}

func TestMigrateProOrganizationSkipsOrganizationNoLongerPro(t *testing.T) {
	t.Parallel()
	tx := &fakeProOrganizationTx{isPro: false}
	added, err := migrateProOrganization(t.Context(), tx, "org-a", true)
	require.NoError(t, err)
	require.Zero(t, added)
	require.False(t, tx.seeded)
	require.True(t, tx.rolledBack)
	require.False(t, tx.committed)
}

func TestBackfillProOrganizationsDryRunSeedsProspectively(t *testing.T) {
	t.Parallel()
	var seeded []string
	report, err := backfillProOrganizations([]string{"org-a", "org-b"}, false, func(id string, apply bool) (int, error) {
		require.False(t, apply)
		seeded = append(seeded, id)
		return 2, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"org-a", "org-b"}, seeded)
	require.Equal(t, proEntitlementsReport{Organizations: 2, FeaturesAdded: 4}, report)
}

func TestBackfillProOrganizationsAppliesEachOrganization(t *testing.T) {
	t.Parallel()
	var seeded []string
	report, err := backfillProOrganizations([]string{"org-a", "org-b"}, true, func(id string, apply bool) (int, error) {
		require.True(t, apply)
		seeded = append(seeded, id)
		return 3, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"org-a", "org-b"}, seeded)
	require.Equal(t, proEntitlementsReport{Organizations: 2, FeaturesAdded: 6}, report)
}

func TestParseProEntitlementsFlagsRequiresWriteConfirmations(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseProEntitlementsFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseProEntitlementsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseProEntitlementsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err := parseProEntitlementsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=pro-entitlements"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
}
