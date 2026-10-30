package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuditKitRuntimeSecurityFlags(t *testing.T) {
	t.Parallel()
	databaseURL := "postgres://user@db.internal:5432/gram?sslmode=require"
	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return databaseURL
		}
		return ""
	}

	cfg, err := parseAuditKitRuntimeSecurityFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply, "a run without -apply is a dry run")
	require.EqualValues(t, auditKitRuntimeSecurityDefaultBatch, cfg.batchSize)

	_, err = parseAuditKitRuntimeSecurityFlags(nil, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseAuditKitRuntimeSecurityFlags([]string{"-environment=staging", "-batch-size=0"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseAuditKitRuntimeSecurityFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseAuditKitRuntimeSecurityFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseAuditKitRuntimeSecurityFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err = parseAuditKitRuntimeSecurityFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-security", "-batch-size=50"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	require.EqualValues(t, 50, cfg.batchSize)

	_, err = parseAuditKitRuntimeSecurityFlags([]string{"-environment=staging"}, func(string) string { return "" })
	require.ErrorContains(t, err, "GRAM_DATABASE_URL")
}

func TestParseAuditKitRuntimeSecurityFlagsRequiresTLSForWrites(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-security"}

	for _, mode := range []string{"", "?sslmode=disable", "?sslmode=allow", "?sslmode=prefer"} {
		getenv := func(string) string { return "postgres://user@db.internal:5432/gram" + mode }
		_, err := parseAuditKitRuntimeSecurityFlags(apply, getenv)
		require.ErrorContains(t, err, "TLS", "sslmode %q", mode)

		_, err = parseAuditKitRuntimeSecurityFlags([]string{"-environment=staging"}, getenv)
		require.NoError(t, err, "a dry run writes nothing, so sslmode %q is allowed", mode)
	}
}

func TestParseAuditKitRuntimeSecurityFlagsRejectsOtherFallbackHosts(t *testing.T) {
	t.Parallel()
	apply := []string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-security"}
	getenv := func(string) string {
		return "postgres://user@db.internal:5432,other.internal:5432/gram?sslmode=require"
	}

	_, err := parseAuditKitRuntimeSecurityFlags(apply, getenv)
	require.ErrorContains(t, err, "single host")
}
