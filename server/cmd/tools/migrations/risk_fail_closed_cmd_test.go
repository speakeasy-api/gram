package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type fakeRiskFailClosedTx struct {
	rowsAffected int64
	execs        int
	committed    bool
	rolledBack   bool
}

func (f *fakeRiskFailClosedTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	f.execs++
	return pgconn.NewCommandTag(fmt.Sprintf("INSERT 0 %d", f.rowsAffected)), nil
}

func (f *fakeRiskFailClosedTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (f *fakeRiskFailClosedTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return nil
}

func (f *fakeRiskFailClosedTx) Commit(context.Context) error   { f.committed = true; return nil }
func (f *fakeRiskFailClosedTx) Rollback(context.Context) error { f.rolledBack = true; return nil }

func TestParseRiskFailClosedFlagsRequiresItsOwnApplyConfirmation(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	cfg, err := parseConfirmedBackfillFlags(riskFailClosedBackfillName, []string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply)

	_, err = parseConfirmedBackfillFlags(riskFailClosedBackfillName, []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=pro-entitlements"}, getenv)
	require.ErrorContains(t, err, "-confirm-apply=risk-fail-closed")

	cfg, err = parseConfirmedBackfillFlags(riskFailClosedBackfillName, []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=risk-fail-closed"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
}

func TestSeedRiskFailClosedDryRunRollsBack(t *testing.T) {
	t.Parallel()
	tx := &fakeRiskFailClosedTx{rowsAffected: 1}
	added, err := seedRiskFailClosed(t.Context(), tx, "org-a", false)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, 1, tx.execs)
	require.True(t, tx.rolledBack)
	require.False(t, tx.committed)
}

func TestSeedRiskFailClosedApplyCommits(t *testing.T) {
	t.Parallel()
	tx := &fakeRiskFailClosedTx{rowsAffected: 1}
	added, err := seedRiskFailClosed(t.Context(), tx, "org-a", true)
	require.NoError(t, err)
	require.True(t, added)
	require.True(t, tx.committed)
	require.False(t, tx.rolledBack)
}

func TestSeedRiskFailClosedKeepsAnExistingChoice(t *testing.T) {
	t.Parallel()
	tx := &fakeRiskFailClosedTx{rowsAffected: 0}
	added, err := seedRiskFailClosed(t.Context(), tx, "org-a", true)
	require.NoError(t, err)
	require.False(t, added)
}

func TestBackfillRiskFailClosedOrganizationsCountsMixedAndAdded(t *testing.T) {
	t.Parallel()
	var seeded []string
	report, err := backfillRiskFailClosedOrganizations([]riskFailClosedCandidate{
		{OrganizationID: "org-a", HasFailOpenPolicy: false},
		{OrganizationID: "org-b", HasFailOpenPolicy: true},
		{OrganizationID: "org-c", HasFailOpenPolicy: false},
	}, true, func(id string, apply bool) (bool, error) {
		require.True(t, apply)
		seeded = append(seeded, id)
		return id != "org-c", nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"org-a", "org-b", "org-c"}, seeded)
	require.Equal(t, riskFailClosedReport{Organizations: 3, MixedOrganizations: 1, FeaturesAdded: 2}, report)
}
