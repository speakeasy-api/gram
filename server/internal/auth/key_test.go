package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOrgWidePluginHooksAPIKeyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "plugins-hooks-20260713-104500-abcdef", want: true},
		{name: "plugins-hooks-download-20260713-104500-0123ab", want: true},
		{name: "plugins-hooks", want: false},
		{name: "plugins-hooks-personal", want: false},
		{name: "plugins-hooks-20260713-104500-nothex", want: false},
		{name: "plugins-hooks-20260713-104500-ABCDEF", want: false},
		{name: "plugins-hooks-20260713-104500.5-abcdef", want: false},
		{name: "plugins-hooks-20261340-256199-abcdef", want: false},
		{name: "plugins-mcp-20260713-104500-abcdef", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsOrgWidePluginHooksAPIKeyName(tt.name))
		})
	}
}
