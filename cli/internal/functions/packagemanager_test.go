package functions

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectPackageManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lockfiles []string
		userAgent string
		want      string
	}{
		{name: "default", lockfiles: nil, userAgent: "", want: "npm"},
		{name: "npm user agent", lockfiles: nil, userAgent: "npm/10.8.2 node/v22.18.0 darwin arm64", want: "npm"},
		{name: "pnpm user agent", lockfiles: nil, userAgent: "pnpm/10.0.0 npm/? node/v22.18.0", want: "pnpm"},
		{name: "yarn user agent", lockfiles: nil, userAgent: "yarn/1.22.22 npm/? node/v22.18.0", want: "yarn"},
		{name: "bun user agent", lockfiles: nil, userAgent: "bun/1.2.0 npm/? node/v22.18.0", want: "bun"},
		{name: "unknown user agent", lockfiles: nil, userAgent: "deno/2.0.0", want: "npm"},
		{name: "pnpm lockfile", lockfiles: []string{"pnpm-lock.yaml"}, userAgent: "", want: "pnpm"},
		{name: "yarn lockfile", lockfiles: []string{"yarn.lock"}, userAgent: "", want: "yarn"},
		{name: "bun text lockfile", lockfiles: []string{"bun.lock"}, userAgent: "", want: "bun"},
		{name: "bun binary lockfile", lockfiles: []string{"bun.lockb"}, userAgent: "", want: "bun"},
		{name: "npm lockfile", lockfiles: []string{"package-lock.json"}, userAgent: "pnpm/10.0.0", want: "npm"},
		{name: "lockfile beats user agent", lockfiles: []string{"yarn.lock"}, userAgent: "pnpm/10.0.0", want: "yarn"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for _, lf := range tc.lockfiles {
				writeFile(t, filepath.Join(dir, lf), "")
			}

			require.Equal(t, tc.want, DetectPackageManager(dir, tc.userAgent))
		})
	}
}

func TestRunScriptArgs(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{"run", "dev"}, runScriptArgs("npm", "dev", nil))
	require.Equal(t, []string{"run", "dev", "--", "--port", "3000"}, runScriptArgs("npm", "dev", []string{"--port", "3000"}))
	require.Equal(t, []string{"run", "dev", "--port", "3000"}, runScriptArgs("pnpm", "dev", []string{"--port", "3000"}))
	require.Equal(t, []string{"run", "dev", "--port", "3000"}, runScriptArgs("yarn", "dev", []string{"--port", "3000"}))
	require.Equal(t, []string{"run", "dev", "--port", "3000"}, runScriptArgs("bun", "dev", []string{"--port", "3000"}))
}
