package telemetry

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry/semantic"
)

// This file adapts the legacy telemetry.query surface onto the semantic
// layer: legacy dimension/measure keys are aliased to catalog vocabulary, the
// request is planned and compiled by the semantic package, and the results
// are converted back into the repo row types the existing buildQueryResult
// consumes. Service.Query routes every non-skill_version request through
// here; the parity tests compare it against the retained legacy repo path.

const semanticUsageModelName = "usage"

// semanticAliasMaps holds the legacy-key alias maps derived from the loaded
// definition (the legacy_key fields are the single source of the mapping).
type semanticAliasMaps struct {
	def   *semantic.Definition
	model *semantic.Model
	// dimLegacyToCatalog / dimCatalogToLegacy alias public telemetry.query
	// dimension keys to catalog dimension names.
	dimLegacyToCatalog map[string]string
	dimCatalogToLegacy map[string]string
	// measureLegacyToCatalog aliases public measure keys (sort_by) to the
	// model's catalog measure names.
	measureLegacyToCatalog map[string]string
	// allMeasures are all model measure names in declaration order.
	allMeasures []string
}

var loadSemanticAliases = sync.OnceValues(func() (*semanticAliasMaps, error) {
	def, err := semantic.Load()
	if err != nil {
		return nil, fmt.Errorf("load semantic definition: %w", err)
	}
	model, ok := def.Model(semanticUsageModelName)
	if !ok {
		return nil, fmt.Errorf("semantic definition has no %q model", semanticUsageModelName)
	}

	maps := &semanticAliasMaps{
		def:                    def,
		model:                  model,
		dimLegacyToCatalog:     make(map[string]string, len(def.Dimensions)),
		dimCatalogToLegacy:     make(map[string]string, len(def.Dimensions)),
		measureLegacyToCatalog: make(map[string]string, len(model.Measures)),
		allMeasures:            make([]string, 0, len(model.Measures)),
	}
	for _, dim := range def.Dimensions {
		if dim.LegacyKey == "" {
			continue
		}
		maps.dimLegacyToCatalog[dim.LegacyKey] = dim.Name
		maps.dimCatalogToLegacy[dim.Name] = dim.LegacyKey
	}
	for _, ms := range model.Measures {
		if ms.LegacyKey != "" {
			maps.measureLegacyToCatalog[ms.LegacyKey] = ms.Name
		}
		maps.allMeasures = append(maps.allMeasures, ms.Name)
	}
	return maps, nil
})

// queryAttributeMetricsSemantic serves the same request as the legacy
// repo.QueryAttributeMetricsTable + QueryAttributeMetricsTimeseries pair,
// routed through the semantic layer. Row shapes match the repo types exactly
// (measure fields by catalog name, dimension_values keys renamed back to
// legacy) so the results feed buildQueryResult untouched.
func (s *Service) queryAttributeMetricsSemantic(ctx context.Context, arg repo.AttributeMetricsQueryParams) ([]repo.AttributeMetricsRow, []repo.AttributeMetricsTimePoint, error) {
	if len(arg.ProjectIDs) == 0 {
		return nil, nil, nil
	}

	aliases, err := loadSemanticAliases()
	if err != nil {
		return nil, nil, err
	}

	groupBy := ""
	if arg.GroupBy != "" {
		groupBy, err = aliasLegacyDimension(aliases, arg.GroupBy)
		if err != nil {
			return nil, nil, fmt.Errorf("group_by: %w", err)
		}
	}
	sortMeasure, ok := aliases.measureLegacyToCatalog[arg.SortBy]
	if !ok {
		return nil, nil, fmt.Errorf("unknown sort_by measure %q", arg.SortBy)
	}

	filters := make([]semantic.Filter, 0, len(arg.Filters))
	for _, f := range arg.Filters {
		// The legacy repo path skips empty-value filters before validating
		// the dimension; mirror that exactly.
		if len(f.Values) == 0 {
			continue
		}
		dim, err := aliasLegacyDimension(aliases, f.Dimension)
		if err != nil {
			return nil, nil, fmt.Errorf("filter: %w", err)
		}
		filters = append(filters, semantic.Filter{Dimension: dim, Values: f.Values})
	}

	tableQuery := semantic.Query{
		Model:                  aliases.model.Name,
		Measures:               aliases.allMeasures,
		GroupBy:                groupBy,
		Filters:                filters,
		TimeStart:              arg.TimeStart,
		TimeEnd:                arg.TimeEnd,
		GranularitySeconds:     0,
		Scope:                  semantic.Scope{ProjectIDs: arg.ProjectIDs},
		Sort:                   &semantic.Sort{Measure: sortMeasure, Desc: true},
		IncludeDimensionValues: true,
	}
	timeseriesQuery := semantic.Query{
		Model:                  aliases.model.Name,
		Measures:               aliases.allMeasures,
		GroupBy:                groupBy,
		Filters:                filters,
		TimeStart:              arg.TimeStart,
		TimeEnd:                arg.TimeEnd,
		GranularitySeconds:     arg.IntervalSeconds,
		Scope:                  semantic.Scope{ProjectIDs: arg.ProjectIDs},
		Sort:                   nil,
		IncludeDimensionValues: false,
	}

	tablePlan, err := semantic.Plan(aliases.def, tableQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("plan semantic table query: %w", err)
	}
	timeseriesPlan, err := semantic.Plan(aliases.def, timeseriesQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("plan semantic timeseries query: %w", err)
	}

	// The grouped table and the per-group timeseries are independent reads —
	// run them concurrently, matching the legacy path.
	var tableRows, timeseriesRows []semantic.Row
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		var egErr error
		tableRows, egErr = semantic.Execute(egCtx, s.chConn, tablePlan)
		if egErr != nil {
			return fmt.Errorf("semantic table query: %w", egErr)
		}
		return nil
	})
	eg.Go(func() error {
		var egErr error
		timeseriesRows, egErr = semantic.Execute(egCtx, s.chConn, timeseriesPlan)
		if egErr != nil {
			return fmt.Errorf("semantic timeseries query: %w", egErr)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, nil, fmt.Errorf("semantic analytics queries: %w", err)
	}

	table := make([]repo.AttributeMetricsRow, 0, len(tableRows))
	for _, row := range tableRows {
		table = append(table, repo.AttributeMetricsRow{
			GroupValue:               row.GroupValue,
			TotalCost:                row.Measures["cost_usd"].Float64,
			TotalInputTokens:         row.Measures["input_tokens"].Int64,
			TotalOutputTokens:        row.Measures["output_tokens"].Int64,
			TotalTokens:              row.Measures["tokens_total"].Int64,
			CacheReadInputTokens:     row.Measures["cache_read_tokens"].Int64,
			CacheCreationInputTokens: row.Measures["cache_write_tokens"].Int64,
			TotalToolCalls:           row.Measures["tool_calls"].Uint64,
			TotalChats:               row.Measures["chats"].Uint64,
			TotalWorkUnits:           row.Measures["total_work_units"].Float64,
			ScoredCost:               row.Measures["scored_cost"].Float64,
			ScoredTokens:             row.Measures["scored_tokens"].Int64,
			DimensionValues:          legacyDimensionValues(aliases, row.DimensionValues),
		})
	}
	timeseries := make([]repo.AttributeMetricsTimePoint, 0, len(timeseriesRows))
	for _, row := range timeseriesRows {
		timeseries = append(timeseries, repo.AttributeMetricsTimePoint{
			GroupValue:               row.GroupValue,
			BucketTimeUnixNano:       row.BucketTimeUnixNano,
			TotalCost:                row.Measures["cost_usd"].Float64,
			TotalInputTokens:         row.Measures["input_tokens"].Int64,
			TotalOutputTokens:        row.Measures["output_tokens"].Int64,
			TotalTokens:              row.Measures["tokens_total"].Int64,
			CacheReadInputTokens:     row.Measures["cache_read_tokens"].Int64,
			CacheCreationInputTokens: row.Measures["cache_write_tokens"].Int64,
			TotalToolCalls:           row.Measures["tool_calls"].Uint64,
			TotalChats:               row.Measures["chats"].Uint64,
			TotalWorkUnits:           row.Measures["total_work_units"].Float64,
			ScoredCost:               row.Measures["scored_cost"].Float64,
			ScoredTokens:             row.Measures["scored_tokens"].Int64,
		})
	}
	return table, timeseries, nil
}

func aliasLegacyDimension(aliases *semanticAliasMaps, legacyKey string) (string, error) {
	name, ok := aliases.dimLegacyToCatalog[legacyKey]
	if !ok {
		return "", fmt.Errorf("unknown dimension %q", legacyKey)
	}
	return name, nil
}

// legacyDimensionValues renames the dimension_values keys from catalog names
// back to the public legacy keys the API response uses. Catalog dimensions
// without a legacy key (session/turn) are dropped — they have no public
// aliasing and never appear on the summaries binding this adapter routes to.
func legacyDimensionValues(aliases *semanticAliasMaps, values map[string][]string) map[string][]string {
	out := make(map[string][]string, len(values))
	for name, vals := range values {
		legacyKey, ok := aliases.dimCatalogToLegacy[name]
		if !ok {
			continue
		}
		out[legacyKey] = vals
	}
	return out
}
