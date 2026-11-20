package usersessions_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
)

// listedPreset is a listPresets item under the presets golden's field names.
type listedPreset struct {
	URL         string `json:"url"`
	VendorKey   string `json:"vendor_key"`
	DisplayName string `json:"display_name"`
	Enabled     bool   `json:"enabled"`
	Pattern     bool   `json:"pattern"`
}

// The endpoint lists the injected catalog's presets in the registry's order.
func TestListPresetsReturnsTheRegistryPresetsInOrder(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	result, err := ti.service.ListPresets(ctx, &gen.ListPresetsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	got := make([]listedPreset, 0, len(result.Items))
	for _, item := range result.Items {
		got = append(got, listedPreset{URL: item.ClientIDMetadataURI, VendorKey: item.VendorKey, DisplayName: item.DisplayName, Enabled: item.Enabled, Pattern: item.IsPattern})
	}

	data, err := os.ReadFile("cimd/admission/testdata/presets_golden.json")
	require.NoError(t, err)
	var want []listedPreset
	require.NoError(t, json.Unmarshal(data, &want))
	require.Equal(t, want, got)
	for _, preset := range want {
		require.Equal(t, strings.Contains(preset.URL, "*"), preset.Pattern, preset.URL)
	}
}
