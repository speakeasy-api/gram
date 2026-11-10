package admin

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

const (
	meterHookFleetInstalls           = "gram.hooks.fleet.installs"
	meterHookFleetOutdatedRatio      = "gram.hooks.fleet.outdated_ratio"
	meterHookFleetLegacyTrafficRatio = "gram.hooks.fleet.legacy_traffic_ratio"

	hookFleetMetricsWindow = 7 * 24 * time.Hour
)

var hookFleetStatuses = []hookversions.Status{
	hookversions.StatusCurrent,
	hookversions.StatusBehind,
	hookversions.StatusUnsupported,
	hookversions.StatusUnknown,
}

// HookVersionMixReader reads installs per organization and version across
// every tenant. Implemented by telemetry.HookFleet.
type HookVersionMixReader interface {
	VersionMix(ctx context.Context, from, to time.Time) ([]telemetry.HookVersionMixRow, error)
}

// HookFleetMetrics emits per-organization hooks install gauges for the last
// seven full UTC days, so Datadog can trend and alert on outdated fleets.
type HookFleetMetrics struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	reader        HookVersionMixReader
	policy        hookversions.Policy
	installs      metric.Int64Gauge
	outdatedRatio metric.Float64Gauge
	legacyRatio   metric.Float64Gauge
	now           func() time.Time
}

func NewHookFleetMetrics(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, reader HookVersionMixReader, policy hookversions.Policy) (*HookFleetMetrics, error) {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/admin/hookfleetmetrics")
	installs, err := meter.Int64Gauge(meterHookFleetInstalls,
		metric.WithDescription("Hooks installs seen in the last 7 days, per organization and version status."),
		metric.WithUnit("{install}"))
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterHookFleetInstalls, err)
	}
	outdated, err := meter.Float64Gauge(meterHookFleetOutdatedRatio,
		metric.WithDescription("Share of an organization's hooks installs that are behind or unsupported (0.0-1.0)."),
		metric.WithUnit("1"))
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterHookFleetOutdatedRatio, err)
	}
	legacy, err := meter.Float64Gauge(meterHookFleetLegacyTrafficRatio,
		metric.WithDescription("Share of an organization's hook events sent to legacy per-tool endpoints (0.0-1.0)."),
		metric.WithUnit("1"))
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", meterHookFleetLegacyTrafficRatio, err)
	}
	return &HookFleetMetrics{
		logger:        logger.With(attr.SlogComponent("hook_fleet_metrics")),
		db:            db,
		reader:        reader,
		policy:        policy,
		installs:      installs,
		outdatedRatio: outdated,
		legacyRatio:   legacy,
		now:           time.Now,
	}, nil
}

type hookOrgMix struct {
	Installs     map[hookversions.Status]int64
	Events       uint64
	LegacyEvents uint64
}

func (m hookOrgMix) total() int64 {
	var n int64
	for _, v := range m.Installs {
		n += v
	}
	return n
}

func (m hookOrgMix) outdatedRatio() float64 {
	total := m.total()
	if total == 0 {
		return 0
	}
	return float64(m.Installs[hookversions.StatusBehind]+m.Installs[hookversions.StatusUnsupported]) / float64(total)
}

func (m hookOrgMix) legacyRatio() float64 {
	if m.Events == 0 {
		return 0
	}
	return float64(m.LegacyEvents) / float64(m.Events)
}

func summarizeHookVersionMix(rows []telemetry.HookVersionMixRow, policy hookversions.Policy) map[string]hookOrgMix {
	out := map[string]hookOrgMix{}
	for _, row := range rows {
		mix, ok := out[row.OrganizationID]
		if !ok {
			mix = hookOrgMix{Installs: map[hookversions.Status]int64{
				hookversions.StatusCurrent:     0,
				hookversions.StatusBehind:      0,
				hookversions.StatusUnsupported: 0,
				hookversions.StatusUnknown:     0,
			}, Events: 0, LegacyEvents: 0}
		}
		mix.Installs[policy.Classify(row.BinaryVersion, row.EndpointGeneration)] += int64(row.Installs) //nolint:gosec // install counts are far below MaxInt64
		mix.Events += row.Events
		mix.LegacyEvents += row.LegacyEvents
		out[row.OrganizationID] = mix
	}
	return out
}

// Emit records the gauges for every organization with hooks traffic in the
// window. Every status is recorded, zeros included, so a status an
// organization moved off reads 0 instead of its last value.
func (m *HookFleetMetrics) Emit(ctx context.Context) error {
	today := m.now().UTC().Truncate(24 * time.Hour)
	rows, err := m.reader.VersionMix(ctx, today.Add(-hookFleetMetricsWindow), today)
	if err != nil {
		return fmt.Errorf("read hook version mix: %w", err)
	}
	mixes := summarizeHookVersionMix(rows, m.policy)

	ids := make([]string, 0, len(mixes))
	for id := range mixes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	slugs := map[string]string{}
	if len(ids) > 0 {
		names, err := repo.New(m.db).AdminListOrganizationNamesByIDs(ctx, ids)
		if err != nil {
			return fmt.Errorf("list hook fleet organizations: %w", err)
		}
		for _, name := range names {
			slugs[name.ID] = name.Slug
		}
	}

	for _, id := range ids {
		mix := mixes[id]
		org := []attribute.KeyValue{attr.OrganizationID(id), attr.OrganizationSlug(slugs[id])}
		for _, status := range hookFleetStatuses {
			m.installs.Record(ctx, mix.Installs[status], metric.WithAttributes(append(org, attr.HookVersionStatus(string(status)))...))
		}
		m.outdatedRatio.Record(ctx, mix.outdatedRatio(), metric.WithAttributes(org...))
		m.legacyRatio.Record(ctx, mix.legacyRatio(), metric.WithAttributes(org...))
	}
	m.logger.InfoContext(ctx, "emitted hook fleet metrics", attr.SlogValueInt(len(ids)))
	return nil
}
