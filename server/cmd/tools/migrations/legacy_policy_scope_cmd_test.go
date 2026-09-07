package main

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/legacypolicyscope"
)

func legacyScopeEnv(dsn string) func(string) string {
	return func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return dsn
		}
		return ""
	}
}

func TestParseLegacyPolicyScopeFlagsDefaultsToDryRun(t *testing.T) {
	t.Parallel()

	cfg, err := parseLegacyPolicyScopeFlags([]string{"-environment=dev"}, legacyScopeEnv("postgres://test"))
	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.ModeDryRun, cfg.mode)
	require.Equal(t, "dev", cfg.environment)
}

func TestParseLegacyPolicyScopeFlagsRejectsUnknownEnvironment(t *testing.T) {
	t.Parallel()

	_, err := parseLegacyPolicyScopeFlags([]string{"-environment=prd"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "unrecognized -environment")
}

// GRAM_ENVIRONMENT spells production "prod", so the confirmation must key off
// the canonical name rather than a literal match on "production".
func TestParseLegacyPolicyScopeFlagsProductionAliasNeedsConfirmation(t *testing.T) {
	t.Parallel()

	getenv := legacyScopeEnv("postgres://gram@db.prod.example.com/gram")
	_, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=prod", "-apply", "-confirm-environment=prod"}, getenv)
	require.ErrorContains(t, err, "-confirm-production=production")

	cfg, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=prod", "-apply", "-confirm-environment=prod", "-confirm-production=production"}, getenv)
	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.ModeApply, cfg.mode)
	require.Equal(t, "production", cfg.environment)
}

func TestParseLegacyPolicyScopeFlagsRefusesProductionDSNUnderLesserEnvironment(t *testing.T) {
	t.Parallel()

	_, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-apply", "-confirm-environment=dev"},
		legacyScopeEnv("postgres://gram@gram-prod-primary.example.com:5432/gram"))
	require.ErrorContains(t, err, "production host")

	// A dev host with a database named "prod" is not a production target.
	_, err = parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-apply", "-confirm-environment=dev"},
		legacyScopeEnv("postgres://gram@gram-dev.example.com:5432/prod"))
	require.NoError(t, err)
}

func TestParseLegacyPolicyScopeFlagsRequiresMatchingEnvironmentConfirmation(t *testing.T) {
	t.Parallel()

	_, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-apply"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "-confirm-environment")
}

// Detection scopes are enforced unconditionally, so applying no longer asks the
// operator to assert a rollout flag is on.
func TestParseLegacyPolicyScopeFlagsApplyNeedsNoScopeFlagConfirmation(t *testing.T) {
	t.Parallel()

	cfg, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-apply", "-confirm-environment=dev"}, legacyScopeEnv("postgres://test"))
	require.NoError(t, err)
	require.Equal(t, legacypolicyscope.ModeApply, cfg.mode)

	_, err = parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-apply", "-confirm-environment=dev", "-confirm-recommended-scopes-enabled"},
		legacyScopeEnv("postgres://test"))
	require.Error(t, err, "the retired flag is no longer accepted")
}

func TestParseLegacyPolicyScopeFlagsRejectsOutOfRangeBatchSize(t *testing.T) {
	t.Parallel()

	_, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-batch-size=2147483648"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "batch size must not exceed")

	cfg, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-batch-size=2147483647"}, legacyScopeEnv("postgres://test"))
	require.NoError(t, err)
	require.Equal(t, math.MaxInt32, cfg.batchSize)
}

// A sub-millisecond timeout serializes to 0ms, which PostgreSQL reads as "no
// timeout": the opposite of what the operator asked for.
func TestParseLegacyPolicyScopeFlagsRejectsSubMillisecondTimeouts(t *testing.T) {
	t.Parallel()

	_, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-lock-timeout=500us"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "at least 1ms")

	_, err = parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-statement-timeout=999ns"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "at least 1ms")

	cfg, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-lock-timeout=1ms", "-statement-timeout=1ms"},
		legacyScopeEnv("postgres://test"))
	require.NoError(t, err)
	require.Equal(t, time.Millisecond, cfg.lockTimeout)
	require.Equal(t, time.Millisecond, cfg.statementTimeout)
}

func TestParseLegacyPolicyScopeFlagsRejectsTimeoutsAbovePostgresMaximum(t *testing.T) {
	t.Parallel()

	// 2147483647ms is the largest value PostgreSQL accepts.
	_, err := parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-lock-timeout=2147483648ms"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "must not exceed")

	_, err = parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-statement-timeout=2147483648ms"}, legacyScopeEnv("postgres://test"))
	require.ErrorContains(t, err, "must not exceed")

	_, err = parseLegacyPolicyScopeFlags(
		[]string{"-environment=dev", "-lock-timeout=2147483647ms", "-statement-timeout=2147483647ms"},
		legacyScopeEnv("postgres://test"))
	require.NoError(t, err)
}
