package mcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDashboardInspectRedirectURIs(t *testing.T) {
	t.Parallel()
	service := &Service{
		// A dev worktree uses a distinct, explicitly configured dashboard
		// port. Register that exact port, not all loopback callbacks.
		siteURL:   &url.URL{Scheme: "https", Host: "localhost:5342"},
		serverURL: &url.URL{Scheme: "https", Host: "server.example.test"},
		platformHosts: map[string]string{
			"dashboard.example.test": "https://dashboard.example.test",
		},
	}
	require.Equal(t, []string{
		"https://dashboard.example.test/mcp-inspect/oauth/callback",
		"https://localhost:5342/mcp-inspect/oauth/callback",
		"https://server.example.test/mcp-inspect/oauth/callback",
	}, service.dashboardInspectRedirectURIs())
}
