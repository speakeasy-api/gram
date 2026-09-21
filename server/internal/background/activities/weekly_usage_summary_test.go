package activities_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWeeklyUsageSummary_UsesCompletedUTCDaysAndExactPaygProductCosts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "weekly_usage_summary_metered_payg")
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	orgID, orgName := createAlertOrgWithAccountType(t, ctx, db, "billing@example.test", "", billing.TierPayg)

	runTime := time.Date(2026, time.September, 21, 12, 0, 0, 0, time.UTC)
	currentStart := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	previousStart := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, chrepo.New(chConn).InsertReadings(ctx, []chrepo.ReadingRow{
		weeklyMeterReading(orgID, metering.MeterAgentSessionStorage, 1_000_000, currentStart.Add(time.Hour)),
		weeklyMeterReading(orgID, metering.MeterRiskGitleaks, 1_000_000, currentStart.Add(2*time.Hour)),
		weeklyMeterReading(orgID, metering.MeterRiskPresidio, 1_000_000, currentStart.Add(3*time.Hour)),
		weeklyMeterReading(orgID, metering.MeterMCPBandwidthEgress, 1_073_741_824, currentStart.Add(4*time.Hour)),
		weeklyMeterReading(orgID, metering.MeterAgentSessionStorage, 9_000_000, runTime.Truncate(24*time.Hour).Add(time.Hour)),
		weeklyMeterReading(orgID, metering.MeterAgentSessionStorage, 500_000, previousStart.Add(time.Hour)),
		weeklyMeterReading(orgID, metering.MeterRiskGitleaks, 1_000_000, previousStart.Add(2*time.Hour)),
		weeklyMeterReading(orgID, metering.MeterMCPBandwidthEgress, 536_870_912, previousStart.Add(3*time.Hour)),
		weeklyMeterReading(orgID, metering.MeterMCPBandwidthEgress, 536_870_912, previousStart.AddDate(0, 0, 20)),
	}))

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	activity := activities.NewWeeklyUsageSummary(testenv.NewLogger(t), db, chConn, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
		"weekly_usage_summary": "weekly-usage-summary-test-id",
	}), true), nil)
	require.NoError(t, activity.Send(ctx, activities.SendWeeklyUsageSummaryArgs{
		Target: activities.WeeklyUsageSummaryTarget{
			OrganizationID:   orgID,
			OrganizationName: orgName,
			OrganizationSlug: orgID,
			AccountType:      string(billing.TierPayg),
			AlertEmail:       "billing@example.test",
			AnchorDay:        1,
		},
		RunTime: runTime,
	}))

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, map[string]string{
		"organization_name":       orgName,
		"cycle_end_date":          "September 30, 2026",
		"days_remaining":          "10 days",
		"usage_through_date":      "September 20, 2026",
		"storage_tokens":          "1,000,000",
		"storage_change_percent":  "+100%",
		"scanning_tokens":         "2,000,000",
		"scanning_change_percent": "+100%",
		"egress_gib":              "1.00",
		"egress_change_percent":   "+100%",
		"show_estimated_spend":    "true",
		"storage_cost_usd":        "$0.35",
		"scanning_cost_usd":       "$1.98",
		"egress_cost_usd":         "$20.00",
		"total_cost_usd":          "$22.33",
		"view_usage_url":          "",
	}, sent[0].DataVariables)
}

func TestWeeklyUsageSummary_EnterpriseOmitsEveryCost(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "weekly_usage_summary_metered_enterprise")
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	orgID, orgName := createAlertOrgWithAccountType(t, ctx, db, "enterprise@example.test", "", billing.TierEnterprise)
	runTime := time.Date(2026, time.September, 21, 0, 0, 0, 0, time.UTC)
	require.NoError(t, chrepo.New(chConn).InsertReadings(ctx, []chrepo.ReadingRow{
		weeklyMeterReading(orgID, metering.MeterAgentSessionStorage, 1_000_000, runTime.AddDate(0, 0, -1)),
	}))

	captured := &captureLoopsClient{sent: nil, failNext: 0}
	activity := activities.NewWeeklyUsageSummary(testenv.NewLogger(t), db, chConn, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
		"weekly_usage_summary": "weekly-usage-summary-test-id",
	}), true), nil)
	require.NoError(t, activity.Send(ctx, activities.SendWeeklyUsageSummaryArgs{
		Target: activities.WeeklyUsageSummaryTarget{
			OrganizationID:   orgID,
			OrganizationName: orgName,
			OrganizationSlug: orgID,
			AccountType:      string(billing.TierEnterprise),
			AlertEmail:       "enterprise@example.test",
			AnchorDay:        1,
		},
		RunTime: runTime,
	}))

	sent := captured.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, "false", sent[0].DataVariables["show_estimated_spend"])
	require.Empty(t, sent[0].DataVariables["storage_cost_usd"])
	require.Empty(t, sent[0].DataVariables["scanning_cost_usd"])
	require.Empty(t, sent[0].DataVariables["egress_cost_usd"])
	require.Empty(t, sent[0].DataVariables["total_cost_usd"])
}
func TestWeeklyUsageSummary_SkipsWhenBothWindowsAreEmpty(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "weekly_usage_summary_empty_windows")
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	orgID, orgName := createAlertOrgWithAccountType(t, ctx, db, "billing@example.test", "", billing.TierPayg)
	captured := &captureLoopsClient{sent: nil, failNext: 0}
	activity := activities.NewWeeklyUsageSummary(testenv.NewLogger(t), db, chConn, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
		"weekly_usage_summary": "weekly-usage-summary-test-id",
	}), true), nil)

	require.NoError(t, activity.Send(ctx, activities.SendWeeklyUsageSummaryArgs{
		Target: activities.WeeklyUsageSummaryTarget{
			OrganizationID:   orgID,
			OrganizationName: orgName,
			OrganizationSlug: orgID,
			AccountType:      string(billing.TierPayg),
			AlertEmail:       "billing@example.test",
			AnchorDay:        1,
		},
		RunTime: time.Date(2026, time.September, 21, 0, 0, 0, 0, time.UTC),
	}))
	require.Empty(t, captured.Sent())
}

func TestWeeklyUsageSummary_SkipsFirstCycleDay(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "weekly_usage_summary_first_day")
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	captured := &captureLoopsClient{sent: nil, failNext: 0}
	activity := activities.NewWeeklyUsageSummary(testenv.NewLogger(t), db, chConn, email.NewService(testenv.NewLogger(t), captured, email.NewTemplateIDs(map[string]string{
		"weekly_usage_summary": "weekly-usage-summary-test-id",
	}), true), nil)

	require.NoError(t, activity.Send(ctx, activities.SendWeeklyUsageSummaryArgs{
		Target:  activities.WeeklyUsageSummaryTarget{OrganizationID: "org-first-day", AnchorDay: 21},
		RunTime: time.Date(2026, time.September, 21, 18, 0, 0, 0, time.UTC),
	}))
	require.Empty(t, captured.Sent())
}

func weeklyMeterReading(organizationID string, meterID metering.MeterID, value int64, occurredAt time.Time) chrepo.ReadingRow {
	unit := metering.UnitSTokens
	measurementMethod := metering.MeasurementTiktokenO200kBase
	if meterID == metering.MeterMCPBandwidthIngress || meterID == metering.MeterMCPBandwidthEgress {
		unit = metering.UnitBytes
		measurementMethod = metering.MeasurementHTTPBodyBytes
	}
	return chrepo.ReadingRow{
		ID:                uuid.New(),
		OrganizationID:    organizationID,
		ProjectID:         uuid.New(),
		MeterID:           string(meterID),
		OperationID:       "weekly-usage-summary-test:" + uuid.NewString(),
		Unit:              string(unit),
		MeasurementMethod: string(measurementMethod),
		Value:             value,
		OccurredAt:        occurredAt,
		ProducedAt:        occurredAt,
		InsertedAt:        occurredAt,
		CorrectsReadingID: nil,
		Attributes:        map[string]string{},
	}
}
