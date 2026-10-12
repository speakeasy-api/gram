package chrepo_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

func TestGetSpendUsesEgressAndScannerExecutionsAndScopesTenant(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	organizationID := "org-" + uuid.NewString()
	otherOrganizationID := "org-" + uuid.NewString()
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	dayTwo := from.AddDate(0, 0, 1)

	firstBlock := []chrepo.ReadingRow{
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, math.MaxInt64, from.Add(time.Hour), nil, nil),
		meterUsageReading(organizationID, metering.MeterMCPBandwidthIngress, 900, from.Add(2*time.Hour), nil, nil),
		meterUsageReading(organizationID, metering.MeterMCPBandwidthEgress, 100, from.Add(3*time.Hour), nil, nil),
		meterUsageReading(organizationID, metering.MeterRiskGitleaks, 40, from.Add(4*time.Hour), nil, nil),
		meterUsageReading(organizationID, metering.MeterRiskPresidio, 40, from.Add(5*time.Hour), nil, nil),
		meterUsageReading(otherOrganizationID, metering.MeterAgentSessionStorage, 1_000_000, from.Add(6*time.Hour), nil, nil),
		meterUsageReading(otherOrganizationID, metering.MeterMCPBandwidthEgress, 1_000_000, from.Add(7*time.Hour), nil, nil),
	}
	require.NoError(t, queries.InsertReadings(t.Context(), firstBlock))
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{
		meterUsageReading(organizationID, metering.MeterAgentSessionStorage, math.MaxInt64, dayTwo.Add(time.Hour), nil, nil),
		meterUsageReading(organizationID, metering.MeterMCPBandwidthEgress, 200, dayTwo.Add(2*time.Hour), nil, nil),
		meterUsageReading(organizationID, metering.MeterRiskPromptInjection, 10, dayTwo.Add(3*time.Hour), nil, nil),
	}))

	rows, err := queries.GetSpend(t.Context(), chrepo.SpendParams{
		OrganizationID: organizationID,
		From:           from,
		To:             from.AddDate(0, 0, 2),
	})
	require.NoError(t, err)

	actual := make(map[string]string, len(rows))
	for _, row := range rows {
		actual[row.Day.Format(time.DateOnly)+":"+row.ProductID] = row.Quantity
	}
	require.Equal(t, map[string]string{
		"2026-09-01:agent_session_storage": "9223372036854775807",
		"2026-09-01:mcp_egress":            "100",
		"2026-09-01:risk_content_scans":    "80",
		"2026-09-02:agent_session_storage": "9223372036854775807",
		"2026-09-02:mcp_egress":            "200",
		"2026-09-02:risk_content_scans":    "10",
	}, actual)
}

func TestGetSpendForOrganizationsGroupsByOrganizationAndIsolatesMixedMeasurement(t *testing.T) {
	t.Parallel()
	conn := newTestClickhouse(t)
	queries := chrepo.New(conn)
	first := "org-" + uuid.NewString()
	second := "org-" + uuid.NewString()
	mixed := "org-" + uuid.NewString()
	excluded := "org-" + uuid.NewString()
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	dayTwo := from.AddDate(0, 0, 1)

	mismatched := meterUsageReading(mixed, metering.MeterAgentSessionStorage, 5, from.Add(time.Hour), nil, nil)
	mismatched.MeasurementMethod = "characters"
	require.NoError(t, queries.InsertReadings(t.Context(), []chrepo.ReadingRow{
		meterUsageReading(first, metering.MeterAgentSessionStorage, 10, from.Add(time.Hour), nil, nil),
		meterUsageReading(first, metering.MeterAgentSessionStorage, 15, from.Add(2*time.Hour), nil, nil),
		meterUsageReading(first, metering.MeterMCPBandwidthIngress, 900, from.Add(3*time.Hour), nil, nil),
		meterUsageReading(first, metering.MeterMCPBandwidthEgress, 100, dayTwo.Add(time.Hour), nil, nil),
		meterUsageReading(second, metering.MeterRiskGitleaks, 40, dayTwo.Add(2*time.Hour), nil, nil),
		meterUsageReading(mixed, metering.MeterAgentSessionStorage, 7, from.Add(2*time.Hour), nil, nil),
		mismatched,
		meterUsageReading(excluded, metering.MeterAgentSessionStorage, 1_000, from.Add(time.Hour), nil, nil),
		// Outside the window.
		meterUsageReading(first, metering.MeterAgentSessionStorage, 1_000, from.AddDate(0, 0, 2).Add(time.Hour), nil, nil),
	}))

	spend, err := queries.GetSpendForOrganizations(t.Context(), chrepo.OrganizationsSpendParams{
		OrganizationIDs: []string{first, second, mixed},
		From:            from,
		To:              from.AddDate(0, 0, 2),
	})
	require.NoError(t, err)

	actual := make(map[string]string, len(spend.Rows))
	for _, row := range spend.Rows {
		actual[row.OrganizationID+":"+row.Day.Format(time.DateOnly)+":"+row.ProductID] = row.Quantity
	}
	require.Equal(t, map[string]string{
		first + ":2026-09-01:agent_session_storage": "25",
		first + ":2026-09-02:mcp_egress":            "100",
		second + ":2026-09-02:risk_content_scans":   "40",
	}, actual)
	require.Equal(t, map[string]struct{}{mixed: {}}, spend.MixedMeasurement)
}

func TestGetSpendForOrganizationsRejectsEmptySelection(t *testing.T) {
	t.Parallel()
	queries := chrepo.New(nil)
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	_, err := queries.GetSpendForOrganizations(t.Context(), chrepo.OrganizationsSpendParams{OrganizationIDs: nil, From: from, To: from.AddDate(0, 0, 1)})
	require.ErrorIs(t, err, chrepo.ErrInvalidUsageSelection)
	_, err = queries.GetSpendForOrganizations(t.Context(), chrepo.OrganizationsSpendParams{OrganizationIDs: []string{""}, From: from, To: from.AddDate(0, 0, 1)})
	require.ErrorIs(t, err, chrepo.ErrInvalidUsageSelection)
	_, err = queries.GetSpendForOrganizations(t.Context(), chrepo.OrganizationsSpendParams{OrganizationIDs: []string{"org"}, From: from, To: from})
	require.ErrorIs(t, err, chrepo.ErrInvalidUsageRange)
}
