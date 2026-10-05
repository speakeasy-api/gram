package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallMethodForPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want installMethod
	}{
		{name: "homebrew cellar", path: "/opt/homebrew/Cellar/cli/1.0.0/bin/speakeasy", want: installMethodHomebrew},
		{name: "linuxbrew cellar", path: "/home/linuxbrew/.linuxbrew/Cellar/gram/1.0.0/bin/gram", want: installMethodHomebrew},
		{name: "npm global", path: "/usr/local/lib/node_modules/@speakeasy-api/cli/bin/speakeasy", want: installMethodNPM},
		{name: "npm under homebrew node", path: "/opt/homebrew/lib/node_modules/@speakeasy-api/cli/bin/speakeasy", want: installMethodNPM},
		{name: "npm on windows", path: `C:\Users\me\AppData\Roaming\npm\node_modules\@speakeasy-api\cli\bin\speakeasy.exe`, want: installMethodNPM},
		{name: "aqua", path: "/home/me/.local/share/aquaproj-aqua/pkgs/github_release/gram", want: installMethodAqua},
		{name: "manual", path: "/usr/local/bin/speakeasy", want: installMethodManual},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, installMethodForPath(tc.path))
		})
	}
}

func TestHomebrewFormulaFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg0 string
		want string
	}{
		{name: "speakeasy", arg0: "/opt/homebrew/bin/speakeasy", want: "speakeasy-api/tap/cli"},
		{name: "legacy gram", arg0: "/opt/homebrew/bin/gram", want: "speakeasy-api/tap/gram"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, homebrewFormulaFor(tc.arg0))
		})
	}
}
