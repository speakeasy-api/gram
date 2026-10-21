package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

func TestParseAuthPermissionDefaultsFlags(t *testing.T) {
	t.Parallel()
	env := func(string) string { return "postgres://u:p@db.example.internal:5432/gram" }

	cfg, err := parseAuthPermissionDefaultsFlags([]string{"-environment=staging"}, env)
	require.NoError(t, err)
	require.False(t, cfg.apply, "dry run by default")

	_, err = parseAuthPermissionDefaultsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.example.internal:5432/gram"}, env)
	require.Error(t, err, "apply needs -confirm-apply")

	cfg, err = parseAuthPermissionDefaultsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.example.internal:5432/gram", "-confirm-apply=auth-permission-defaults"}, env)
	require.NoError(t, err)
	require.True(t, cfg.apply)

	_, err = parseAuthPermissionDefaultsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other:5432/gram", "-confirm-apply=auth-permission-defaults"}, env)
	require.Error(t, err, "the target must match the database")
}

func TestBackfillAuthPermissionDefaultsAggregates(t *testing.T) {
	t.Parallel()
	report := func(kinds map[authclientbackfill.GrantKind]int, written int, waiting bool, failed bool) authclientbackfill.Report {
		r := authclientbackfill.Report{PendingGrantsByKind: kinds, GrantsWritten: written, APIKeysWaiting: waiting, PhaseErrors: map[string]string{}}
		if failed {
			r.PhaseErrors[authclientbackfill.PhaseGrants] = "boom"
		}
		return r
	}
	results := map[string]authclientbackfill.Report{
		"a": report(map[authclientbackfill.GrantKind]int{authclientbackfill.GrantPersonalDefault: 1, authclientbackfill.GrantSharedClient: 0, authclientbackfill.GrantAPIKey: 2}, 3, false, false),
		"b": report(map[authclientbackfill.GrantKind]int{authclientbackfill.GrantPersonalDefault: 0, authclientbackfill.GrantSharedClient: 4, authclientbackfill.GrantAPIKey: 0}, 4, true, false),
		"c": report(nil, 0, false, true),
	}
	results["e"] = report(map[authclientbackfill.GrantKind]int{authclientbackfill.GrantPersonalDefault: 1, authclientbackfill.GrantSharedClient: 0, authclientbackfill.GrantAPIKey: 0}, 1, true, false)
	results["e"].PhaseErrors[authclientbackfill.PhaseEMA] = "readiness check failed"
	got, err := backfillAuthPermissionDefaults([]string{"a", "b", "c", "d", "e"}, func(id string) (authclientbackfill.Report, error) {
		if id == "d" {
			return authclientbackfill.Report{}, authclientbackfill.ErrRunInProgress
		}
		return results[id], nil
	}, func(int, int) {})
	require.NoError(t, err)
	require.Equal(t, 5, got.Organizations)
	require.Equal(t, map[authclientbackfill.GrantKind]int{authclientbackfill.GrantPersonalDefault: 2, authclientbackfill.GrantAPIKey: 2, authclientbackfill.GrantSharedClient: 4}, got.Granted)
	require.Equal(t, 8, got.Written, "a readiness-check error does not undo committed grants")
	require.Equal(t, 2, got.KeysWaiting)
	require.Equal(t, 1, got.Failed)
	require.Equal(t, 1, got.Busy)

	_, err = backfillAuthPermissionDefaults([]string{"a"}, func(string) (authclientbackfill.Report, error) {
		return authclientbackfill.Report{}, errors.New("database down")
	}, func(int, int) {})
	require.Error(t, err)
}
