package functions

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDev_NoPackageJSON(t *testing.T) {
	t.Parallel()

	err := testRunner(t.TempDir(), &bytes.Buffer{}).Dev(t.Context(), t.TempDir(), nil)
	require.ErrorContains(t, err, "read package.json")
}

func TestDev_NoDevScript(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"scripts":{"build":"x"}}`)

	err := testRunner(t.TempDir(), &bytes.Buffer{}).Dev(t.Context(), dir, nil)
	require.ErrorIs(t, err, ErrNoDevScript)
}

func TestDev_RunsScriptWithArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lockfile  string
		userAgent string
		extra     []string
		want      string
	}{
		{name: "npm without args", lockfile: "", userAgent: "", extra: nil, want: "npm run dev"},
		{name: "npm forwards after separator", lockfile: "", userAgent: "", extra: []string{"--port", "3000"}, want: "npm run dev -- --port 3000"},
		{name: "pnpm lockfile", lockfile: "pnpm-lock.yaml", userAgent: "", extra: []string{"--port", "3000"}, want: "pnpm run dev --port 3000"},
		{name: "yarn user agent", lockfile: "", userAgent: "yarn/1.22.22", extra: []string{"-v"}, want: "yarn run dev -v"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bin := fakeBinDir(t)
			log := filepath.Join(t.TempDir(), "calls.log")
			for _, pm := range []string{"npm", "pnpm", "yarn", "bun"} {
				writeFakeBin(t, bin, pm, `echo "${0##*/} $*" >> "`+log+`"`+"\n")
			}

			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "package.json"), `{"scripts":{"dev":"node ./src/server.ts"}}`)
			if tc.lockfile != "" {
				writeFile(t, filepath.Join(dir, tc.lockfile), "")
			}

			r := testRunner(bin, &bytes.Buffer{}, "npm_config_user_agent="+tc.userAgent)
			require.NoError(t, r.Dev(t.Context(), dir, tc.extra))

			calls, err := os.ReadFile(filepath.Clean(log))
			require.NoError(t, err)
			require.Equal(t, tc.want, strings.TrimSpace(string(calls)))
		})
	}
}

func TestDev_ScriptFails(t *testing.T) {
	t.Parallel()

	bin := fakeBinDir(t)
	writeFakeBin(t, bin, "npm", "exit 3\n")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"scripts":{"dev":"x"}}`)

	err := testRunner(bin, &bytes.Buffer{}).Dev(t.Context(), dir, nil)
	require.ErrorContains(t, err, "npm run dev")
}

func TestDev_PackageManagerMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"scripts":{"dev":"x"}}`)

	err := testRunner(t.TempDir(), &bytes.Buffer{}).Dev(t.Context(), dir, nil)
	require.ErrorContains(t, err, "run the dev script with npm")
}
