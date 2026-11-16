package k8s

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithCustomDomainBackendDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    customDomainBackendServices
		expected customDomainBackendServices
	}{
		{
			name:     "empty backends use the historical server default",
			input:    customDomainBackendServices{},
			expected: customDomainBackendServices{mcp: "gram-server", server: "gram-server"},
		},
		{
			name:     "server backend falls back to existing MCP backend",
			input:    customDomainBackendServices{mcp: "custom-domain-mcp"},
			expected: customDomainBackendServices{mcp: "custom-domain-mcp", server: "custom-domain-mcp"},
		},
		{
			name:     "server backend can roll out independently",
			input:    customDomainBackendServices{mcp: "custom-domain-mcp", server: "custom-domain-server"},
			expected: customDomainBackendServices{mcp: "custom-domain-mcp", server: "custom-domain-server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, withCustomDomainBackendDefaults(tt.input))
		})
	}
}
