package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var clientScopesNow = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)

func clientScopesEnv(key string) string {
	if key == "GRAM_DATABASE_URL" {
		return "postgres://test"
	}
	return ""
}

func TestParseClientScopesFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseClientScopesFlags([]string{"-environment=prod", "-created-before=2026-10-14T18:00:00Z"}, clientScopesEnv, clientScopesNow, io.Discard)
	require.NoError(t, err)
	require.False(t, cfg.options.Apply)
	require.False(t, cfg.restore)
	require.True(t, cfg.options.CreatedBefore.Equal(time.Date(2026, 10, 14, 18, 0, 0, 0, time.UTC)))
}

func TestParseClientScopesFlagsRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"missing environment":     {"-created-before=2026-10-14T18:00:00Z"},
		"missing cutoff":          {"-environment=prod"},
		"malformed cutoff":        {"-environment=prod", "-created-before=2026-10-14"},
		"future cutoff":           {"-environment=prod", "-created-before=2026-11-01T00:00:00Z"},
		"unconfirmed apply":       {"-apply", "-environment=prod", "-created-before=2026-10-14T18:00:00Z", "-report=r.json"},
		"mismatched confirm":      {"-apply", "-environment=prod", "-confirm-environment=dev", "-created-before=2026-10-14T18:00:00Z", "-report=r.json"},
		"apply without report":    {"-apply", "-environment=prod", "-confirm-environment=prod", "-created-before=2026-10-14T18:00:00Z"},
		"restore with cutoff":     {"-restore", "-environment=prod", "-created-before=2026-10-14T18:00:00Z"},
		"restore apply no report": {"-restore", "-apply", "-environment=prod", "-confirm-environment=prod"},
		"positional argument":     {"-environment=prod", "-created-before=2026-10-14T18:00:00Z", "extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseClientScopesFlags(args, clientScopesEnv, clientScopesNow, io.Discard)
			require.Error(t, err)
		})
	}
}

func TestParseClientScopesFlagsAcceptsConfirmedWrites(t *testing.T) {
	t.Parallel()
	cfg, err := parseClientScopesFlags([]string{"-apply", "-environment=prod", "-confirm-environment=prod", "-created-before=2026-10-14T18:00:00Z", "-report=r.json"}, clientScopesEnv, clientScopesNow, io.Discard)
	require.NoError(t, err)
	require.True(t, cfg.options.Apply)
	require.Equal(t, "r.json", cfg.reportPath)

	cfg, err = parseClientScopesFlags([]string{"-restore", "-apply", "-environment=prod", "-confirm-environment=prod", "-report=r.json"}, clientScopesEnv, clientScopesNow, io.Discard)
	require.NoError(t, err)
	require.True(t, cfg.restore)
	require.True(t, cfg.options.Apply)
}

func TestRunClientScopesHelp(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, runClientScopes([]string{"-h"}, io.Discard, clientScopesEnv))
}

func TestRunClientScopesRefusesExistingReport(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "report.json")
	require.NoError(t, os.WriteFile(path, []byte("earlier apply"), 0o600))
	code := runClientScopes([]string{"-environment=prod", "-created-before=2026-10-14T18:00:00Z", "-report=" + path}, io.Discard, clientScopesEnv)
	require.Equal(t, 2, code)
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "earlier apply", string(contents))
}
