package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type fakeLogsReadGrantsStore struct {
	principals []accessrepo.ListPrincipalsMissingScopeRow
	heldScopes []string
	inserted   []accessrepo.InsertPrincipalGrantIfAbsentParams
}

func (f *fakeLogsReadGrantsStore) ListPrincipalsMissingScope(_ context.Context, params accessrepo.ListPrincipalsMissingScopeParams) ([]accessrepo.ListPrincipalsMissingScopeRow, error) {
	f.heldScopes = params.HeldScopes
	return f.principals, nil
}

func (f *fakeLogsReadGrantsStore) InsertPrincipalGrantIfAbsent(_ context.Context, params accessrepo.InsertPrincipalGrantIfAbsentParams) (int64, error) {
	f.inserted = append(f.inserted, params)
	return 1, nil
}

func TestParseLogsReadGrantsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()

	cfg, err := parseLogsReadGrantsFlags([]string{"-environment=staging"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.False(t, cfg.apply)
}

func TestParseLogsReadGrantsFlagsApplyRequiresConfirmation(t *testing.T) {
	t.Parallel()

	_, err := parseLogsReadGrantsFlags([]string{"-apply", "-environment=staging"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.Error(t, err)
}

func TestBackfillLogsReadGrantsDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	store := &fakeLogsReadGrantsStore{
		principals: []accessrepo.ListPrincipalsMissingScopeRow{
			{OrganizationID: "org-a", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeRole, "global:00000000-0000-0000-0000-000000000001")},
			{OrganizationID: "org-a", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "user_01")},
		},
		heldScopes: nil,
		inserted:   nil,
	}

	report, err := backfillLogsReadGrants(t.Context(), store, false)
	require.NoError(t, err)
	require.Equal(t, 2, report.Principals)
	require.Equal(t, 2, report.GrantsAdded)
	require.Empty(t, store.inserted)
	require.ElementsMatch(t, logsReadLegacyScopes, store.heldScopes)
}

func TestBackfillLogsReadGrantsApplyInsertsUnrestrictedGrant(t *testing.T) {
	t.Parallel()

	store := &fakeLogsReadGrantsStore{
		principals: []accessrepo.ListPrincipalsMissingScopeRow{
			{OrganizationID: "org-a", PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeRole, "global:00000000-0000-0000-0000-000000000001")},
		},
		heldScopes: nil,
		inserted:   nil,
	}

	report, err := backfillLogsReadGrants(t.Context(), store, true)
	require.NoError(t, err)
	require.Equal(t, 1, report.GrantsAdded)
	require.Len(t, store.inserted, 1)

	grant := store.inserted[0]
	require.Equal(t, "org-a", grant.OrganizationID)
	require.Equal(t, "role:global:00000000-0000-0000-0000-000000000001", grant.PrincipalUrn.String())
	require.Equal(t, string(authz.ScopeLogsRead), grant.Scope)

	var selector map[string]string
	require.NoError(t, json.Unmarshal(grant.Selectors, &selector))
	require.Equal(t, map[string]string{
		authz.SelectorKeyResourceKind: authz.ResourceKindLogs,
		authz.SelectorKeyResourceID:   authz.WildcardResource,
	}, selector)
}
