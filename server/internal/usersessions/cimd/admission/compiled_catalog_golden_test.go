package admission

// These goldens were first generated from the registry-backed admission code
// before it took a snapshot, which established equivalence. A registry change
// needs `-args -update`.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// goldenPreset is a preset as the admission catalog lists it.
type goldenPreset struct {
	URL         string `json:"url"`
	VendorKey   string `json:"vendor_key"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	Pattern     bool   `json:"pattern"`
}

// goldenDecision is a Decision under the field names the golden was recorded with.
type goldenDecision struct {
	Outcome Outcome      `json:"Outcome"`
	Admit   AdmitReason  `json:"Admit"`
	Denial  DenialReason `json:"Denial"`
}

func toGoldenDecision(d Decision) goldenDecision {
	return goldenDecision(d)
}

// goldenCase is one client_id and what admission decides about it.
type goldenCase struct {
	ClientID    string                  `json:"client_id"`
	MatchReason AdmitReason             `json:"match_reason"`
	Matched     bool                    `json:"matched"`
	Preset      *goldenPreset           `json:"preset"`
	Shadow      goldenDecision          `json:"shadow"`
	Evaluate    map[Mode]goldenDecision `json:"evaluate"`
}

func toGoldenPreset(preset Preset) goldenPreset {
	return goldenPreset{URL: preset.URL, VendorKey: preset.VendorKey, DisplayName: preset.DisplayName, Enabled: preset.Enabled, Pattern: preset.IsPattern()}
}

// goldenClientIDs are every catalog document URL, variants of each, wildcard
// segment probes, and inputs admission must refuse.
func goldenClientIDs(catalog *aivendors.Snapshot) []string {
	var ids []string
	seen := map[string]struct{}{}
	add := func(id string) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	segments := []string{"a", "abc-123_XYZ", "AbC0", "", "a/b", "a%2Fb", "..", ".", "a.b", "a?b", "a#b", "a b", strings.Repeat("z", 64), strings.Repeat("y", 300), "codex"}
	for _, preset := range catalog.Catalog() {
		u := preset.URL
		add(u)
		add(u + "/")
		add(u + "?x=1")
		add(u + "#frag")
		add(strings.ToUpper(u))
		add(strings.Replace(u, "https://", "http://", 1))
		add(strings.Replace(u, "https://", "https://www.", 1))
		add(strings.TrimSuffix(u, "/") + "x")
		add(u[:len(u)-1])
		if i := strings.Index(u[len("https://"):], "/"); i >= 0 {
			cut := len("https://") + i
			add(u[:cut] + "/" + u[cut:])
			add(u[:cut] + ":443" + u[cut:])
			add(strings.ToUpper(u[:cut]) + u[cut:])
			add(u[:cut] + strings.ToUpper(u[cut:]))
		}
		if strings.Contains(u, "*") {
			for _, segment := range segments {
				add(strings.Replace(u, "*", segment, 1))
			}
		}
	}
	add("")
	add("not-a-url")
	add("my-client")
	add("https://unknown.example.com/client.json")
	add("https://client.example.com/" + strings.Repeat("a", MaxClientIDLength))
	add("https://chatgpt.com/" + strings.Repeat("a", MaxClientIDLength))
	add("https://chatgpt.com/oauth/" + strings.Repeat("a", 3000) + "/client.json")
	return ids
}

func TestCompiledSnapshotAdmitsAsTheRegistryDid(t *testing.T) {
	t.Parallel()

	catalog := aivendorstest.CompiledSnapshot()
	ids := goldenClientIDs(catalog)
	cases := make([]goldenCase, 0, len(ids))
	for _, id := range ids {
		reason, matched := CatalogMatch(catalog, id)
		tc := goldenCase{
			ClientID:    id,
			MatchReason: reason,
			Matched:     matched,
			Preset:      nil,
			Shadow:      toGoldenDecision(EvaluateShadow(catalog, id)),
			Evaluate:    make(map[Mode]goldenDecision, 2),
		}
		// The other modes never consult the catalog.
		for _, mode := range []Mode{ModePresets, ModeReporting} {
			tc.Evaluate[mode] = toGoldenDecision(Evaluate(catalog, mode, id))
		}
		if preset, ok := catalog.CatalogPreset(id); ok {
			tc.Preset = new(toGoldenPreset(preset))
		}
		cases = append(cases, tc)
	}
	data, err := json.Marshal(cases)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/admission_golden.json", data)
}

// The presets listing, and the inbound EMA list built from it, keep the
// registry's entries and order.
func TestCompiledSnapshotListsPresetsAsTheRegistryDid(t *testing.T) {
	t.Parallel()

	presets := aivendorstest.CompiledSnapshot().Catalog()
	listing := make([]goldenPreset, 0, len(presets))
	for _, preset := range presets {
		listing = append(listing, toGoldenPreset(preset))
	}
	data, err := json.Marshal(listing)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/presets_golden.json", data)
}
