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
			"insecure.example.test":  "http://insecure.example.test",
			"127.0.0.1":              "http://127.0.0.1:6161",
		},
	}
	require.Equal(t, []string{
		"http://127.0.0.1:6161/inspect/oauth/callback",
		"https://dashboard.example.test/inspect/oauth/callback",
		"https://localhost:5342/inspect/oauth/callback",
		"https://server.example.test/inspect/oauth/callback",
	}, service.dashboardInspectRedirectURIs())
}
