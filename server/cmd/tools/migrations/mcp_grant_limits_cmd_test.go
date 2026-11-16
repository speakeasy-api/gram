package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMCPGrantLimitsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseMCPGrantLimitsFlags([]string{"-environment=staging", "-org=org-a"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.False(t, cfg.apply)
	require.Equal(t, "org-a", cfg.orgID)
}

func TestParseMCPGrantLimitsFlagsRequiresWriteConfirmations(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseMCPGrantLimitsFlags([]string{"-apply"}, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseMCPGrantLimitsFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseMCPGrantLimitsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseMCPGrantLimitsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err := parseMCPGrantLimitsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=mcp-grant-limits"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
}

func TestParseMCPGrantLimitsFlagsRejectsExplicitlyEmptyOrg(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseMCPGrantLimitsFlags([]string{"-environment=staging", "-org="}, getenv)
	require.ErrorContains(t, err, "-org must not be empty")

	cfg, err := parseMCPGrantLimitsFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.Empty(t, cfg.orgID)
}

func TestParseMCPGrantLimitsFlagsRejectsFallbackHosts(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432,other.internal:5432/gram" }

	_, err := parseMCPGrantLimitsFlags([]string{"-environment=staging"}, getenv)
	require.ErrorContains(t, err, "single host")
}
