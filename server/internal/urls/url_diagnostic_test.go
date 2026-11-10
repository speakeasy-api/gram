package urls_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urls"
)

func TestDiagnosticURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ raw, want string }{
		{"https://user:password@example.test/mcp%2Froute?arbitrary=secret#fragment", "https://example.test/mcp%2Froute"},
		{"http://[::1]:8080/mcp?", "http://[::1]:8080/mcp"},
		{"https://example.test/mcp", "https://example.test/mcp"},
		{"https://example.test/%secret", "<invalid URL>"},
		{"relative?secret", "<invalid URL>"},
		{"https:secret", "<invalid URL>"},
		{"javascript:secret", "<invalid URL>"},
		{"", ""},
	} {
		require.Equal(t, tc.want, urls.DiagnosticURL(tc.raw))
	}
}
