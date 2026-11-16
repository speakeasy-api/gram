package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelsRouteClassification(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path string
		mcp  bool
	}{
		{path: "/channels/example-org/example-project/mcp", mcp: true},
		{path: "/channels/other-org/other-project/mcp", mcp: true},
		{path: "/channels/example-org/example-project/api"},
		{path: "/channels/mcp"},
		{path: "/channels/api"},
		{path: "/channels/example-org/example-project/mcp/"},
		{path: "/channels/example-org/example-project/mcp/extra"},
		{path: "/channels//example-project/mcp"},
		{path: "/channels/example-org//mcp"},
		{path: "/channels/./example-project/mcp"},
		{path: "/channels/example-org/../mcp"},
		{path: "/channels/example%2Forg/example-project/mcp"},
		{path: "/channels/example%252Forg/example-project/mcp"},
		{path: "/channels/example%5Corg/example-project/mcp"},
		{path: "/channels/example-org/example-project/%6dcp"},
		{path: "/%63hannels/example-org/example-project/mcp"},
		{path: "/.well-known/oauth-protected-resource/channels/example-org/example-project/mcp"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			require.Equal(t, tc.mcp, isMCPJSONRPCEndpoint(channelsClassificationPath(req)))
		})
	}
}
