package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallMethodForPath(t *testing.T) {
	t.Parallel()

	roots := nodeGlobalRoots{
		NPM:  "/opt/homebrew/lib/node_modules",
		PNPM: "/Users/me/Library/pnpm/global/5/node_modules",
		Yarn: "/Users/me/.config/yarn/global",
	}

	tests := []struct {
		name  string
		path  string
		roots nodeGlobalRoots
		want  installMethod
	}{
		{name: "homebrew cellar", path: "/opt/homebrew/Cellar/cli/1.0.0/bin/speakeasy", roots: roots, want: installMethodHomebrew},
		{name: "linuxbrew cellar", path: "/home/linuxbrew/.linuxbrew/Cellar/gram/1.0.0/bin/gram", roots: roots, want: installMethodHomebrew},
		{name: "npm global under homebrew node", path: "/opt/homebrew/lib/node_modules/@speakeasy-api/cli/bin/speakeasy", roots: roots, want: installMethodNPM},
		{
			name:  "npm global on windows",
			path:  `C:\Users\me\AppData\Roaming\npm\node_modules\@speakeasy-api\cli\bin\speakeasy.exe`,
			roots: nodeGlobalRoots{NPM: `C:\Users\me\AppData\Roaming\npm\node_modules`, PNPM: "", Yarn: ""},
			want:  installMethodNPM,
		},
		{name: "pnpm global", path: "/Users/me/Library/pnpm/global/5/node_modules/@speakeasy-api/cli/bin/speakeasy", roots: roots, want: installMethodPNPM},
		{name: "yarn global", path: "/Users/me/.config/yarn/global/node_modules/@speakeasy-api/cli/bin/speakeasy", roots: roots, want: installMethodYarn},
		{name: "project dependency", path: "/Users/me/work/app/node_modules/@speakeasy-api/cli/bin/speakeasy", roots: roots, want: installMethodNodeModules},
		{name: "npx cache", path: "/Users/me/.npm/_npx/abc/node_modules/@speakeasy-api/cli/bin/speakeasy", roots: roots, want: installMethodNodeModules},
		{
			name:  "node_modules without npm installed",
			path:  "/usr/local/lib/node_modules/@speakeasy-api/cli/bin/speakeasy",
			roots: nodeGlobalRoots{NPM: "", PNPM: "", Yarn: ""},
			want:  installMethodNodeModules,
		},
		{name: "sibling of npm root is not npm", path: "/opt/homebrew/lib/node_modules_old/speakeasy", roots: roots, want: installMethodHomebrew},
		{name: "aqua", path: "/home/me/.local/share/aquaproj-aqua/pkgs/github_release/gram", roots: roots, want: installMethodAqua},
		{name: "manual", path: "/usr/local/bin/speakeasy", roots: roots, want: installMethodManual},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, installMethodForPath(tc.path, tc.roots))
		})
	}
}

func TestNodePackageManagerHint(t *testing.T) {
	t.Parallel()

	require.Equal(t, "Installed with pnpm. Update with: pnpm add -g @speakeasy-api/cli@latest", nodePackageManagerHint(installMethodPNPM))
	require.Equal(t, "Installed with yarn. Update with: yarn global add @speakeasy-api/cli@latest", nodePackageManagerHint(installMethodYarn))
	require.Contains(t, nodePackageManagerHint(installMethodNodeModules), "not a global npm, pnpm or yarn root")
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
