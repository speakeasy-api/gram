package analytics

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

const timeBucketAlias = "time_bucket"

// RollupAvailability records the complete interval served by a rollup.
type RollupAvailability struct {
	coverageStart time.Time
	freshThrough  time.Time
}

// NewRollupAvailability creates rollup coverage metadata. A zero value means
// the rollup is unavailable and forces a fact-plan fallback.
func NewRollupAvailability(coverageStart, freshThrough time.Time) (RollupAvailability, error) {
	if coverageStart.IsZero() || freshThrough.IsZero() {
		return RollupAvailability{}, fmt.Errorf("%w: rollup coverage bounds are required", ErrInvalidQuery)
	}
	coverageStart = coverageStart.UTC()
	freshThrough = freshThrough.UTC()
	if !coverageStart.Before(freshThrough) {
		return RollupAvailability{}, fmt.Errorf("%w: rollup coverage start must precede freshness", ErrInvalidQuery)
	}

	return RollupAvailability{
		coverageStart: coverageStart,
		freshThrough:  freshThrough,
	}, nil
}

// Planner validates semantic queries and selects an eligible physical source.
type Planner struct {
	now             time.Time
	usageDailyState RollupAvailability
}

// NewPlanner creates a deterministic planner for the supplied evaluation time.
func NewPlanner(now time.Time, usageDailyState RollupAvailability) Planner {
	return Planner{
		now:             now.UTC(),
		usageDailyState: usageDailyState,
	}
}

// CompiledQuery is the repository-facing output of semantic compilation.
type CompiledQuery struct {
	// PlanID is a stable diagnostic identifier rather than a physical table name.
	PlanID PlanID

	// SQL is compiler-owned ClickHouse SQL with parameter placeholders.
	SQL string

	// Args are the bound values for SQL in placeholder order.
	Args []any
}

// Compile validates a semantic query, selects a source, and emits ClickHouse SQL.
func (p Planner) Compile(query Query) (CompiledQuery, error) {
	if err := validateCatalog(usageDataset); err != nil {
		return CompiledQuery{}, fmt.Errorf("validate usage catalog: %w", err)
	}
	if err := validateQuery(query, usageDataset); err != nil {
		return CompiledQuery{}, err
	}

	plan, err := p.selectPlan(query, usageDataset)
	if err != nil {
		return CompiledQuery{}, err
	}

	compiled, err := compileQuery(query, usageDataset, plan)
	if err != nil {
		return CompiledQuery{}, fmt.Errorf("compile %s query with %s: %w", query.dataset, plan.id, err)
	}
	return compiled, nil
}

func validateQuery(query Query, dataset datasetDefinition) error {
	if query.dataset != dataset.id {
		return fmt.Errorf("%w: dataset %q is not cataloged", ErrInvalidQuery, query.dataset)
	}
	if query.scope.organizationID == uuid.Nil || len(query.scope.projectIDs) == 0 {
		return fmt.Errorf("%w: tenant scope is required", ErrInvalidQuery)
	}
	if query.timeRange.from.IsZero() || query.timeRange.to.IsZero() || !query.timeRange.from.Before(query.timeRange.to) {
		return fmt.Errorf("%w: a valid half-open time range is required", ErrInvalidQuery)
	}
	if query.timeRange.to.Sub(query.timeRange.from) > dataset.maxRange {
		return fmt.Errorf("%w: range exceeds %s", ErrInvalidQuery, dataset.maxRange)
	}
	if len(query.measures) == 0 {
		return fmt.Errorf("%w: at least one measure is required", ErrInvalidQuery)
	}
	if len(query.dimensions) > dataset.maxDimensions {
		return fmt.Errorf("%w: at most %d dimensions are allowed", ErrInvalidQuery, dataset.maxDimensions)
	}
	if query.grain != GrainNone && query.grain != GrainDay {
		return fmt.Errorf("%w: grain %q is not cataloged", ErrInvalidQuery, query.grain)
	}
	if query.limit < 0 || query.limit > dataset.maxLimit {
		return fmt.Errorf("%w: limit must be between 0 and %d", ErrInvalidQuery, dataset.maxLimit)
	}

	seenDimensions := make(map[DimensionID]struct{}, len(query.dimensions))
	for _, dimensionID := range query.dimensions {
		if _, exists := dataset.dimensions[dimensionID]; !exists {
			return fmt.Errorf("%w: dimension %q is not cataloged", ErrInvalidQuery, dimensionID)
		}
		if _, exists := seenDimensions[dimensionID]; exists {
			return fmt.Errorf("%w: dimension %q is duplicated", ErrInvalidQuery, dimensionID)
		}
		seenDimensions[dimensionID] = struct{}{}
	}

	seenMeasures := make(map[MeasureID]struct{}, len(query.measures))
	for _, measureID := range query.measures {
		if _, exists := dataset.measures[measureID]; !exists {
			return fmt.Errorf("%w: measure %q is not cataloged", ErrInvalidQuery, measureID)
		}
		if _, exists := seenMeasures[measureID]; exists {
			return fmt.Errorf("%w: measure %q is duplicated", ErrInvalidQuery, measureID)
		}
		seenMeasures[measureID] = struct{}{}
	}

	for _, filter := range query.filters {
		dimension, exists := dataset.dimensions[filter.dimension]
		if !exists {
			return fmt.Errorf("%w: filter dimension %q is not cataloged", ErrInvalidQuery, filter.dimension)
		}
		if _, exists := dimension.operators[filter.operator]; !exists {
			return fmt.Errorf("%w: operator %q is not supported by %q", ErrInvalidQuery, filter.operator, filter.dimension)
		}
		if len(filter.values) == 0 || len(filter.values) > dataset.maxFilterValues {
			return fmt.Errorf("%w: filter %q requires 1 to %d values", ErrInvalidQuery, filter.dimension, dataset.maxFilterValues)
		}
		if filter.operator == OperatorEquals && len(filter.values) != 1 {
			return fmt.Errorf("%w: equals filter %q requires one value", ErrInvalidQuery, filter.dimension)
		}
	}

	for _, order := range query.orders {
		measure, exists := dataset.measures[order.measure]
		if !exists || !measure.sortable {
			return fmt.Errorf("%w: measure %q is not sortable", ErrInvalidQuery, order.measure)
		}
		if _, selected := seenMeasures[order.measure]; !selected {
			return fmt.Errorf("%w: ordered measure %q must be selected", ErrInvalidQuery, order.measure)
		}
		if order.direction != DirectionAscending && order.direction != DirectionDescending {
			return fmt.Errorf("%w: order direction %q is invalid", ErrInvalidQuery, order.direction)
		}
	}

	return nil
}

func (p Planner) selectPlan(query Query, dataset datasetDefinition) (planDefinition, error) {
	for _, plan := range dataset.plans {
		if !planSupportsQuery(plan, query, dataset) {
			continue
		}

		switch plan.kind {
		case planKindRollup:
			if !isDayAligned(query.timeRange.from) || !isDayAligned(query.timeRange.to) {
				continue
			}
			if p.usageDailyState.coverageStart.IsZero() || p.usageDailyState.freshThrough.IsZero() {
				continue
			}
			if query.timeRange.from.Before(p.usageDailyState.coverageStart) || query.timeRange.to.After(p.usageDailyState.freshThrough) {
				continue
			}
			return plan, nil
		case planKindFact:
			if p.now.IsZero() {
				continue
			}
			if query.timeRange.from.Before(p.now.Add(-dataset.factRetention)) {
				continue
			}
			return plan, nil
		}
	}

	return planDefinition{}, fmt.Errorf("%w: dataset=%s range=[%s,%s) grain=%s", ErrNoCompatiblePlan, query.dataset, query.timeRange.from.Format(time.RFC3339), query.timeRange.to.Format(time.RFC3339), query.grain)
}

func planSupportsQuery(plan planDefinition, query Query, dataset datasetDefinition) bool {
	if _, supported := plan.supportedGrains[query.grain]; !supported {
		return false
	}
	for _, dimensionID := range query.dimensions {
		if _, supported := dataset.dimensions[dimensionID].expressions[plan.id]; !supported {
			return false
		}
	}
	for _, measureID := range query.measures {
		if _, supported := dataset.measures[measureID].expressions[plan.id]; !supported {
			return false
		}
	}
	for _, filter := range query.filters {
		if _, supported := dataset.dimensions[filter.dimension].expressions[plan.id]; !supported {
			return false
		}
	}
	return true
}

func compileQuery(query Query, dataset datasetDefinition, plan planDefinition) (CompiledQuery, error) {
	selects := make([]string, 0, 1+len(query.dimensions)+len(query.measures))
	groups := make([]string, 0, 1+len(query.dimensions))
	if query.grain == GrainDay {
		selects = append(selects, plan.dayExpression+" AS "+timeBucketAlias)
		groups = append(groups, timeBucketAlias)
	}
	for _, dimensionID := range query.dimensions {
		dimension := dataset.dimensions[dimensionID]
		selects = append(selects, dimension.expressions[plan.id]+" AS "+dimension.alias)
		groups = append(groups, dimension.alias)
	}
	for _, measureID := range query.measures {
		measure := dataset.measures[measureID]
		selects = append(selects, measure.expressions[plan.id]+" AS "+measure.alias)
	}

	table := plan.table
	if plan.usesFinal {
		table += " FINAL"
	}
	builder := squirrel.StatementBuilder.
		PlaceholderFormat(squirrel.Question).
		Select(selects...).
		From(table).
		Where(squirrel.Eq{"organization_id": query.scope.organizationID}).
		Where(squirrel.Eq{"project_id": query.scope.projectIDs}).
		Where(squirrel.GtOrEq{plan.timeColumn: query.timeRange.from}).
		Where(squirrel.Lt{plan.timeColumn: query.timeRange.to})

	for _, filter := range query.filters {
		expression := dataset.dimensions[filter.dimension].expressions[plan.id]
		switch filter.operator {
		case OperatorEquals:
			builder = builder.Where(squirrel.Eq{expression: filter.values[0]})
		case OperatorIn:
			builder = builder.Where(squirrel.Eq{expression: filter.values})
		}
	}
	if len(groups) > 0 {
		builder = builder.GroupBy(groups...)
	}

	if len(query.orders) > 0 {
		for _, order := range query.orders {
			alias := dataset.measures[order.measure].alias
			builder = builder.OrderBy(alias + " " + strings.ToUpper(string(order.direction)))
		}
	} else {
		if query.grain == GrainDay {
			builder = builder.OrderBy(timeBucketAlias + " ASC")
		}
		for _, dimensionID := range query.dimensions {
			builder = builder.OrderBy(dataset.dimensions[dimensionID].alias + " ASC")
		}
	}

	limit := query.limit
	if limit == 0 {
		limit = dataset.defaultLimit
	}
	builder = builder.Limit(uint64(limit))

	sql, args, err := builder.ToSql()
	if err != nil {
		return CompiledQuery{}, err
	}
	return CompiledQuery{
		PlanID: plan.id,
		SQL:    sql,
		Args:   slices.Clone(args),
	}, nil
}

func isDayAligned(value time.Time) bool {
	value = value.UTC()
	startOfDay := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
	return value.Equal(startOfDay)
}
