package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMCPUnannotatedGrantsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseMCPUnannotatedGrantsFlags([]string{"-environment=staging", "-org=org-a"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.False(t, cfg.apply)
	require.Equal(t, "org-a", cfg.orgID)
}

func TestParseMCPUnannotatedGrantsFlagsRequiresWriteConfirmations(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram?sslmode=require" }

	_, err := parseMCPUnannotatedGrantsFlags([]string{"-apply"}, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseMCPUnannotatedGrantsFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseMCPUnannotatedGrantsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseMCPUnannotatedGrantsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err := parseMCPUnannotatedGrantsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=mcp-unannotated-grants"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
}

func TestParseMCPUnannotatedGrantsFlagsRejectsExplicitlyEmptyOrg(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseMCPUnannotatedGrantsFlags([]string{"-environment=staging", "-org="}, getenv)
	require.ErrorContains(t, err, "-org must not be empty")

	cfg, err := parseMCPUnannotatedGrantsFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.Empty(t, cfg.orgID)
}

func TestParseMCPUnannotatedGrantsFlagsRejectsFallbackHosts(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432,other.internal:5432/gram" }

	_, err := parseMCPUnannotatedGrantsFlags([]string{"-environment=staging"}, getenv)
	require.ErrorContains(t, err, "single host")
}

func TestParseMCPUnannotatedGrantsFlagsRequiresTLSForWrites(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=mcp-unannotated-grants"}
	for _, mode := range []string{"", "?sslmode=disable", "?sslmode=allow", "?sslmode=prefer"} {
		getenv := func(string) string { return "postgres://user@db.internal:5432/gram" + mode }
		_, err := parseMCPUnannotatedGrantsFlags(apply, getenv)
		require.ErrorContains(t, err, "TLS", "sslmode %q", mode)

		_, err = parseMCPUnannotatedGrantsFlags([]string{"-environment=staging"}, getenv)
		require.NoError(t, err, "a dry run writes nothing, so sslmode %q is allowed", mode)
	}
}
