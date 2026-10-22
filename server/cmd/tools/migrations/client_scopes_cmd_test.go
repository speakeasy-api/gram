package main

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func clientScopesEnv(key string) string {
	if key == "GRAM_DATABASE_URL" {
		return "postgres://test"
	}
	return ""
}

func TestParseClientScopesFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseClientScopesFlags([]string{"-environment=prod", "-created-before=2026-10-14T18:00:00Z"}, clientScopesEnv, io.Discard)
	require.NoError(t, err)
	require.False(t, cfg.options.Apply)
	require.Equal(t, "2026-10-14T18:00:00Z", cfg.options.CreatedBefore.UTC().Format("2006-01-02T15:04:05Z"))
}

func TestParseClientScopesFlagsRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"missing environment":  {"-created-before=2026-10-14T18:00:00Z"},
		"missing cutoff":       {"-environment=prod"},
		"malformed cutoff":     {"-environment=prod", "-created-before=2026-10-14"},
		"unconfirmed apply":    {"-apply", "-environment=prod", "-created-before=2026-10-14T18:00:00Z", "-report=r.json"},
		"mismatched confirm":   {"-apply", "-environment=prod", "-confirm-environment=dev", "-created-before=2026-10-14T18:00:00Z", "-report=r.json"},
		"apply without report": {"-apply", "-environment=prod", "-confirm-environment=prod", "-created-before=2026-10-14T18:00:00Z"},
		"positional argument":  {"-environment=prod", "-created-before=2026-10-14T18:00:00Z", "extra"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseClientScopesFlags(args, clientScopesEnv, io.Discard)
			require.Error(t, err)
		})
	}
}

func TestParseClientScopesFlagsAcceptsConfirmedApply(t *testing.T) {
	t.Parallel()
	cfg, err := parseClientScopesFlags([]string{"-apply", "-environment=prod", "-confirm-environment=prod", "-created-before=2026-10-14T18:00:00Z", "-report=r.json"}, clientScopesEnv, io.Discard)
	require.NoError(t, err)
	require.True(t, cfg.options.Apply)
	require.Equal(t, "r.json", cfg.reportPath)
}
