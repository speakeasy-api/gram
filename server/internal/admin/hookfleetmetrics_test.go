package admin

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

type stubVersionMix struct {
	rows       []telemetry.HookVersionMixRow
	from, to   time.Time
	callsCount int
}

func (s *stubVersionMix) VersionMix(_ context.Context, from, to time.Time) ([]telemetry.HookVersionMixRow, error) {
	s.from, s.to = from, to
	s.callsCount++
	return s.rows, nil
}

func mixRow(org, version, generation string, installs, events, legacy uint64) telemetry.HookVersionMixRow {
	return telemetry.HookVersionMixRow{OrganizationID: org, BinaryVersion: version, EndpointGeneration: generation, Installs: installs, Events: events, LegacyEvents: legacy}
}

func TestHookFleetMetricsEmit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "adminhookfleetmetrics")
	require.NoError(t, err)
	seedOrg(t, ctx, conn, orgFixture{id: "org_hooks_a", name: "Hooks A", slug: "hooks-a"})

	reader := &stubVersionMix{rows: []telemetry.HookVersionMixRow{
		mixRow("org_hooks_a", "0.7.0", "ingest_binary", 2, 60, 0),
		mixRow("org_hooks_a", "0.5.0", "ingest_binary", 1, 20, 0),
		mixRow("org_hooks_a", "", "legacy_keyed", 1, 20, 20),
		mixRow("org_unknown", "", "ingest_script", 2, 10, 0),
	}}
	metricReader := sdkmetric.NewManualReader()
	policy := hookversions.Policy{Latest: semver.MustParse("0.7.0"), Minimum: semver.MustParse("0.3.0")}
	m, err := NewHookFleetMetrics(slog.New(slog.DiscardHandler), sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader)), conn, reader, policy)
	require.NoError(t, err)
	m.now = func() time.Time { return time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC) }

	require.NoError(t, m.Emit(ctx))
	today := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	require.Equal(t, today.Add(-7*24*time.Hour), reader.from)
	require.Equal(t, today, reader.to)

	var rm metricdata.ResourceMetrics
	require.NoError(t, metricReader.Collect(ctx, &rm))
	installs := map[string]int64{}
	ratios := map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, met := range scope.Metrics {
			switch data := met.Data.(type) {
			case metricdata.Gauge[int64]:
				for _, p := range data.DataPoints {
					installs[pointKey(t, p.Attributes)] = p.Value
				}
			case metricdata.Gauge[float64]:
				for _, p := range data.DataPoints {
					ratios[met.Name+"/"+pointKey(t, p.Attributes)] = p.Value
				}
			}
		}
	}
	require.Equal(t, map[string]int64{
		"org_hooks_a/hooks-a/current":     2,
		"org_hooks_a/hooks-a/behind":      1,
		"org_hooks_a/hooks-a/unsupported": 1,
		"org_hooks_a/hooks-a/unknown":     0,
		"org_unknown//current":            0,
		"org_unknown//behind":             0,
		"org_unknown//unsupported":        0,
		"org_unknown//unknown":            2,
	}, installs)
	require.Equal(t, map[string]float64{
		meterHookFleetOutdatedRatio + "/org_hooks_a/hooks-a/":      0.5,
		meterHookFleetLegacyTrafficRatio + "/org_hooks_a/hooks-a/": 0.2,
		meterHookFleetOutdatedRatio + "/org_unknown//":             0,
		meterHookFleetLegacyTrafficRatio + "/org_unknown//":        0,
	}, ratios)
}

func pointKey(t *testing.T, set attribute.Set) string {
	t.Helper()
	id, _ := set.Value(attr.OrganizationIDKey)
	slug, _ := set.Value(attr.OrganizationSlugKey)
	status, _ := set.Value(attr.HookVersionStatusKey)
	return id.AsString() + "/" + slug.AsString() + "/" + status.AsString()
}
