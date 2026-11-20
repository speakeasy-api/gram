package aitargets_test

// This golden was first generated from the registry-backed Defaults before it
// took a snapshot, which established equivalence. list_version and etag derive
// from the served built-in targets, so a scan list change needs `-args -update`.

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// compiledDefaultsGolden is the default scan list and what an organization
// with no rows is served.
type compiledDefaultsGolden struct {
	// Defaults are the default targets in order.
	Defaults []aitargets.Target `json:"defaults"`

	// ListVersion is the served list_version.
	ListVersion int32 `json:"list_version"`

	// ETag is the served ETag.
	ETag string `json:"etag"`

	// Envelope is the ai_scan envelope agents receive.
	Envelope map[string]any `json:"envelope"`
}

func defaultsGolden(t *testing.T, catalog *aivendors.Snapshot) []byte {
	t.Helper()
	snapshot := aitargets.NewOrganizationList(catalog, nil).Snapshot
	data, err := json.Marshal(compiledDefaultsGolden{
		Defaults:    aitargets.Defaults(catalog),
		ListVersion: snapshot.ListVersion,
		ETag:        snapshot.ETag,
		Envelope:    snapshot.EnvelopeValue(),
	})
	require.NoError(t, err)
	return data
}

func TestCompiledSnapshotServesTheRegistryScanList(t *testing.T) {
	t.Parallel()
	testenv.RequireJSONGolden(t, "testdata/defaults_golden.json", defaultsGolden(t, aivendorstest.CompiledSnapshot()))
}

// A freshly seeded database serves devices the same list, version and ETag.
// Only the order of Defaults and of each target's matcher lists changes; the
// envelope carries no matchers.
func TestDatabaseSnapshotServesTheRegistryScanList(t *testing.T) {
	t.Parallel()
	compiled := aivendorstest.CompiledSnapshot()
	database := aivendorstest.DatabaseSnapshot()
	want := aitargets.NewOrganizationList(compiled, nil).Snapshot
	got := aitargets.NewOrganizationList(database, nil).Snapshot
	require.Equal(t, want.ListVersion, got.ListVersion)
	require.Equal(t, want.ETag, got.ETag)
	require.Equal(t, want.EnvelopeValue(), got.EnvelopeValue())
	require.Equal(t, sortedMatchers(want.Targets()), sortedMatchers(got.Targets()))
	require.ElementsMatch(t, sortedMatchers(aitargets.Defaults(compiled)), sortedMatchers(aitargets.Defaults(database)))
	testenv.RequireJSONGolden(t, "testdata/defaults_database_order_golden.json", defaultsGolden(t, database))
}

func sortedMatchers(targets []aitargets.Target) []aitargets.Target {
	for i := range targets {
		slices.Sort(targets[i].GatewayClient.CIMDVendorKeys)
		slices.Sort(targets[i].GatewayClient.OAuthClientIDs)
		slices.Sort(targets[i].GatewayClient.ClientInfoNames)
	}
	return targets
}
