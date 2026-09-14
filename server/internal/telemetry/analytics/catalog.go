package analytics

import (
	"fmt"
	"slices"
	"time"
)

// PlanID identifies a compiler-owned physical plan without exposing it in the
// semantic Query type.
type PlanID string

const (
	// PlanUsageDaily reads the refreshable daily usage rollup.
	PlanUsageDaily PlanID = "usage_daily"

	// PlanUsageFacts reads deduplicated canonical usage facts.
	PlanUsageFacts PlanID = "usage_facts"
)

// DimensionMetadata describes one public grouping and filtering axis.
type DimensionMetadata struct {
	// ID is the stable semantic dimension identifier.
	ID DimensionID

	// ValueType describes the values accepted by filters and returned in rows.
	ValueType string

	// Operators lists the filter operations accepted for this dimension.
	Operators []Operator
}

// MeasureMetadata describes one public aggregation.
type MeasureMetadata struct {
	// ID is the stable semantic measure identifier.
	ID MeasureID

	// Unit describes the measure's result unit.
	Unit string

	// Exact reports whether the measure must preserve exact parity.
	Exact bool

	// Sortable reports whether callers may order by the measure.
	Sortable bool
}

type dimensionDefinition struct {
	id          DimensionID
	alias       string
	valueType   string
	operators   map[Operator]struct{}
	expressions map[PlanID]string
}

type measureDefinition struct {
	id          MeasureID
	alias       string
	unit        string
	exact       bool
	sortable    bool
	expressions map[PlanID]string
}

type planKind uint8

const (
	planKindRollup planKind = iota + 1
	planKindFact
)

type planDefinition struct {
	id              PlanID
	kind            planKind
	table           string
	timeColumn      string
	dayExpression   string
	usesFinal       bool
	supportedGrains map[Grain]struct{}
}

type datasetDefinition struct {
	id              DatasetID
	dimensions      map[DimensionID]dimensionDefinition
	measures        map[MeasureID]measureDefinition
	plans           []planDefinition
	maxRange        time.Duration
	factRetention   time.Duration
	defaultLimit    int
	maxLimit        int
	maxDimensions   int
	maxFilterValues int
}

var usageDataset = datasetDefinition{
	id: DatasetUsage,
	dimensions: map[DimensionID]dimensionDefinition{
		DimensionProvider: {
			id:        DimensionProvider,
			alias:     "provider",
			valueType: "string",
			operators: map[Operator]struct{}{
				OperatorEquals: {},
				OperatorIn:     {},
			},
			expressions: map[PlanID]string{
				PlanUsageDaily: "provider",
				PlanUsageFacts: "provider",
			},
		},
		DimensionModel: {
			id:        DimensionModel,
			alias:     "model",
			valueType: "string",
			operators: map[Operator]struct{}{
				OperatorEquals: {},
				OperatorIn:     {},
			},
			expressions: map[PlanID]string{
				PlanUsageDaily: "model",
				PlanUsageFacts: "model",
			},
		},
		DimensionSource: {
			id:        DimensionSource,
			alias:     "source",
			valueType: "string",
			operators: map[Operator]struct{}{
				OperatorEquals: {},
				OperatorIn:     {},
			},
			expressions: map[PlanID]string{
				PlanUsageDaily: "source",
				PlanUsageFacts: "source",
			},
		},
	},
	measures: map[MeasureID]measureDefinition{
		MeasureRequests: {
			id:       MeasureRequests,
			alias:    "requests",
			unit:     "requests",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(requests)",
				PlanUsageFacts: "count()",
			},
		},
		MeasureInputTokens: {
			id:       MeasureInputTokens,
			alias:    "input_tokens",
			unit:     "tokens",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(input_tokens)",
				PlanUsageFacts: "sum(input_tokens)",
			},
		},
		MeasureOutputTokens: {
			id:       MeasureOutputTokens,
			alias:    "output_tokens",
			unit:     "tokens",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(output_tokens)",
				PlanUsageFacts: "sum(output_tokens)",
			},
		},
		MeasureCacheReadInputTokens: {
			id:       MeasureCacheReadInputTokens,
			alias:    "cache_read_input_tokens",
			unit:     "tokens",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(cache_read_input_tokens)",
				PlanUsageFacts: "sum(cache_read_input_tokens)",
			},
		},
		MeasureCacheCreationInputTokens: {
			id:       MeasureCacheCreationInputTokens,
			alias:    "cache_creation_input_tokens",
			unit:     "tokens",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(cache_creation_input_tokens)",
				PlanUsageFacts: "sum(cache_creation_input_tokens)",
			},
		},
		MeasureTotalTokens: {
			id:       MeasureTotalTokens,
			alias:    "total_tokens",
			unit:     "tokens",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(total_tokens)",
				PlanUsageFacts: "sum(total_tokens)",
			},
		},
		MeasureTotalCost: {
			id:       MeasureTotalCost,
			alias:    "total_cost",
			unit:     "currency",
			exact:    true,
			sortable: true,
			expressions: map[PlanID]string{
				PlanUsageDaily: "sum(total_cost)",
				PlanUsageFacts: "sum(total_cost)",
			},
		},
	},
	plans: []planDefinition{
		{
			id:            PlanUsageDaily,
			kind:          planKindRollup,
			table:         "telemetry_usage_daily",
			timeColumn:    "day",
			dayExpression: "day",
			usesFinal:     false,
			supportedGrains: map[Grain]struct{}{
				GrainDay: {},
			},
		},
		{
			id:            PlanUsageFacts,
			kind:          planKindFact,
			table:         "telemetry_usage_facts",
			timeColumn:    "event_time",
			dayExpression: "toStartOfDay(event_time)",
			usesFinal:     true,
			supportedGrains: map[Grain]struct{}{
				GrainNone: {},
				GrainDay:  {},
			},
		},
	},
	maxRange:        730 * 24 * time.Hour,
	factRetention:   90 * 24 * time.Hour,
	defaultLimit:    100,
	maxLimit:        1000,
	maxDimensions:   3,
	maxFilterValues: 100,
}

// UsageDimensions returns a stable, sorted snapshot of the usage dimensions.
func UsageDimensions() []DimensionMetadata {
	result := make([]DimensionMetadata, 0, len(usageDataset.dimensions))
	for _, definition := range usageDataset.dimensions {
		operators := make([]Operator, 0, len(definition.operators))
		for operator := range definition.operators {
			operators = append(operators, operator)
		}
		slices.Sort(operators)
		result = append(result, DimensionMetadata{
			ID:        definition.id,
			ValueType: definition.valueType,
			Operators: operators,
		})
	}
	slices.SortFunc(result, func(a, b DimensionMetadata) int {
		return stringCompare(string(a.ID), string(b.ID))
	})
	return result
}

// UsageMeasures returns a stable, sorted snapshot of the usage measures.
func UsageMeasures() []MeasureMetadata {
	result := make([]MeasureMetadata, 0, len(usageDataset.measures))
	for _, definition := range usageDataset.measures {
		result = append(result, MeasureMetadata{
			ID:       definition.id,
			Unit:     definition.unit,
			Exact:    definition.exact,
			Sortable: definition.sortable,
		})
	}
	slices.SortFunc(result, func(a, b MeasureMetadata) int {
		return stringCompare(string(a.ID), string(b.ID))
	})
	return result
}

func stringCompare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func validateCatalog(dataset datasetDefinition) error {
	planIDs := make(map[PlanID]struct{}, len(dataset.plans))
	for _, plan := range dataset.plans {
		if plan.id == "" || plan.table == "" || plan.timeColumn == "" {
			return fmt.Errorf("plan has incomplete physical metadata")
		}
		if _, exists := planIDs[plan.id]; exists {
			return fmt.Errorf("plan %q is duplicated", plan.id)
		}
		planIDs[plan.id] = struct{}{}
	}

	aliases := make(map[string]string, len(dataset.dimensions)+len(dataset.measures))
	for id, dimension := range dataset.dimensions {
		if dimension.id != id || dimension.alias == "" || len(dimension.expressions) == 0 {
			return fmt.Errorf("dimension %q has incomplete metadata", id)
		}
		if previous, exists := aliases[dimension.alias]; exists {
			return fmt.Errorf("dimension %q reuses alias %q from %s", id, dimension.alias, previous)
		}
		aliases[dimension.alias] = "dimension " + string(id)
		for planID := range dimension.expressions {
			if _, exists := planIDs[planID]; !exists {
				return fmt.Errorf("dimension %q references unknown plan %q", id, planID)
			}
		}
	}

	for id, measure := range dataset.measures {
		if measure.id != id || measure.alias == "" || len(measure.expressions) == 0 {
			return fmt.Errorf("measure %q has incomplete metadata", id)
		}
		if previous, exists := aliases[measure.alias]; exists {
			return fmt.Errorf("measure %q reuses alias %q from %s", id, measure.alias, previous)
		}
		aliases[measure.alias] = "measure " + string(id)
		for planID := range measure.expressions {
			if _, exists := planIDs[planID]; !exists {
				return fmt.Errorf("measure %q references unknown plan %q", id, planID)
			}
		}
	}

	return nil
}
