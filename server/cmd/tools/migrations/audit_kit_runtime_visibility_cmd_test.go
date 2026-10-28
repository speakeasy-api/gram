package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuditKitRuntimeVisibilityFlags(t *testing.T) {
	t.Parallel()
	getenv := func(string) string { return "postgres://user@db.internal:5432/gram" }

	cfg, err := parseAuditKitRuntimeVisibilityFlags([]string{"-environment=staging"}, getenv)
	require.NoError(t, err)
	require.False(t, cfg.apply, "a run without -apply is a dry run")
	require.EqualValues(t, auditKitRuntimeVisibilityDefaultBatch, cfg.batchSize)

	_, err = parseAuditKitRuntimeVisibilityFlags(nil, getenv)
	require.ErrorContains(t, err, "environment is required")

	_, err = parseAuditKitRuntimeVisibilityFlags([]string{"-environment=staging", "-batch-size=0"}, getenv)
	require.ErrorContains(t, err, "batch-size")

	_, err = parseAuditKitRuntimeVisibilityFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")

	_, err = parseAuditKitRuntimeVisibilityFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=other.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "-confirm-target=db.internal:5432/gram")

	_, err = parseAuditKitRuntimeVisibilityFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram"}, getenv)
	require.ErrorContains(t, err, "confirm-apply")

	cfg, err = parseAuditKitRuntimeVisibilityFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging", "-confirm-target=db.internal:5432/gram", "-confirm-apply=audit-kit-runtime-visibility", "-batch-size=50"}, getenv)
	require.NoError(t, err)
	require.True(t, cfg.apply)
	require.EqualValues(t, 50, cfg.batchSize)
}
