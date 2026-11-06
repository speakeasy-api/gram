package mcpregistry

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRefreshDecision(t *testing.T) {
	t.Parallel()
	a := json.RawMessage(`{"server":{"name":"example.test/demo","description":"old"}}`)
	b := json.RawMessage(`{"server":{"name":"example.test/demo","description":"new"}}`)
	c := json.RawMessage(`{"server":{"name":"example.test/demo","description":"local"}}`)
	for _, tc := range []struct {
		name                  string
		base, local, upstream json.RawMessage
		published             bool
		want                  string
	}{
		{"create", nil, nil, a, false, "create"},
		{"adopt", nil, a, a, true, "adopt"},
		{"unowned", nil, a, b, true, "conflict"},
		{"update", a, a, b, true, "update"},
		{"local edit", a, c, b, true, "conflict"},
		{"converged", a, b, b, true, "adopt"},
		{"remove", a, a, nil, true, "unpublish"},
		{"remove edited", a, c, nil, true, "conflict"},
		{"local publication", a, a, b, false, "conflict"},
		{"missing local", a, nil, b, false, "conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			action, _ := refreshDecision(tc.base, tc.local, tc.upstream, tc.published)
			require.Equal(t, tc.want, action)
		})
	}
}

func TestRefreshPreservesLocalChangesWhenUpstreamUnchanged(t *testing.T) {
	t.Parallel()
	base := json.RawMessage(`{"server":{"name":"example.test/demo","description":"old"}}`)
	local := json.RawMessage(`{"server":{"name":"example.test/demo","description":"local"}}`)
	for _, published := range []bool{true, false} {
		action, _ := refreshDecision(base, local, base, published)
		require.Equal(t, "preserved", action)
	}
	action, _ := refreshDecision(nil, local, nil, true)
	require.Equal(t, "preserved", action)
}

func TestRefreshSemanticTimestampsAndAssets(t *testing.T) {
	t.Parallel()
	read := func(context.Context, string) ([]byte, error) { return []byte("same bytes"), nil }
	first := json.RawMessage(`{"server":{"name":"example.test/demo"},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"deleted","updatedAt":"2024-01-01T00:00:00Z"},"com.speakeasy.ai/registry":{"publishedAt":"2024-01-01T00:00:00Z"},"com.speakeasy.ai/icons":{"default":{"src":"https://first.test/icon","mimeType":"image/svg+xml","sizes":["any"]}}}}`)
	second := json.RawMessage(strings.ReplaceAll(strings.ReplaceAll(string(first), "2024-01-01", "2025-01-01"), "first.test", "second.test"))
	a, err := refreshSemantic(t.Context(), first, read)
	require.NoError(t, err)
	b, err := refreshSemantic(t.Context(), second, read)
	require.NoError(t, err)
	require.Equal(t, a, b)
	require.Contains(t, string(a), `"status":"deleted"`)
	active := json.RawMessage(strings.Replace(string(second), "deleted", "active", 1))
	c, err := refreshSemantic(t.Context(), active, read)
	require.NoError(t, err)
	require.NotEqual(t, a, c)
}
