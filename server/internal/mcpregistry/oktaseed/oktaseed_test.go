package oktaseed_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/oktaseed"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatal(err)
	}
	infra = env
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

func TestVendorsAreValidRecords(t *testing.T) {
	t.Parallel()
	v, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	seen := map[string]string{}
	for _, vendor := range oktaseed.Vendors {
		require.NotEmpty(t, vendor.Remotes, vendor.Name)
		for _, name := range vendor.Mapping.OINNames {
			require.NotContains(t, seen, name, "OIN name claimed twice")
			seen[name] = vendor.Name
		}
	}
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "oktaseedtestdb")
	require.NoError(t, err)
	svc := mcpregistry.New(db, v)
	logger := testenv.NewLogger(t)

	// A dry run reports the creates and writes nothing.
	require.NotEmpty(t, oktaseed.Vendors)
	result, err := oktaseed.Apply(ctx, logger, svc, true)
	require.NoError(t, err)
	require.Equal(t, oktaseed.Result{Created: len(oktaseed.Vendors), Updated: 0, Unchanged: 0}, result)
	_, err = svc.GetByName(ctx, oktaseed.Vendors[0].Name)
	require.ErrorIs(t, err, mcpregistry.ErrNotFound)

	// A dry run reports the conflict a real create would hit.
	claim := `{"server":{"name":"example.test/squatter","description":"Squatter","version":"1","remotes":[{"type":"streamable-http","url":"https://mcp.example.test/mcp"}]},"_meta":{"com.speakeasy.ai/okta":{"oinNames":[` + strconv.Quote(oktaseed.Vendors[0].Mapping.OINNames[0]) + `]}}}`
	squatter, err := svc.Create(ctx, json.RawMessage(claim))
	require.NoError(t, err)
	_, err = oktaseed.Apply(ctx, logger, svc, true)
	var invalid *mcpregistry.InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, invalid.Issues[0].Message, "example.test/squatter")
	_, err = svc.Save(ctx, squatter.ID, mcpregistry.Token(squatter), json.RawMessage(`{"server":{"name":"example.test/squatter","description":"Squatter","version":"1","remotes":[{"type":"streamable-http","url":"https://mcp.example.test/mcp"}]}}`))
	require.NoError(t, err)

	result, err = oktaseed.Apply(ctx, logger, svc, false)
	require.NoError(t, err)
	require.Equal(t, oktaseed.Result{Created: len(oktaseed.Vendors), Updated: 0, Unchanged: 0}, result)
	for _, vendor := range oktaseed.Vendors {
		e, err := svc.GetByName(ctx, vendor.Name)
		require.NoError(t, err)
		require.Empty(t, v.ValidateStored(e.Data), vendor.Name)
		mapping, err := mcpregistry.ParseOktaMapping(e.Data)
		require.NoError(t, err)
		require.Equal(t, vendor.Mapping, mapping)
		if vendor.IconURL != "" {
			require.Contains(t, string(e.Data), vendor.IconURL)
		}
		recorded, _ := storedSupportsDCR(t, e.Data)
		require.Equal(t, vendor.SupportsDCR, recorded, vendor.Name)
	}

	// A second run changes nothing.
	result, err = oktaseed.Apply(ctx, logger, svc, false)
	require.NoError(t, err)
	require.Equal(t, oktaseed.Result{Created: 0, Updated: 0, Unchanged: len(oktaseed.Vendors)}, result)

	// Staff edits outside the namespace survive a re-run; a drifted mapping
	// is put back without touching them.
	first := oktaseed.Vendors[0]
	e, err := svc.GetByName(ctx, first.Name)
	require.NoError(t, err)
	var root map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(e.Data, &root))
	var meta map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(root["_meta"], &meta))
	meta["com.speakeasy.ai/catalog"] = json.RawMessage(`{"documentationUrl":"https://docs.example.test/edited"}`)
	meta[mcpregistry.OktaNamespace] = json.RawMessage(`{"oinNames":["drifted"]}`)
	rawMeta, err := json.Marshal(meta)
	require.NoError(t, err)
	root["_meta"] = rawMeta
	edited, err := json.Marshal(root)
	require.NoError(t, err)
	_, err = svc.Save(ctx, e.ID, mcpregistry.Token(e), edited)
	require.NoError(t, err)

	result, err = oktaseed.Apply(ctx, logger, svc, false)
	require.NoError(t, err)
	require.Equal(t, 1, result.Updated)
	e, err = svc.GetByName(ctx, first.Name)
	require.NoError(t, err)
	require.Contains(t, string(e.Data), "https://docs.example.test/edited")
	mapping, err := mcpregistry.ParseOktaMapping(e.Data)
	require.NoError(t, err)
	require.Equal(t, first.Mapping, mapping)
}

func TestApplyRepairsNullMetadata(t *testing.T) {
	t.Parallel()
	v, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "oktaseednulltestdb")
	require.NoError(t, err)
	svc := mcpregistry.New(db, v)

	// A legacy row that predates validation may hold "_meta": null; the seed
	// repairs it instead of panicking on a nil map.
	first := oktaseed.Vendors[0]
	remotes := make([]map[string]string, 0, len(first.Remotes))
	for _, r := range first.Remotes {
		remotes = append(remotes, map[string]string{"type": r.Type, "url": r.URL})
	}
	legacy, err := json.Marshal(map[string]any{
		"server": map[string]any{"name": first.Name, "description": "Legacy", "version": "1", "remotes": remotes},
		"_meta":  nil,
	})
	require.NoError(t, err)
	require.NoError(t, registryrepo.New(db).InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: legacy, Published: true}))

	result, err := oktaseed.Apply(ctx, testenv.NewLogger(t), svc, false)
	require.NoError(t, err)
	require.Equal(t, 1, result.Updated)
	e, err := svc.GetByName(ctx, first.Name)
	require.NoError(t, err)
	require.Empty(t, v.ValidateStored(e.Data))
	mapping, err := mcpregistry.ParseOktaMapping(e.Data)
	require.NoError(t, err)
	require.Equal(t, first.Mapping, mapping)
	require.Contains(t, string(e.Data), first.IconURL)
}

func TestApplyKeepsExistingIcon(t *testing.T) {
	t.Parallel()
	v, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "oktaseedicontestdb")
	require.NoError(t, err)
	svc := mcpregistry.New(db, v)

	first := oktaseed.Vendors[0]
	remotes := make([]map[string]string, 0, len(first.Remotes))
	for _, r := range first.Remotes {
		remotes = append(remotes, map[string]string{"type": r.Type, "url": r.URL})
	}
	const chosen = "https://icons.example.test/chosen.png"
	stored, err := json.Marshal(map[string]any{
		"server": map[string]any{"name": first.Name, "description": "Curated", "version": "1", "remotes": remotes, "icons": []map[string]string{{"src": chosen}}},
		"_meta":  map[string]any{mcpregistry.OktaNamespace: first.Mapping, "com.speakeasy.ai/catalog": map[string]any{"supportsDcr": false}},
	})
	require.NoError(t, err)
	_, err = svc.Create(ctx, stored)
	require.NoError(t, err)

	result, err := oktaseed.Apply(ctx, testenv.NewLogger(t), svc, false)
	require.NoError(t, err)
	require.Equal(t, 1, result.Unchanged)
	e, err := svc.GetByName(ctx, first.Name)
	require.NoError(t, err)
	require.Contains(t, string(e.Data), chosen)
	require.NotContains(t, string(e.Data), first.IconURL)
	require.True(t, first.SupportsDCR)
	recorded, present := storedSupportsDCR(t, e.Data)
	require.True(t, present)
	require.False(t, recorded)
}

func TestApplyRefusesUndecodableCatalogMetadata(t *testing.T) {
	t.Parallel()
	v, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "oktaseedcatalogtestdb")
	require.NoError(t, err)
	svc := mcpregistry.New(db, v)

	var vendor oktaseed.Vendor
	for _, candidate := range oktaseed.Vendors {
		if candidate.SupportsDCR {
			vendor = candidate
			break
		}
	}
	require.NotEmpty(t, vendor.Name)
	remotes := make([]map[string]string, 0, len(vendor.Remotes))
	for _, r := range vendor.Remotes {
		remotes = append(remotes, map[string]string{"type": r.Type, "url": r.URL})
	}
	// A legacy row whose catalog namespace is not an object is left alone
	// rather than replaced with only the seeded flag.
	legacy, err := json.Marshal(map[string]any{
		"server": map[string]any{"name": vendor.Name, "description": "Legacy", "version": "1", "remotes": remotes},
		"_meta":  map[string]any{"com.speakeasy.ai/catalog": "kept"},
	})
	require.NoError(t, err)
	require.NoError(t, registryrepo.New(db).InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: legacy, Published: true}))

	_, err = oktaseed.Apply(ctx, testenv.NewLogger(t), svc, false)
	require.ErrorContains(t, err, "decode catalog metadata")
	e, err := svc.GetByName(ctx, vendor.Name)
	require.NoError(t, err)
	require.JSONEq(t, string(legacy), string(e.Data))
}

// storedSupportsDCR reads the catalog flag and whether the record sets it.
func storedSupportsDCR(t *testing.T, data json.RawMessage) (bool, bool) {
	t.Helper()
	var root struct {
		Meta map[string]map[string]json.RawMessage `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(data, &root))
	raw, ok := root.Meta["com.speakeasy.ai/catalog"]["supportsDcr"]
	if !ok {
		return false, false
	}
	var value bool
	require.NoError(t, json.Unmarshal(raw, &value))
	return value, true
}
