package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMemoryRoleGrantsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseMemoryRoleGrantsFlags([]string{"-environment=staging", "-org=org-a"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.False(t, cfg.apply)
	require.Equal(t, "org-a", cfg.orgID)
}

func TestParseMemoryRoleGrantsFlagsRequiresWriteConfirmations(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram?sslmode=require" }

	_, err := parseMemoryRoleGrantsFlags([]string{"-apply"}, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseMemoryRoleGrantsFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseMemoryRoleGrantsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseMemoryRoleGrantsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err := parseMemoryRoleGrantsFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=memory-role-grants"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
}

func TestParseMemoryRoleGrantsFlagsRejectsExplicitlyEmptyOrg(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	_, err := parseMemoryRoleGrantsFlags([]string{"-environment=staging", "-org="}, getenv)
	require.ErrorContains(t, err, "-org must not be empty")

	cfg, err := parseMemoryRoleGrantsFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.Empty(t, cfg.orgID)
}

func TestParseMemoryRoleGrantsFlagsRejectsFallbackHosts(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432,other.internal:5432/gram" }

	_, err := parseMemoryRoleGrantsFlags([]string{"-environment=staging"}, getenv)
	require.ErrorContains(t, err, "single host")
}

func TestParseMemoryRoleGrantsFlagsRequiresTLSForWrites(t *testing.T) {
	t.Parallel()
	writeArgs := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=memory-role-grants"}

	for _, mode := range []string{"", "?sslmode=prefer", "?sslmode=allow", "?sslmode=disable"} {
		getenv := func(string) string { return "postgres://user@db.internal:5432/gram" + mode }
		_, err := parseMemoryRoleGrantsFlags(writeArgs, getenv)
		require.ErrorContains(t, err, "TLS", "sslmode %q", mode)

		cfg, err := parseMemoryRoleGrantsFlags([]string{"-environment=staging"}, getenv)
		require.NoError(t, err, "dry runs do not need TLS (sslmode %q)", mode)
		require.False(t, cfg.apply)
	}
}
