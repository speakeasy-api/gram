package aitargets_test

// This golden was first generated from the registry-backed Defaults before it
// took a snapshot, which established equivalence. A registry change needs
// `-args -update`.

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
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

func TestCompiledSnapshotServesTheRegistryScanList(t *testing.T) {
	t.Parallel()

	defaults := aitargets.Defaults(aivendors.CompiledSnapshot())
	snapshot := aitargets.NewSnapshot(aitargets.ListVersion(), aitargets.Served(aitargets.Overlay(defaults, nil)))
	data, err := json.Marshal(compiledDefaultsGolden{
		Defaults:    defaults,
		ListVersion: aitargets.ListVersion(),
		ETag:        snapshot.ETag,
		Envelope:    snapshot.EnvelopeValue(),
	})
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/defaults_golden.json", data)
}
