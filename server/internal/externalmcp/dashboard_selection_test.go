package externalmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardSSESelectionCompatibility(t *testing.T) {
	t.Parallel()
	body := []byte(`{"server":{"remotes":[{"type":"sse","url":"https://example.com/first"},{"type":"sse","url":"https://example.com/last"}]},"_meta":{"com.pulsemcp/server-version":{"remotes[0]":{"tools":[{"name":"first"}]},"remotes[1]":{"tools":[{"name":"last"}]}}}}`)
	for _, tc := range []struct {
		name   string
		native bool
		tool   string
	}{{"native", true, "last"}, {"legacy", false, "first"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			details, err := decodeDashboardDetails(body, tc.native)
			require.NoError(t, err)
			require.Len(t, details.Remotes, 2)
			require.Len(t, details.Tools, 1)
			require.Equal(t, tc.tool, *details.Tools[0].Name)
		})
	}
}
