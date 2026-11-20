package mcpconnectionattempts

// This golden was first generated from the registry-backed AppOf and AppLabel
// before they took a snapshot, which established equivalence. A registry
// change needs `-args -update`.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// goldenApp is the app a client_id resolves to and its daily-outcomes label.
type goldenApp struct {
	Key    string `json:"key"`
	Family string `json:"family"`
	Label  string `json:"label"`
}

// goldenAppCase is one client_id resolved as admitted and as not admitted.
type goldenAppCase struct {
	ClientID    string    `json:"client_id"`
	Admitted    goldenApp `json:"admitted"`
	NotAdmitted goldenApp `json:"not_admitted"`
}

// goldenLabel is the label one app_key gets.
type goldenLabel struct {
	AppKey string `json:"app_key"`
	Label  string `json:"label"`
}

func TestCompiledSnapshotNamesAppsAsTheRegistryDid(t *testing.T) {
	t.Parallel()

	catalog := aivendors.CompiledSnapshot()
	hosts := CatalogAppHosts(catalog)
	var ids, keys []string
	for _, preset := range catalog.Catalog() {
		ids = append(ids, strings.ReplaceAll(preset.URL, "*", "probe"))
		keys = append(keys, NormalizeProviderHost(preset.URL))
	}
	ids = append(ids, "", "my-client", "https://unknown.example.com/client.json", "https://claude.ai/other/client.json", "http://claude.ai/oauth/claude-code-client-metadata")
	keys = append(keys, "", "unknown.example.com", "claude.ai.example.com", AppDCR.String(), AppFirstParty.String(), AppOtherMetadataURL.String())

	app := func(a App) goldenApp { return goldenApp{Key: a.key, Family: a.family, Label: AppLabel(hosts, a.key)} }
	golden := struct {
		Apps   []goldenAppCase `json:"apps"`
		Labels []goldenLabel   `json:"labels"`
	}{Apps: nil, Labels: nil}
	for _, id := range ids {
		golden.Apps = append(golden.Apps, goldenAppCase{
			ClientID:    id,
			Admitted:    app(AppOf(catalog, id, ClientIDAdmitted)),
			NotAdmitted: app(AppOf(catalog, id, ClientIDNotAdmitted)),
		})
	}
	for _, key := range keys {
		golden.Labels = append(golden.Labels, goldenLabel{AppKey: key, Label: AppLabel(hosts, key)})
	}
	data, err := json.Marshal(golden)
	require.NoError(t, err)
	testenv.RequireJSONGolden(t, "testdata/app_golden.json", data)
}
