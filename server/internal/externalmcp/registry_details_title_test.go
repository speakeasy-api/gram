package externalmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Server details keep the registry's human-readable title beside its
// reverse-DNS name, so a server added from the catalog is named "Linear".
func TestServerDetailsKeepTheRegistryTitle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ body, want string }{
		{body: `{"server":{"name":"app.linear/mcp","title":"Linear","remotes":[{"type":"streamable-http","url":"https://mcp.linear.app/mcp"}]}}`, want: "Linear"},
		{body: `{"server":{"name":"app.linear/mcp","remotes":[{"type":"streamable-http","url":"https://mcp.linear.app/mcp"}]}}`, want: ""},
	} {
		var entry serverDetailsEntry
		require.NoError(t, json.Unmarshal([]byte(tc.body), &entry))
		details := projectServerDetails(entry, nil)
		require.Equal(t, "app.linear/mcp", details.Name)
		require.Equal(t, tc.want, details.Title)
	}
}
