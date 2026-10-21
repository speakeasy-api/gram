package mcp

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestURLNamesLocation(t *testing.T) {
	t.Parallel()

	const location = "https://mcp.example.com/mcp/slug"

	tests := []struct {
		name      string
		presented string
		subpaths  bool
		want      bool
	}{
		{name: "exact", presented: "https://mcp.example.com/mcp/slug", subpaths: false, want: true},
		{name: "trailing slash", presented: "https://mcp.example.com/mcp/slug/", subpaths: false, want: true},
		{name: "host case", presented: "https://MCP.Example.COM/mcp/slug", subpaths: false, want: true},
		{name: "explicit default port", presented: "https://mcp.example.com:443/mcp/slug", subpaths: false, want: true},
		{name: "trailing dot host", presented: "https://mcp.example.com./mcp/slug", subpaths: false, want: true},
		{name: "query and fragment ignored", presented: "https://mcp.example.com/mcp/slug?x=1#frag", subpaths: false, want: true},
		{name: "userinfo ignored", presented: "https://user@mcp.example.com/mcp/slug", subpaths: false, want: true},
		{name: "percent-encoded path", presented: "https://mcp.example.com/mcp/%73lug", subpaths: false, want: true},
		{name: "dot segments resolved", presented: "https://mcp.example.com/mcp/other/../slug", subpaths: false, want: true},
		{name: "sub-path with subpaths", presented: "https://mcp.example.com/mcp/slug/install", subpaths: true, want: true},
		{name: "sub-path without subpaths", presented: "https://mcp.example.com/mcp/slug/install", subpaths: false, want: false},
		{name: "sibling slug sharing a prefix", presented: "https://mcp.example.com/mcp/slug-two", subpaths: true, want: false},
		{name: "dot segments escaping the root", presented: "https://mcp.example.com/mcp/slug/../other", subpaths: true, want: false},
		{name: "parent path", presented: "https://mcp.example.com/mcp", subpaths: true, want: false},
		{name: "different host", presented: "https://other.example.com/mcp/slug", subpaths: true, want: false},
		{name: "subdomain", presented: "https://a.mcp.example.com/mcp/slug", subpaths: true, want: false},
		{name: "different port", presented: "https://mcp.example.com:8443/mcp/slug", subpaths: true, want: false},
		{name: "different scheme", presented: "http://mcp.example.com/mcp/slug", subpaths: true, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			presented, err := url.Parse(tc.presented)
			require.NoError(t, err)
			require.Equal(t, tc.want, urlNamesLocation(presented, location, tc.subpaths))
		})
	}
}
