package externalmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardDetailsPreferLastSSERemote(t *testing.T) {
	t.Parallel()
	body := []byte(`{"server":{"remotes":[{"type":"sse","url":"https://example.com/first"},{"type":"sse","url":"https://example.com/last"}]},"_meta":{"com.pulsemcp/server-version":{"remotes[0]":{"tools":[{"name":"first"}]},"remotes[1]":{"tools":[{"name":"last"}]}}}}`)
	details, err := decodeDashboardDetails(body)
	require.NoError(t, err)
	require.Len(t, details.Remotes, 2)
	require.Len(t, details.Tools, 1)
	require.Equal(t, "last", *details.Tools[0].Name)
}
