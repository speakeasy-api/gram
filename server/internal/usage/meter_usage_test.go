package usage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetMeterUsageBuildsDenseClippedOrganizationReport(t *testing.T) {
	t.Parallel()
	organizationID := "org-" + uuid.NewString()
	otherOrganizationID := "org-" + uuid.NewString()
	service := newTestService(t, &mockBillingRepo{}, organizationID, 0)
	service.now = func() time.Time { return time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC) }
	from := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.April, 3, 6, 0, 0, 0, time.UTC)

	rows := []chrepo.ReadingRow{
		apiMeterUsageReading(organizationID, 9_007_199_254_740_993, from.Add(time.Hour)),
		apiMeterUsageReading(organizationID, 7, from.Add(25*time.Hour)),
		apiMeterUsageReading(otherOrganizationID, 1_000_000, from.Add(time.Hour)),
	}
	require.NoError(t, chrepo.New(service.meterReadConn).InsertReadings(t.Context(), rows))

	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID), authz.NewGrant(authz.ScopeOrgRead, organizationID))
	fromText, toText := from.Format(time.RFC3339), to.Format(time.RFC3339)
	result, err := service.GetMeterUsage(ctx, &gen.GetMeterUsagePayload{
		Family:      string(metering.UsageFamilyAgentSessionStorage),
		From:        &fromText,
		To:          &toText,
		Breakdown:   nil,
		ReadingKind: chrepo.ReadingKindUsage,
	})
	require.NoError(t, err)
	require.Equal(t, "9007199254741000", result.Total)
	require.Equal(t, "total", result.Breakdown.Dimension)
	require.Len(t, result.BillingCycles, 12)
	require.Len(t, result.Buckets, 3)
	require.Equal(t, fromText, result.Buckets[0].From)
	require.Equal(t, "2026-04-02T00:00:00Z", result.Buckets[0].To)
	require.Equal(t, "0", result.Buckets[2].Total)
	require.Len(t, result.Breakdown.Series, 1)
	require.Equal(t, []string{"9007199254740993", "7", "0"}, result.Breakdown.Series[0].Values)
	require.NotNil(t, result.Breakdown.Series[0].Key)
	require.Equal(t, "total", *result.Breakdown.Series[0].Key)
}

func TestGetMeterUsageRequiresOrganizationRead(t *testing.T) {
	t.Parallel()
	organizationID := "org-" + uuid.NewString()
	service := newTestService(t, &mockBillingRepo{}, organizationID, 0)
	ctx := authztest.WithExactGrants(t, billingEmailAdminContext(t, organizationID))

	_, err := service.GetMeterUsage(ctx, &gen.GetMeterUsagePayload{
		Family:      string(metering.UsageFamilyAgentSessionStorage),
		ReadingKind: chrepo.ReadingKindUsage,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestResolveMeterUsageWindowRejectsUnpairedAndOversizedRanges(t *testing.T) {
	t.Parallel()
	active := BillingCyclePeriod{
		Start: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
	}
	from := active.Start.Format(time.RFC3339)
	_, _, err := resolveMeterUsageWindow(&from, nil, active)
	require.Error(t, err)

	to := active.Start.AddDate(0, 3, 0).Add(time.Nanosecond).Format(time.RFC3339Nano)
	_, _, err = resolveMeterUsageWindow(&from, &to, active)
	require.Error(t, err)
}

func TestResolveMeterUsageWindowClampsThreeCalendarMonths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		from string
		to   string
	}{
		{name: "month end", from: "2026-01-31T12:30:00Z", to: "2026-04-30T12:30:00Z"},
		{name: "leap year and UTC normalization", from: "2023-11-30T17:30:00+02:00", to: "2024-02-29T15:30:00Z"},
		{name: "long quarter", from: "2026-07-01T00:00:00Z", to: "2026-10-01T00:00:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, to, err := resolveMeterUsageWindow(&test.from, &test.to, BillingCyclePeriod{})
			require.NoError(t, err)
			require.Equal(t, test.to, to.Format(time.RFC3339))
			beyond := to.Add(time.Nanosecond).Format(time.RFC3339Nano)
			_, _, err = resolveMeterUsageWindow(&test.from, &beyond, BillingCyclePeriod{})
			require.Error(t, err)
		})
	}
}

func apiMeterUsageReading(organizationID string, value int64, occurredAt time.Time) chrepo.ReadingRow {
	return chrepo.ReadingRow{
		ID:                uuid.New(),
		OrganizationID:    organizationID,
		ProjectID:         uuid.New(),
		MeterID:           string(metering.MeterAgentSessionStorage),
		OperationID:       "api-usage-test:" + uuid.NewString(),
		Unit:              string(metering.UnitSTokens),
		MeasurementMethod: string(metering.MeasurementTiktokenO200kBase),
		Value:             value,
		OccurredAt:        occurredAt,
		ProducedAt:        occurredAt,
		InsertedAt:        occurredAt,
		CorrectsReadingID: nil,
		Attributes:        map[string]string{"codec": string(metering.MeasurementTiktokenO200kBase)},
	}
}
