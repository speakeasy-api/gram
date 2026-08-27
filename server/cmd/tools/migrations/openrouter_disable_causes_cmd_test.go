package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/openrouterdisablecauses"
)

func TestParseOpenRouterDisableCausesFlagsDefaultsToDryRun(t *testing.T) {
	t.Parallel()

	cfg, err := parseOpenRouterDisableCausesFlags([]string{"-environment=staging"}, func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	})
	require.NoError(t, err)
	require.Equal(t, openrouterdisablecauses.ModeDryRun, cfg.mode)
}

func TestParseOpenRouterDisableCausesFlagsRequiresExplicitSafeModes(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "GRAM_DATABASE_URL" {
			return "postgres://test"
		}
		return ""
	}

	_, err := parseOpenRouterDisableCausesFlags([]string{"-apply", "-validate", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "exactly one mode")
	_, err = parseOpenRouterDisableCausesFlags([]string{"-apply"}, getenv)
	require.ErrorContains(t, err, "environment")
	_, err = parseOpenRouterDisableCausesFlags([]string{"-apply", "-environment=staging"}, getenv)
	require.ErrorContains(t, err, "confirm-environment")
	_, err = parseOpenRouterDisableCausesFlags([]string{"-apply", "-environment=production", "-confirm-environment=production"}, getenv)
	require.ErrorContains(t, err, "confirm-production")
	cfg, err := parseOpenRouterDisableCausesFlags([]string{"-apply", "-environment=staging", "-confirm-environment=staging"}, getenv)
	require.NoError(t, err)
	require.Equal(t, openrouterdisablecauses.ModeApply, cfg.mode)
	_, err = parseOpenRouterDisableCausesFlags([]string{"-manual-override", "-environment=production", "-confirm-environment=production", "-confirm-production=production"}, getenv)
	require.ErrorContains(t, err, "confirm-manual-override")

	manualGetenv := func(key string) string {
		switch key {
		case "GRAM_DATABASE_URL":
			return "postgres://test"
		case "GRAM_OPENROUTER_DISABLE_CAUSES_OVERRIDE_TOKEN":
			return "protected"
		default:
			return ""
		}
	}
	cfg, err = parseOpenRouterDisableCausesFlags([]string{"-manual-override", "-confirm-manual-override", "-environment=staging"}, manualGetenv)
	require.ErrorContains(t, err, "confirm-environment")
	cfg, err = parseOpenRouterDisableCausesFlags([]string{"-manual-override", "-confirm-manual-override", "-environment=staging", "-confirm-environment=staging"}, manualGetenv)
	require.NoError(t, err)
	require.Equal(t, openrouterdisablecauses.ModeManualOverride, cfg.mode)
}

func TestOpenRouterDisableCausesBlockedLogIsCategorizedAndPrivacySafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		category string
	}{
		{name: "ambiguous rows", err: fmt.Errorf("organization <ORG_ID>: %w", openrouterdisablecauses.ErrAmbiguousRows), category: "ambiguous_rows"},
		{name: "validation", err: openrouterdisablecauses.ErrValidationFailed, category: "validation_failed"},
		{name: "override conflict", err: openrouterdisablecauses.ErrManualOverrideConflict, category: "override_conflict"},
		{name: "database timeout", err: fmt.Errorf("postgres://user:secret@host/db: %w", context.DeadlineExceeded), category: "database_or_timeout"},
		{name: "database connection", err: &pgconn.ConnectError{}, category: "database_or_timeout"},
		{name: "unexpected", err: errors.New("override contents <ORG_ID>"), category: "unexpected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line := blockedOpenRouterDisableCausesLogLine(tt.err)
			require.Contains(t, line, "error_category="+tt.category)
			require.NotContains(t, line, "<ORG_ID>")
			require.NotContains(t, line, "secret")
			require.NotContains(t, line, "postgres://")
		})
	}
}

func TestDocumentedOpenRouterDisableCausesModesParse(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		switch key {
		case "GRAM_DATABASE_URL":
			return "postgres://test"
		case "GRAM_OPENROUTER_DISABLE_CAUSES_OVERRIDE_TOKEN":
			return "protected"
		default:
			return ""
		}
	}

	for _, args := range [][]string{
		{"-environment=staging"},
		{"-validate", "-environment=staging"},
		{"-apply", "-environment=staging", "-confirm-environment=staging"},
		{"-apply", "-environment=production", "-confirm-environment=production", "-confirm-production=production"},
		{"-manual-override", "-environment=staging", "-confirm-environment=staging", "-confirm-manual-override"},
	} {
		_, err := parseOpenRouterDisableCausesFlags(args, getenv)
		require.NoError(t, err)
	}

	_, err := parseOpenRouterDisableCausesFlags([]string{"-environment=staging", "-dry-run=false"}, getenv)
	require.Error(t, err)
}

func TestDecodeManualOverrideRequiresProtectedAuthorization(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"authorization_token":"provided","organization_id":"<ORG_ID>","key_type":"chat","causes":["admin_lock"]}`)
	_, err := decodeManualOverride(bytes.NewReader(raw), "expected")
	require.ErrorIs(t, err, openrouterdisablecauses.ErrManualOverrideUnauthorized)

	raw = []byte(`{"authorization_token":"expected","organization_id":"<ORG_ID>","key_type":"chat","causes":["admin_lock"]}`)
	override, err := decodeManualOverride(bytes.NewReader(raw), "expected")
	require.NoError(t, err)
	require.Equal(t, "<ORG_ID>", override.OrganizationID)
}

func TestDecodeValidationOverridesRequiresProtectedAuthorization(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"authorization_token":"wrong","overrides":[{"organization_id":"<ORG_ID>","key_type":"chat","causes":["admin_lock"]}]}`)
	_, err := decodeValidationOverrides(bytes.NewReader(raw), "expected")
	require.ErrorIs(t, err, openrouterdisablecauses.ErrManualOverrideUnauthorized)

	raw = []byte(`{"authorization_token":"expected","overrides":[{"organization_id":"<ORG_ID>","key_type":"chat","causes":["admin_lock"]}]}`)
	overrides, err := decodeValidationOverrides(bytes.NewReader(raw), "expected")
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	require.Equal(t, "<ORG_ID>", overrides[0].OrganizationID)
}

func TestOpenRouterDisableCausesSummaryIsAggregateOnly(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	err := writeOpenRouterDisableCausesSummary(&out, commandSummary{
		RunID: "run-test", Environment: "staging", CodeSHA: "sha-test", ClassifierVersion: classifierVersion, Result: "blocked",
		Summary: openrouterdisablecauses.Summary{Mode: openrouterdisablecauses.ModeApply, Scanned: 2, Classified: 1, Ambiguous: map[string]int64{openrouterdisablecauses.AmbiguousNoProvenance: 1}},
	})
	require.NoError(t, err)
	require.NotContains(t, out.String(), "<ORG_ID>")
	require.NotContains(t, out.String(), "key_hash")
	require.NotContains(t, out.String(), "money")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	require.Equal(t, "blocked", decoded["result"])
}
