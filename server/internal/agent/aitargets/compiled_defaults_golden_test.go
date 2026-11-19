package aitargets_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// compiledDefaultsGolden is the registry-backed scan list recorded before
// Defaults took a snapshot.
type compiledDefaultsGolden struct {
	// Defaults are the default targets in order.
	Defaults json.RawMessage `json:"defaults"`

	// ListVersion is the served list_version.
	ListVersion int32 `json:"list_version"`

	// ETag is the served ETag for an organization with no rows.
	ETag string `json:"etag"`

	// Envelope is the ai_scan envelope for an organization with no rows.
	Envelope map[string]any `json:"envelope"`
}

func TestCompiledSnapshotServesTheRegistryScanList(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/defaults_golden.json")
	require.NoError(t, err)
	var golden compiledDefaultsGolden
	require.NoError(t, json.Unmarshal(data, &golden))

	defaults := aitargets.Defaults(aivendors.CompiledSnapshot())
	encoded, err := json.Marshal(defaults)
	require.NoError(t, err)
	require.JSONEq(t, string(golden.Defaults), string(encoded))

	require.Equal(t, golden.ListVersion, aitargets.ListVersion())
	snapshot := aitargets.NewSnapshot(aitargets.ListVersion(), aitargets.Served(aitargets.Overlay(defaults, nil)))
	require.Equal(t, golden.ETag, snapshot.ETag)
	require.Equal(t, golden.Envelope, snapshot.EnvelopeValue())
}
