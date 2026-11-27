package shadowmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func routeAt(gatewayName string, urls ...string) OfficialRoute {
	route := OfficialRoute{GatewayName: gatewayName, URL: urls[0], keys: nil}
	for _, raw := range urls {
		key, ok := OfficialServerKey(raw)
		if ok {
			route.keys = append(route.keys, key)
		}
	}
	return route
}

func TestConnectedRoute(t *testing.T) {
	t.Parallel()

	own := routeAt("", "https://app.example.com/mcp/tracker", "https://mcp.acme.test")
	gateway := routeAt("Team Gateway", "https://app.example.com/mcp/team")
	server := OfficialServer{Name: "Tracker", Routes: []OfficialRoute{own, gateway}}

	cases := []struct {
		name      string
		connected []string
		want      string
		found     bool
	}{
		{name: "gateway with trailing slash", connected: []string{"https://upstream.example.com/mcp", "https://app.example.com/mcp/team/"}, want: "Team Gateway", found: true},
		{name: "own custom domain address", connected: []string{"https://mcp.acme.test/"}, want: "", found: true},
		{name: "only the direct upstream", connected: []string{"https://upstream.example.com/mcp"}, found: false},
		{name: "no list", connected: nil, found: false},
		{name: "unparseable entry", connected: []string{"not a url"}, found: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			route, ok := server.ConnectedRoute(tc.connected)
			require.Equal(t, tc.found, ok)
			if ok {
				require.Equal(t, tc.want, route.GatewayName)
			}
		})
	}
}
