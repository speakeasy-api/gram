package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrantRoutesToUpstream(t *testing.T) {
	t.Parallel()

	const upstream = "https://a.example.com/mcp"
	cases := []struct {
		name, resource, upstream string
		tunneled, want           bool
	}{
		{name: "remote exact match", resource: upstream, upstream: upstream, tunneled: false, want: true},
		{name: "remote trailing slash normalized", resource: upstream + "/", upstream: upstream, tunneled: false, want: true},
		{name: "remote legacy null resource", resource: "", upstream: upstream, tunneled: false, want: false},
		{name: "remote other upstream", resource: "https://b.example.com/mcp", upstream: upstream, tunneled: false, want: false},
		{name: "remote bare slash is not unqualified", resource: "/", upstream: upstream, tunneled: false, want: false},
		{name: "tunneled unqualified grant", resource: "", upstream: "urn:tunnel:a", tunneled: true, want: true},
		{name: "tunneled identifier match", resource: "urn:tunnel:a", upstream: "urn:tunnel:a", tunneled: true, want: true},
		{name: "tunneled qualified elsewhere", resource: "urn:tunnel:b", upstream: "urn:tunnel:a", tunneled: true, want: false},
		{name: "tunneled qualified without identifier", resource: "urn:tunnel:a", upstream: "", tunneled: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, grantRoutesToUpstream(tc.resource, tc.upstream, tc.tunneled))
		})
	}
}
