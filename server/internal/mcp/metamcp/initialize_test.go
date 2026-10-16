package metamcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveDiscoveryInstructions(t *testing.T) {
	t.Parallel()
	empty, blank, custom := "", " \t\n", "  Operator instructions.  "
	for _, value := range []*string{nil, &empty, &blank} {
		require.Equal(t, Instructions, ResolveDiscoveryInstructions(value, DiscoveryModeProgressive))
		require.Contains(t, ResolveDiscoveryInstructions(value, DiscoveryModeDirect), "qualified server--tool name")
		require.NotContains(t, ResolveDiscoveryInstructions(value, DiscoveryModeDirect), "execute_tool")
	}
	for _, mode := range []DiscoveryMode{DiscoveryModeDirect, DiscoveryModeProgressive} {
		require.Equal(t, custom, ResolveDiscoveryInstructions(&custom, mode))
	}
}

func TestResolveInstructions(t *testing.T) {
	t.Parallel()

	empty := ""
	blank := " \t\n\u2003"
	custom := "  Follow these instructions.\nThen list_servers.  "
	for _, tc := range []struct {
		name   string
		custom *string
		want   string
	}{
		{name: "unset", want: Instructions},
		{name: "empty", custom: &empty, want: Instructions},
		{name: "whitespace only", custom: &blank, want: Instructions},
		{name: "custom replaces default verbatim", custom: &custom, want: custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveInstructions(tc.custom); got != tc.want {
				t.Errorf("ResolveInstructions() = %q, want %q", got, tc.want)
			}
		})
	}
}
