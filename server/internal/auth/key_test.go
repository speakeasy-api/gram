package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsOrgWidePluginHooksAPIKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		testName  string
		keyName   string
		key       string
		keyPrefix string
		want      bool
	}{
		{testName: "publish hooks key", keyName: "plugins-hooks-20260713-104500-abcdef", key: "gram_live_abcdef012345", keyPrefix: "gram_live_abcde", want: true},
		{testName: "download hooks key", keyName: "plugins-hooks-download-20260713-104500-0123ab", key: "gram_live_0123ababcdef", keyPrefix: "gram_live_0123a", want: true},
		{testName: "matching name but unrelated token", keyName: "plugins-hooks-20260713-104500-abcdef", key: "gram_live_123456abcdef", keyPrefix: "gram_live_12345", want: false},
		{testName: "legacy personal name", keyName: "plugins-hooks", want: false},
		{testName: "personal suffix", keyName: "plugins-hooks-personal", want: false},
		{testName: "non-hex suffix", keyName: "plugins-hooks-20260713-104500-nothex", want: false},
		{testName: "uppercase suffix", keyName: "plugins-hooks-20260713-104500-ABCDEF", want: false},
		{testName: "fractional seconds", keyName: "plugins-hooks-20260713-104500.5-abcdef", want: false},
		{testName: "invalid timestamp", keyName: "plugins-hooks-20261340-256199-abcdef", want: false},
		{testName: "mcp purpose", keyName: "plugins-mcp-20260713-104500-abcdef", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsOrgWidePluginHooksAPIKey(tt.keyName, tt.key, tt.keyPrefix))
		})
	}
}
