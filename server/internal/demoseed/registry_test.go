//go:build demoseed_safety

package demoseed

import (
	"encoding/json"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestRegistrySurvivesLocalReseed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "registryseeddb")
	require.NoError(t, err)
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	s := mcpregistry.New(db, validator)
	// A fresh deployment needs no starter import, and tenant seeding leaves it empty.
	seedLocalPostgres(ctx, t, db, LocalSpec())
	blob := assetstest.NewTestBlobStore(t)
	require.NoError(t, RunLocalFixtures(ctx, testenv.NewLogger(t), db, blob, nil, LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local"}))
	page, err := s.List(ctx, mcpregistry.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, page.Entries)
	const raw = `{"server":{"name":"example.test/seed-one","description":"Original","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`
	first, err := s.Create(ctx, json.RawMessage(raw))
	require.NoError(t, err)
	const other = `{"server":{"name":"example.test/seed-two","description":"Original","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`
	second, err := s.Create(ctx, json.RawMessage(other))
	require.NoError(t, err)
	second, err = s.Save(ctx, second.ID, mcpregistry.Token(second), json.RawMessage(`{"server":{"name":"example.test/seed-two","description":"Staff edit","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]}}`))
	require.NoError(t, err)
	second, err = s.SetPublished(ctx, second.ID, mcpregistry.Token(second), false)
	require.NoError(t, err)
	for range 2 {
		seedLocalPostgres(ctx, t, db, LocalSpec())
		require.NoError(t, RunLocalFixtures(ctx, testenv.NewLogger(t), db, blob, nil, LocalFixturesOptions{DeveloperEmail: testDeveloperEmail, Environment: "local"}))
		got, err := s.Get(ctx, first.ID)
		require.NoError(t, err)
		require.Equal(t, first, got)
		got, err = s.Get(ctx, second.ID)
		require.NoError(t, err)
		require.Equal(t, second, got)
	}
}
