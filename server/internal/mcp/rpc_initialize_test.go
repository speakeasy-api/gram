package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseInitializeParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		raw              string
		wantValid        bool
		wantProtocol     string
		wantClientName   string
		wantClientVer    string
		wantCapabilities []string
	}{
		{
			name:             "empty params",
			raw:              "",
			wantValid:        true,
			wantCapabilities: nil,
		},
		{
			name:             "full params with sorted capabilities",
			raw:              `{"protocolVersion":"2025-03-26","clientInfo":{"name":"cursor","version":"1.2.3"},"capabilities":{"tools":{},"roots":{},"sampling":{}}}`,
			wantValid:        true,
			wantProtocol:     "2025-03-26",
			wantClientName:   "cursor",
			wantClientVer:    "1.2.3",
			wantCapabilities: []string{"roots", "sampling", "tools"},
		},
		{
			name:             "no capabilities",
			raw:              `{"protocolVersion":"2025-03-26","clientInfo":{"name":"claude","version":"1.0.0"}}`,
			wantValid:        true,
			wantProtocol:     "2025-03-26",
			wantClientName:   "claude",
			wantClientVer:    "1.0.0",
			wantCapabilities: nil,
		},
		{
			name:      "malformed params (array instead of object)",
			raw:       `["not","an","object"]`,
			wantValid: false,
		},
		{
			name:      "malformed params (invalid json)",
			raw:       `{not json`,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			params, capabilities, err := parseInitializeParams(json.RawMessage(tt.raw))
			if !tt.wantValid {
				require.Error(t, err)
				require.Empty(t, capabilities)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.wantProtocol, params.ProtocolVersion)
			require.Equal(t, tt.wantClientName, params.ClientInfo.Name)
			require.Equal(t, tt.wantClientVer, params.ClientInfo.Version)
			require.Equal(t, tt.wantCapabilities, capabilities)
		})
	}
}

// TestParseInitializeParams_CapabilitiesAlwaysSorted guards against map
// iteration order leaking into the recorded capability list.
func TestParseInitializeParams_CapabilitiesAlwaysSorted(t *testing.T) {
	t.Parallel()

	raw := `{"capabilities":{"zeta":{},"alpha":{},"mu":{},"beta":{}}}`
	for range 20 {
		_, capabilities, err := parseInitializeParams(json.RawMessage(raw))
		require.NoError(t, err)
		require.Equal(t, []string{"alpha", "beta", "mu", "zeta"}, capabilities)
	}
}

func TestParseInitializeParams_DeepClientNameNotTruncatedAtParse(t *testing.T) {
	t.Parallel()

	// Truncation happens at capture time via conv.TruncateString; parsing
	// itself preserves the raw client identity.
	long := strings.Repeat("x", 250)
	raw := `{"clientInfo":{"name":"` + long + `","version":"1.0.0"}}`
	params, _, err := parseInitializeParams(json.RawMessage(raw))
	require.NoError(t, err)
	require.Len(t, params.ClientInfo.Name, 250)
}
