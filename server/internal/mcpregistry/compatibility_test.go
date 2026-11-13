package mcpregistry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
)

func TestCompatibilityPublicationNeverReselectsDefault(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	first, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	_, err = s.Save(ctx, first.ID, Token(first), json.RawMessage(strings.Replace(basicRecord, `"1.0.0"`, `"2.0.0"`, 1)))
	var invalid *InvalidError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "/server/version", invalid.Issues[0].Path)
	withdrawn, err := s.SetPublished(ctx, first.ID, Token(first), false)
	require.NoError(t, err)
	parent, err := s.GetServer(ctx, first.ServerID)
	require.NoError(t, err)
	require.Nil(t, parent.DefaultEntryID)
	_, err = s.SetPublished(ctx, first.ID, Token(withdrawn), true)
	require.NoError(t, err)
	parent, err = s.GetServer(ctx, first.ServerID)
	require.NoError(t, err)
	require.Nil(t, parent.DefaultEntryID)
	page, err := s.Discover(ctx, DiscoveryOptions{})
	require.NoError(t, err)
	require.Empty(t, page.Records)
	_, err = s.LookupDiscoveryVersion(ctx, parent.Name, "latest", false)
	require.ErrorIs(t, err, ErrNotFound)
	retained, err := s.GetByName(ctx, parent.Name)
	require.NoError(t, err)
	require.Equal(t, first.ID, retained.ID)
}

func TestCompatibilityHiddenAndUnlinkedDiscovery(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	first, err := s.Create(ctx, json.RawMessage(basicRecord))
	require.NoError(t, err)
	require.NoError(t, repo.New(db).HideRegistryServerFixture(ctx, first.ServerID))
	unlinkedID := uuid.New()
	raw := strings.Replace(basicRecord, "example.test/demo", "example.test/unlinked", 1)
	require.NoError(t, repo.New(db).InsertRegistryUnlinkedEntryFixture(ctx, repo.InsertRegistryUnlinkedEntryFixtureParams{ID: unlinkedID, Data: []byte(raw), Published: true}))
	page, err := s.Discover(ctx, DiscoveryOptions{})
	require.NoError(t, err)
	require.Empty(t, page.Records)
	for _, name := range []string{"example.test/demo", "example.test/unlinked"} {
		_, err = s.LookupDiscoveryVersion(ctx, name, "latest", false)
		require.ErrorIs(t, err, ErrNotFound)
	}
	exact, err := s.LookupDiscoveryVersion(ctx, "example.test/demo", "1.0.0", false)
	require.NoError(t, err)
	require.Equal(t, first.ID, exact.ID)
	retained, err := s.GetByName(ctx, "example.test/demo")
	require.NoError(t, err)
	require.Equal(t, first.ID, retained.ID)
	_, err = s.LookupDiscoveryVersion(ctx, "example.test/unlinked", "1.0.0", false)
	require.ErrorIs(t, err, ErrNotFound)
	unlinked, err := s.Get(ctx, unlinkedID)
	require.NoError(t, err)
	_, err = s.Save(ctx, unlinked.ID, Token(unlinked), json.RawMessage(raw))
	require.ErrorIs(t, err, ErrNotFound)
}
