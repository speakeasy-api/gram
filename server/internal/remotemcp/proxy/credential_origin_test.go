package proxy

import (
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestCredentialOriginRejectsChangedDestinationBeforeDial(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"https://other.example.com/mcp", "http://mcp.example.com/mcp", "https://user:secret@mcp.example.com/mcp"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			p := &Proxy{Logger: testenv.NewLogger(t), RemoteURL: target, CredentialOrigin: "https://mcp.example.com", NonStreamingTimeout: time.Second, AuthorizationOverride: "TEST_SECRET"}
			request, err := http.NewRequest(http.MethodPost, "https://localhost/mcp", nil)
			require.NoError(t, err)
			_, response, err := p.forwardRequest(t.Context(), request, nil, nil)
			if response != nil {
				require.NoError(t, response.Body.Close())
			}
			require.ErrorContains(t, err, "upstream request")
			require.NotContains(t, err.Error(), "TEST_SECRET")
		})
	}
}
