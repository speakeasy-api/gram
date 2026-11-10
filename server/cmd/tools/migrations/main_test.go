package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/riskfindings"
)

func TestParseFlagsGapFillDefaultsToRetentionWindow(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://local")
	t.Setenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING", `{"current":"v1","keys":{"v1":"dGVzdA=="}}`)

	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	cfg, err := parseFlags([]string{
		"-gap-fill",
		"-org", "org_test",
		"-sources", "presidio,gitleaks,presidio",
	}, now)
	require.NoError(t, err)
	require.True(t, cfg.gapFill)
	require.Equal(t, []string{"gitleaks", "presidio"}, cfg.sources)
	require.NotNil(t, cfg.from)
	require.Equal(t, now.Add(-clickhouseRetention), *cfg.from)
}

func TestParseFlagsGapFillRequiresOrganization(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://local")
	t.Setenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING", `{"current":"v1","keys":{"v1":"dGVzdA=="}}`)

	_, err := parseFlags([]string{"-gap-fill"}, time.Now().UTC())
	require.ErrorContains(t, err, "-org is required")
}

func TestParseFlagsExplicitEmptyFromDisablesDefault(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://local")
	t.Setenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING", `{"current":"v1","keys":{"v1":"dGVzdA=="}}`)

	cfg, err := parseFlags([]string{"-gap-fill", "-org", "org_test", "-from", ""}, time.Now().UTC())
	require.NoError(t, err)
	require.Nil(t, cfg.from)
}

func TestPrintReportIncludesPerSourceGapFillCounts(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	require.NoError(t, printReport(
		&out,
		config{orgID: "org_test", dryRun: true, gapFill: true},
		5,
		map[string]int64{"gitleaks": 2, "presidio": 3},
		map[string]riskfindings.SourceStats{
			"gitleaks": {PresentByID: 1, Inserted: 1},
			"presidio": {MatchedByKey: 2, StateCopies: 1, SkippedCHNewer: 1, Inserted: 1},
		},
		uuid.Nil.String(),
	))

	require.Contains(t, out.String(), "gitleaks: postgres=2 present_by_id=1 matched_by_key=0 state_copies=0 skipped_ch_newer=0 inserts=1")
	require.Contains(t, out.String(), "presidio: postgres=3 present_by_id=0 matched_by_key=2 state_copies=1 skipped_ch_newer=1 inserts=1")
	require.Contains(t, out.String(), "last cursor: (dry run, no durable checkpoint)")
}

func TestParseFlagsGapFillResumeRequiresExplicitFrom(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://local")
	t.Setenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING", `{"current":"v1","keys":{"v1":"dGVzdA=="}}`)

	cursor := "00000000-0000-0000-0000-00000000000a/2026-07-02T03:04:05.123456Z/00000000-0000-0000-0000-000000000001"
	_, err := parseFlags([]string{"-gap-fill", "-org", "org_test", "-gap-fill-cursor", cursor}, time.Now().UTC())
	require.ErrorContains(t, err, "-from is required")

	cfg, err := parseFlags([]string{"-gap-fill", "-org", "org_test", "-gap-fill-cursor", cursor, "-from", "2026-07-01T00:00:00Z"}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, cursor, cfg.gapFillCursor.String())
	require.Equal(t, cfg.gapFillCursor, cfg.criteria()[riskfindings.CriteriaGapFillCursor])
}

func TestParseFlagsGapFillRejectsIDCursor(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://local")
	t.Setenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING", `{"current":"v1","keys":{"v1":"dGVzdA=="}}`)

	_, err := parseFlags([]string{"-gap-fill", "-org", "org_test", "-cursor", "00000000-0000-0000-0000-000000000001", "-from", "2026-07-01T00:00:00Z"}, time.Now().UTC())
	require.ErrorContains(t, err, "-gap-fill-cursor")
}

func TestParseFlagsGapFillCursorMustMatchProject(t *testing.T) {
	t.Setenv("GRAM_DATABASE_URL", "postgres://local")
	t.Setenv("GRAM_RISK_FINGERPRINT_PEPPER_KEYRING", `{"current":"v1","keys":{"v1":"dGVzdA=="}}`)

	cursor := "00000000-0000-0000-0000-00000000000a/2026-07-02T03:04:05Z/00000000-0000-0000-0000-000000000001"
	_, err := parseFlags([]string{
		"-gap-fill", "-org", "org_test", "-from", "2026-07-01T00:00:00Z",
		"-project", "00000000-0000-0000-0000-00000000000b", "-gap-fill-cursor", cursor,
	}, time.Now().UTC())
	require.ErrorContains(t, err, "different project")
}
