package oktaseed_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/oktaseed"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
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

	result, err := oktaseed.Apply(ctx, logger, svc)
	require.NoError(t, err)
	require.Equal(t, oktaseed.Result{Created: len(oktaseed.Vendors), Updated: 0, Unchanged: 0}, result)
	for _, vendor := range oktaseed.Vendors {
		e, err := svc.GetByName(ctx, vendor.Name)
		require.NoError(t, err)
		require.Empty(t, v.ValidateStored(e.Data), vendor.Name)
		mapping, err := mcpregistry.ParseOktaMapping(e.Data)
		require.NoError(t, err)
		require.Equal(t, vendor.Mapping, mapping)
	}

	// A second run changes nothing.
	result, err = oktaseed.Apply(ctx, logger, svc)
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

	result, err = oktaseed.Apply(ctx, logger, svc)
	require.NoError(t, err)
	require.Equal(t, 1, result.Updated)
	e, err = svc.GetByName(ctx, first.Name)
	require.NoError(t, err)
	require.Contains(t, string(e.Data), "https://docs.example.test/edited")
	mapping, err := mcpregistry.ParseOktaMapping(e.Data)
	require.NoError(t, err)
	require.Equal(t, first.Mapping, mapping)
}
