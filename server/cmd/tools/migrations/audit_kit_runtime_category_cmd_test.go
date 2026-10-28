package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuditKitRuntimeCategoryFlags(t *testing.T) {
	t.Parallel()
	databaseURL := "postgres://user@db.internal:5432/gram?sslmode=require"
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return databaseURL
		}
		return ""
	}

	cfg, err := parseAuditKitRuntimeCategoryFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply, "a run without -apply is a dry run")
	require.EqualValues(t, auditKitRuntimeCategoryDefaultBatch, cfg.batchSize)

	_, err = parseAuditKitRuntimeCategoryFlags(nil, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseAuditKitRuntimeCategoryFlags([]string{"-environment=staging", "-batch-size=0"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseAuditKitRuntimeCategoryFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseAuditKitRuntimeCategoryFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseAuditKitRuntimeCategoryFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err = parseAuditKitRuntimeCategoryFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-category", "-batch-size=50"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	require.EqualValues(t, 50, cfg.batchSize)

	_, err = parseAuditKitRuntimeCategoryFlags([]string{"-environment=staging"}, func(string) string { return "" })
	require.ErrorContains(t, err, "GRAM_DATABASE_URL")
}

func TestParseAuditKitRuntimeCategoryFlagsRequiresTLSForWrites(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-category"}

	for _, mode := range []string{"", "?sslmode=disable", "?sslmode=allow", "?sslmode=prefer"} {
		getenv := func(string) string { return "postgres://user@db.internal:5432/gram" + mode }
		_, err := parseAuditKitRuntimeCategoryFlags(apply, getenv)
		require.ErrorContains(t, err, "TLS", "sslmode %q", mode)

		_, err = parseAuditKitRuntimeCategoryFlags([]string{"-environment=staging"}, getenv)
		require.NoError(t, err, "a dry run writes nothing, so sslmode %q is allowed", mode)
	}
}

func TestParseAuditKitRuntimeCategoryFlagsRejectsOtherFallbackHosts(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-category"}
	getenv := func(string) string {
		return "postgres://user@db.internal:5432,other.internal:5432/gram?sslmode=require"
	}

	_, err := parseAuditKitRuntimeCategoryFlags(apply, getenv)
	require.ErrorContains(t, err, "single host")
}
