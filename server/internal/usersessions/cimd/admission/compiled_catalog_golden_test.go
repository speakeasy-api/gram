package admission

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// goldenPreset is a preset as the registry-backed admission catalog listed it.
type goldenPreset struct {
	URL         string `json:"url"`
	VendorKey   string `json:"vendor_key"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	Pattern     bool   `json:"pattern"`
}

// goldenCase is one client_id and what the registry-backed admission decided.
type goldenCase struct {
	ClientID    string            `json:"client_id"`
	MatchReason AdmitReason       `json:"match_reason"`
	Matched     bool              `json:"matched"`
	Preset      *goldenPreset     `json:"preset"`
	Shadow      Decision          `json:"shadow"`
	Evaluate    map[Mode]Decision `json:"evaluate"`
}

func readGolden(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, v))
}

// The goldens were recorded from the registry-backed admission functions
// before they took a snapshot, so the compiled snapshot must reproduce them.
func TestCompiledSnapshotAdmitsAsTheRegistryDid(t *testing.T) {
	t.Parallel()

	var cases []goldenCase
	readGolden(t, "testdata/admission_golden.json", &cases)
	require.NotEmpty(t, cases)

	catalog := aivendors.CompiledSnapshot()
	probed := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		probed[tc.ClientID] = struct{}{}

		reason, ok := CatalogMatch(catalog, tc.ClientID)
		require.Equalf(t, tc.Matched, ok, "match %q", tc.ClientID)
		require.Equalf(t, tc.MatchReason, reason, "match reason %q", tc.ClientID)

		preset, ok := catalog.CatalogPreset(tc.ClientID)
		if tc.Preset == nil {
			require.Falsef(t, ok, "preset %q", tc.ClientID)
		} else {
			require.Truef(t, ok, "preset %q", tc.ClientID)
			got := goldenPreset{URL: preset.URL, VendorKey: preset.VendorKey, DisplayName: preset.DisplayName, Enabled: preset.Enabled, Pattern: preset.IsPattern()}
			require.Equalf(t, tc.Preset, &got, "preset %q", tc.ClientID)
		}

		require.Equalf(t, tc.Shadow, EvaluateShadow(catalog, tc.ClientID), "shadow %q", tc.ClientID)
		require.Lenf(t, tc.Evaluate, 2, "evaluate %q", tc.ClientID)
		for mode, want := range tc.Evaluate {
			require.Equalf(t, want, Evaluate(catalog, mode, tc.ClientID), "evaluate %s %q", mode, tc.ClientID)
		}
	}

	for _, preset := range catalog.Catalog() {
		require.Containsf(t, probed, preset.URL, "the golden must probe every catalog document")
	}
}

// The presets listing, and the inbound EMA list built from it, keep the
// registry's entries and order.
func TestCompiledSnapshotListsPresetsAsTheRegistryDid(t *testing.T) {
	t.Parallel()

	var want []goldenPreset
	readGolden(t, "testdata/presets_golden.json", &want)

	presets := aivendors.CompiledSnapshot().Catalog()
	got := make([]goldenPreset, 0, len(presets))
	for _, preset := range presets {
		got = append(got, goldenPreset{URL: preset.URL, VendorKey: preset.VendorKey, DisplayName: preset.DisplayName, Enabled: preset.Enabled, Pattern: preset.IsPattern()})
	}
	require.Equal(t, want, got)
}
