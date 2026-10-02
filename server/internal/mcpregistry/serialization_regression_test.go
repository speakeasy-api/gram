package mcpregistry

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAcceptedInputSurvivesJSONBSerialization(t *testing.T) {
	t.Parallel()
	ctx, s, _ := newTestService(t)
	prefix := `{"server":{"name":"io.example/serialization","description":"test","version":"1"},"_meta":{"io.example/private":{"integer":9007199254740993,"decimal":1.234567890123456789}},"extension":"`
	suffix := `"}`
	raw := []byte(prefix + strings.Repeat("x", (8<<20)-1024-len(prefix)-len(suffix)) + suffix)
	created, err := s.Create(ctx, raw)
	require.NoError(t, err)
	got, err := s.Get(ctx, created.ID)
	require.NoError(t, err)
	t.Logf("input=%d stored_jsonb_text=%d", len(raw), len(got.Data))
	require.LessOrEqual(t, len(got.Data), StoredRecordByteLimit)
	require.Contains(t, string(got.Data), "9007199254740993")
	require.Contains(t, string(got.Data), "1.234567890123456789")
	page, err := s.List(ctx, ListOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	assert.Empty(t, page.Entries[0].Issues, "accepted record must not become invalid due to JSONB spaces")
	unpublished, err := s.SetPublished(ctx, got.ID, Token(got), false)
	require.NoError(t, err)
	_, err = s.SetPublished(ctx, got.ID, Token(unpublished), true)
	require.NoError(t, err, "accepted record must remain publishable")
	// Create's published default also exercises discovery independently of republish.
	other := strings.Replace(string(raw), "io.example/serialization", "io.example/discoverable", 1)
	_, err = s.Create(ctx, []byte(other))
	require.NoError(t, err)
	discovered, err := s.Discover(ctx, DiscoveryOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, discovered.Records, 2, "accepted published record must survive discovery validation")
	for _, record := range discovered.Records {
		require.Contains(t, string(record), "9007199254740993")
		require.Contains(t, string(record), "1.234567890123456789")
	}
}
