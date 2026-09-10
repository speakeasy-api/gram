// Command example compiles representative production telemetry queries.
//
// It deliberately stops before database execution. In production the telemetry
// repository would execute CompiledQuery.SQL with CompiledQuery.Args and scan a
// result type owned by the selected dataset.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/telemetry/analytics"
)

type queryExample struct {
	name    string
	planner analytics.Planner
	query   analytics.Query
	wantErr error
}

func main() {
	if err := run(os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(output io.Writer) error {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	// A service obtains these IDs from the authenticated organization and its
	// authorized project set. Request payloads must not create Scope directly.
	organizationID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	projectIDs := []uuid.UUID{
		uuid.MustParse("20000000-0000-0000-0000-000000000001"),
		uuid.MustParse("20000000-0000-0000-0000-000000000002"),
	}
	scope, err := analytics.NewScope(organizationID, projectIDs...)
	if err != nil {
		return fmt.Errorf("create authenticated telemetry scope: %w", err)
	}

	// Rollup coverage comes from rollup-control metadata. It is not supplied by
	// the API caller because callers cannot decide that incomplete data is safe.
	rollupAvailability, err := analytics.NewRollupAvailability(
		time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		return fmt.Errorf("create usage rollup availability: %w", err)
	}
	rollupPlanner := analytics.NewPlanner(now, rollupAvailability)
	factOnlyPlanner := analytics.NewPlanner(now, analytics.RollupAvailability{})

	dailyRange, err := analytics.NewTimeRange(
		time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		return fmt.Errorf("create daily example range: %w", err)
	}
	preciseRange, err := analytics.NewTimeRange(
		time.Date(2026, time.August, 23, 10, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		return fmt.Errorf("create precise example range: %w", err)
	}
	oldRange, err := analytics.NewTimeRange(
		time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		return fmt.Errorf("create out-of-retention example range: %w", err)
	}

	examples := []queryExample{
		{
			name:    "whole-window totals use exact facts",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, preciseRange).Select(
				analytics.MeasureRequests,
				analytics.MeasureInputTokens,
				analytics.MeasureOutputTokens,
				analytics.MeasureCacheReadInputTokens,
				analytics.MeasureCacheCreationInputTokens,
				analytics.MeasureTotalTokens,
				analytics.MeasureTotalCost,
			),
			wantErr: nil,
		},
		{
			name:    "daily totals use the complete rollup",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, dailyRange).
				AtGrain(analytics.GrainDay).
				Select(analytics.MeasureRequests, analytics.MeasureTotalTokens, analytics.MeasureTotalCost),
			wantErr: nil,
		},
		{
			name:    "daily cube combines every dimension and filter operator",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, dailyRange).
				AtGrain(analytics.GrainDay).
				GroupBy(analytics.DimensionProvider, analytics.DimensionModel, analytics.DimensionSource).
				Select(analytics.MeasureRequests, analytics.MeasureTotalTokens, analytics.MeasureTotalCost).
				Where(
					analytics.Equals(analytics.DimensionModel, "example-model"),
					analytics.OneOf(analytics.DimensionSource, "provider_otel", "provider_api"),
				).
				OrderBy(analytics.Descending(analytics.MeasureTotalCost)).
				WithLimit(50),
			wantErr: nil,
		},
		{
			name:    "sub-day provider leaderboard falls back to facts",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, preciseRange).
				AtGrain(analytics.GrainDay).
				GroupBy(analytics.DimensionProvider).
				Select(analytics.MeasureRequests, analytics.MeasureTotalTokens, analytics.MeasureTotalCost).
				OrderBy(analytics.Ascending(analytics.MeasureTotalTokens)).
				WithLimit(25),
			wantErr: nil,
		},
		{
			name:    "daily model series falls back when rollup state is unavailable",
			planner: factOnlyPlanner,
			query: analytics.NewUsageQuery(scope, dailyRange).
				AtGrain(analytics.GrainDay).
				GroupBy(analytics.DimensionModel).
				Select(
					analytics.MeasureInputTokens,
					analytics.MeasureOutputTokens,
					analytics.MeasureCacheReadInputTokens,
					analytics.MeasureCacheCreationInputTokens,
				),
			wantErr: nil,
		},
		{
			name:    "default ordering and row limit are compiler policy",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, dailyRange).
				AtGrain(analytics.GrainDay).
				GroupBy(analytics.DimensionSource).
				Select(analytics.MeasureRequests),
			wantErr: nil,
		},
		{
			name:    "a query without measures is rejected",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, dailyRange).
				AtGrain(analytics.GrainDay).
				GroupBy(analytics.DimensionProvider),
			wantErr: analytics.ErrInvalidQuery,
		},
		{
			name:    "an uncataloged dimension is rejected",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, preciseRange).
				GroupBy(analytics.DimensionID("arbitrary_sql")).
				Select(analytics.MeasureRequests),
			wantErr: analytics.ErrInvalidQuery,
		},
		{
			name:    "an excessive result limit is rejected",
			planner: rollupPlanner,
			query: analytics.NewUsageQuery(scope, preciseRange).
				Select(analytics.MeasureRequests).
				WithLimit(1001),
			wantErr: analytics.ErrInvalidQuery,
		},
		{
			name:    "a valid range outside every physical source is rejected",
			planner: factOnlyPlanner,
			query: analytics.NewUsageQuery(scope, oldRange).
				AtGrain(analytics.GrainDay).
				Select(analytics.MeasureRequests),
			wantErr: analytics.ErrNoCompatiblePlan,
		},
	}

	var report strings.Builder
	for _, example := range examples {
		compiled, compileErr := example.planner.Compile(example.query)
		_, _ = report.WriteString("\n## " + example.name + "\n")
		if example.wantErr != nil {
			if !errors.Is(compileErr, example.wantErr) {
				return fmt.Errorf("compile %q: expected %v, got %v", example.name, example.wantErr, compileErr)
			}
			_, _ = report.WriteString("rejected: " + compileErr.Error() + "\n")
			continue
		}
		if compileErr != nil {
			return fmt.Errorf("compile %q: %w", example.name, compileErr)
		}

		_, _ = report.WriteString("plan: " + string(compiled.PlanID) + "\n")
		_, _ = report.WriteString("sql:  " + compiled.SQL + "\n")
		_, _ = report.WriteString(fmt.Sprintf("args: %#v\n", compiled.Args))
	}

	if _, err := io.WriteString(output, report.String()); err != nil {
		return fmt.Errorf("write compiled query examples: %w", err)
	}
	return nil
}
