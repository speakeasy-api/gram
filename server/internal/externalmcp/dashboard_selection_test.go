package externalmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardKeepsLegacyToolDeclarationsOpaque(t *testing.T) {
	t.Parallel()
	body := []byte(`{"server":{"remotes":[{"type":"sse","url":"https://example.com/first"},{"type":"sse","url":"https://example.com/last"}]},"_meta":{"com.pulsemcp/server-version":{"remotes[0]":{"tools":[{"name":"first"}]},"remotes[1]":{"tools":[{"name":"last"}]}}}}`)
	details, err := decodeDashboardDetails(body)
	require.NoError(t, err)
	require.Len(t, details.Remotes, 2)

	var record struct {
		Meta map[string]any `json:"_meta"`
	}
	require.NoError(t, json.Unmarshal(body, &record))
	require.Equal(t, record.Meta, details.Meta)
}
