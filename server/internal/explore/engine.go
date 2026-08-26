package explore

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Conn is the minimal ClickHouse connection surface the engine needs.
type Conn interface {
	Query(ctx context.Context, query string, args ...any) (driver.Rows, error)
	Exec(ctx context.Context, query string, args ...any) error
}

var sq = squirrel.StatementBuilder.PlaceholderFormat(squirrel.Question)

// Calculation is one aggregate operation over a semantic dataset field.
type Calculation struct {
	Op     string `json:"op"`
	Column string `json:"column,omitempty"`
}

func (c Calculation) Canonical() string {
	if c.Column == "" {
		return c.Op
	}
	return c.Op + "(" + c.Column + ")"
}

// Query is the engine's dataset-and-calculations query shape.
type Query struct {
	Dataset string

	Calculations []Calculation

	GroupBy []string

	GroupExpressions []GroupExpression

	Filters []Filter

	TimeStart int64
	TimeEnd   int64

	GranularitySeconds int64

	ProjectIDs []uuid.UUID

	SortBy string

	SortDesc bool

	Limit int
}

// Filter is one row predicate over a canonical field.
type Filter struct {
	Dimension string   `json:"dimension"`
	Op        string   `json:"op,omitempty"`
	Values    []string `json:"values"`
}

// GroupExpression is one named conditional grouping axis.
type GroupExpression struct {
	Name      string   `json:"name"`
	Dimension string   `json:"dimension"`
	Op        string   `json:"op,omitempty"`
	Values    []string `json:"values"`
}

type planFilter struct {
	Target   string
	Column   string
	Presence string
	Op       string
	Values   []string
	Number   float64
	Measure  bool
}

func resolveFilter(ds *Dataset, filter Filter) (planFilter, error) {
	resolved := planFilter{
		Target:   filter.Dimension,
		Column:   "",
		Presence: "",
		Op:       filter.Op,
		Values:   filter.Values,
		Number:   0,
		Measure:  false,
	}
	if resolved.Op == "" {
		resolved.Op = "in"
	}

	field, ok := ds.fieldByName(filter.Dimension)
	if !ok {
		return resolved, &UnknownMemberError{
			Kind:   "field",
			Name:   filter.Dimension,
			Detail: fmt.Sprintf("dataset %q declares no such field", ds.Name),
		}
	}
	if !slices.Contains(field.filterOps(), resolved.Op) {
		return resolved, &QueryValidationError{Msg: fmt.Sprintf(
			"operator %q is not legal on %s field %q; legal operators: [%s]",
			resolved.Op,
			field.Type,
			field.Name,
			strings.Join(field.filterOps(), ", "),
		)}
	}

	resolved.Column = field.canonicalExpr()
	resolved.Measure = field.Role == FieldRoleMeasure
	if resolved.Measure {
		resolved.Presence = field.presenceExpr()
	}

	switch resolved.Op {
	case "in", "not_in":
		if len(filter.Values) == 0 {
			return resolved, &QueryValidationError{Msg: fmt.Sprintf(
				"filter %s %s needs at least one value",
				field.Name,
				resolved.Op,
			)}
		}
	case "contains":
		if len(filter.Values) != 1 || filter.Values[0] == "" {
			return resolved, &QueryValidationError{Msg: fmt.Sprintf(
				"filter %s contains needs exactly one non-empty value",
				field.Name,
			)}
		}
	case "exists":
	default:
		if len(filter.Values) != 1 {
			return resolved, &QueryValidationError{Msg: fmt.Sprintf(
				"filter %s %s needs exactly one numeric value",
				field.Name,
				resolved.Op,
			)}
		}
		number, err := strconv.ParseFloat(filter.Values[0], 64)
		if err != nil {
			return resolved, &QueryValidationError{Msg: fmt.Sprintf(
				"filter %s %s: %q is not a number",
				field.Name,
				resolved.Op,
				filter.Values[0],
			)}
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return resolved, &QueryValidationError{Msg: fmt.Sprintf(
				"filter %s %s: %q is not a finite number",
				field.Name,
				resolved.Op,
				filter.Values[0],
			)}
		}
		resolved.Number = number
	}

	return resolved, nil
}

func (filter planFilter) predicate() (squirrel.Sqlizer, error) {
	var predicate squirrel.Sqlizer
	switch filter.Op {
	case "in":
		predicate = squirrel.Eq{filter.Column: filter.Values}
	case "not_in":
		predicate = squirrel.NotEq{filter.Column: filter.Values}
	case "contains":
		predicate = squirrel.Expr(
			"positionCaseInsensitive("+filter.Column+", ?) > 0",
			filter.Values[0],
		)
	case "exists":
		predicate = squirrel.Expr(filter.Column + " != ''")
	case "eq":
		predicate = squirrel.Expr(filter.Column+" = ?", filter.Number)
	case "neq":
		predicate = squirrel.Expr(filter.Column+" != ?", filter.Number)
	case "gt":
		predicate = squirrel.Expr(filter.Column+" > ?", filter.Number)
	case "gte":
		predicate = squirrel.Expr(filter.Column+" >= ?", filter.Number)
	case "lt":
		predicate = squirrel.Expr(filter.Column+" < ?", filter.Number)
	case "lte":
		predicate = squirrel.Expr(filter.Column+" <= ?", filter.Number)
	default:
		return nil, fmt.Errorf("unsupported resolved filter operator %q", filter.Op)
	}
	if !filter.Measure {
		return predicate, nil
	}
	return squirrel.And{
		squirrel.Expr(filter.Presence + " = 1"),
		predicate,
	}, nil
}

type planCalculation struct {
	Canonical string
	Op        string
	Field     *Field
}

func resolveCalculation(ds *Dataset, calculation Calculation) (planCalculation, error) {
	resolved := planCalculation{
		Canonical: calculation.Canonical(),
		Op:        calculation.Op,
		Field:     nil,
	}

	switch calculation.Op {
	case "COUNT":
		if calculation.Column != "" {
			return resolved, &QueryValidationError{Msg: "COUNT takes no column"}
		}
		return resolved, nil
	case "COUNT_DISTINCT":
		field, ok := ds.fieldByName(calculation.Column)
		if !ok || field.Role != FieldRoleDimension {
			return resolved, &UnknownMemberError{
				Kind:   "column",
				Name:   calculation.Column,
				Detail: fmt.Sprintf("COUNT_DISTINCT targets one of dataset %q's dimensions", ds.Name),
			}
		}
		resolved.Field = field
		return resolved, nil
	case "SUM", "AVG", "MIN", "MAX", "P50", "P95", "P99":
		field, ok := ds.fieldByName(calculation.Column)
		if !ok || field.Role != FieldRoleMeasure {
			return resolved, &UnknownMemberError{
				Kind:   "column",
				Name:   calculation.Column,
				Detail: fmt.Sprintf("dataset %q's measures are [%s]", ds.Name, strings.Join(ds.measureNames(), ", ")),
			}
		}
		resolved.Field = field
		return resolved, nil
	default:
		return resolved, &UnknownMemberError{
			Kind:   "op",
			Name:   calculation.Op,
			Detail: "supported: COUNT, COUNT_DISTINCT, SUM, AVG, MIN, MAX, P50, P95, P99",
		}
	}
}

func (calculation *planCalculation) sql() string {
	var expression string
	switch calculation.Op {
	case "COUNT":
		expression = "count()"
	case "COUNT_DISTINCT":
		column := calculation.Field.canonicalExpr()
		expression = fmt.Sprintf("uniqExactIf(%s, %s != '')", column, column)
	case "SUM":
		expression = fmt.Sprintf("sum(%s)", calculation.Field.canonicalExpr())
	case "AVG":
		expression = fmt.Sprintf(
			"avgIf(%s, %s = 1)",
			calculation.Field.canonicalExpr(),
			calculation.Field.presenceExpr(),
		)
	case "MIN":
		expression = fmt.Sprintf(
			"minIf(%s, %s = 1)",
			calculation.Field.canonicalExpr(),
			calculation.Field.presenceExpr(),
		)
	case "MAX":
		expression = fmt.Sprintf(
			"maxIf(%s, %s = 1)",
			calculation.Field.canonicalExpr(),
			calculation.Field.presenceExpr(),
		)
	case "P50", "P95", "P99":
		level := map[string]string{
			"P50": "0.5",
			"P95": "0.95",
			"P99": "0.99",
		}[calculation.Op]
		expression = fmt.Sprintf(
			"quantileIf(%s)(%s, %s = 1)",
			level,
			calculation.Field.canonicalExpr(),
			calculation.Field.presenceExpr(),
		)
	}
	return fmt.Sprintf("ifNull(ifNotFinite(toFloat64(%s), 0), 0)", expression)
}

type queryPlan struct {
	Query   Query
	Dataset *Dataset

	Table string

	TimeColumn string

	Dimensions map[string]string

	GroupBy []string

	GroupExpressions []planGroupExpression

	Filters []planFilter

	Calculations []planCalculation

	SortIndex int
}

type planGroupExpression struct {
	Name      string
	Predicate planFilter
}

func (p *queryPlan) groupNames() []string {
	names := make([]string, 0, len(p.GroupBy)+len(p.GroupExpressions))
	names = append(names, p.GroupBy...)
	for _, expression := range p.GroupExpressions {
		names = append(names, expression.Name)
	}
	return names
}

func plan(query Query) (*queryPlan, error) {
	if query.Dataset == "" {
		return nil, &QueryValidationError{Msg: "query requires a dataset"}
	}
	if len(query.Calculations) == 0 {
		return nil, &QueryValidationError{Msg: "query requests no calculations"}
	}
	if query.GranularitySeconds != 0 && query.GranularitySeconds < minGranularitySeconds {
		return nil, &QueryValidationError{Msg: fmt.Sprintf(
			"granularity_seconds must be 0 or >= %d",
			minGranularitySeconds,
		)}
	}

	dataset, ok := datasetByName(query.Dataset)
	if !ok {
		return nil, &UnknownMemberError{Kind: "dataset", Name: query.Dataset, Detail: ""}
	}

	calculations := make([]planCalculation, 0, len(query.Calculations))
	seenCalculations := make(map[string]bool, len(query.Calculations))
	for _, calculation := range query.Calculations {
		resolved, err := resolveCalculation(dataset, calculation)
		if err != nil {
			return nil, err
		}
		if seenCalculations[resolved.Canonical] {
			continue
		}
		seenCalculations[resolved.Canonical] = true
		calculations = append(calculations, resolved)
	}

	dimensions := make(map[string]string, len(query.GroupBy))
	groupNames := make(map[string]bool, len(query.GroupBy)+len(query.GroupExpressions))
	for _, name := range query.GroupBy {
		if _, duplicate := dimensions[name]; duplicate {
			return nil, &QueryValidationError{Msg: fmt.Sprintf(
				"dimension %q appears twice in group_by",
				name,
			)}
		}
		column, ok := dataset.dimensionColumn(name)
		if !ok {
			return nil, &UnknownMemberError{
				Kind:   "dimension",
				Name:   name,
				Detail: fmt.Sprintf("dataset %q carries no dimension %q", dataset.Name, name),
			}
		}
		dimensions[name] = column
		groupNames[name] = true
	}

	groupExpressions := make([]planGroupExpression, 0, len(query.GroupExpressions))
	for _, expression := range query.GroupExpressions {
		nameKey := strings.TrimSpace(expression.Name)
		if nameKey == "" {
			return nil, &QueryValidationError{Msg: "group expression name must not be empty"}
		}
		if groupNames[nameKey] {
			return nil, &QueryValidationError{Msg: fmt.Sprintf(
				"group name %q appears more than once",
				expression.Name,
			)}
		}
		resolved, err := resolveFilter(dataset, Filter{
			Dimension: expression.Dimension,
			Op:        expression.Op,
			Values:    expression.Values,
		})
		if err != nil {
			return nil, err
		}
		groupNames[nameKey] = true
		groupExpressions = append(groupExpressions, planGroupExpression{
			Name:      expression.Name,
			Predicate: resolved,
		})
	}

	filters := make([]planFilter, 0, len(query.Filters))
	for _, filter := range query.Filters {
		resolved, err := resolveFilter(dataset, filter)
		if err != nil {
			return nil, err
		}
		filters = append(filters, resolved)
	}

	sortIndex := -1
	if query.SortBy != "" {
		for i := range calculations {
			if calculations[i].Canonical == query.SortBy {
				sortIndex = i
				break
			}
		}
		if sortIndex < 0 {
			return nil, &QueryValidationError{Msg: fmt.Sprintf(
				"sort_by %q is not among the requested calculations",
				query.SortBy,
			)}
		}
	}

	return &queryPlan{
		Query:            query,
		Dataset:          dataset,
		Table:            dataset.Table,
		TimeColumn:       canonicalColumn(dataset.TimeColumn),
		Dimensions:       dimensions,
		GroupBy:          slices.Clone(query.GroupBy),
		GroupExpressions: groupExpressions,
		Filters:          filters,
		Calculations:     calculations,
		SortIndex:        sortIndex,
	}, nil
}

// compileCanonical builds the dataset-specific authority pipeline shared by
// aggregate and dimension-value queries.
func compileCanonical(dataset *Dataset, query Query) (string, []any, error) {
	switch dataset.CanonicalStrategy {
	case CanonicalStrategyStandard:
		return compileStandardCanonical(dataset, query)
	case CanonicalStrategyTurn:
		return compileTurnCanonical(dataset, query)
	default:
		return "", nil, fmt.Errorf(
			"dataset %q has unsupported canonical strategy %q",
			dataset.Name,
			dataset.CanonicalStrategy,
		)
	}
}

func scopedObservationBuilder(dataset *Dataset, query Query, columns []string) squirrel.SelectBuilder {
	builder := sq.Select(columns...).From(dataset.Table)
	if dataset.MeasurementName != "" {
		builder = builder.Where("measurement_name = ?", dataset.MeasurementName)
	}
	return builder.
		Where(squirrel.Eq{"project_id": query.ProjectIDs}).
		Where(dataset.TimeColumn+" >= fromUnixTimestamp64Nano(?)", query.TimeStart).
		Where(dataset.TimeColumn+" <= fromUnixTimestamp64Nano(?)", query.TimeEnd)
}

func compileStandardCanonical(dataset *Dataset, query Query) (string, []any, error) {
	columns := []string{
		"project_id",
		"natural_id",
		"argMax(occurred_at, " + authorityWeightExpr("occurred_at") + ") AS " + canonicalColumn("occurred_at"),
	}
	for _, field := range dataset.canonicalFields() {
		columns = append(columns, standardCanonicalFieldExpr(field))
	}

	builder := scopedObservationBuilder(dataset, query, columns).
		GroupBy("project_id", "natural_id")

	sql, args, err := builder.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building canonical %s query: %w", dataset.Name, err)
	}
	return sql, args, nil
}

func compileTurnCanonical(dataset *Dataset, query Query) (string, []any, error) {
	fields := dataset.canonicalFields()
	componentColumns := []string{
		"project_id",
		"natural_id",
		"source_channel",
		"observation_kind",
		"component_id",
		"argMax(occurred_at, " + observationWeightExpr() + ") AS " + componentColumn("occurred_at"),
		"argMax(observed_at, " + observationWeightExpr() + ") AS " + componentColumn("observed_at"),
		"argMax(src_event_id, " + observationWeightExpr() + ") AS " + componentColumn("src_event_id"),
	}
	for _, field := range fields {
		componentColumns = append(componentColumns, componentCanonicalFieldExpr(field))
	}

	componentBuilder := scopedObservationBuilder(dataset, query, componentColumns).
		GroupBy(
			"project_id",
			"natural_id",
			"source_channel",
			"observation_kind",
			"component_id",
		)
	componentSQL, args, err := componentBuilder.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building turn component query: %w", err)
	}

	providerColumns := []string{
		"project_id",
		"natural_id",
		"'provider_otel' AS source_channel",
		"max(" + componentColumn("occurred_at") + ") AS " + candidateColumn("occurred_at"),
		"max(" + componentColumn("observed_at") + ") AS " + candidateColumn("observed_at"),
		"argMax(" + componentColumn("src_event_id") + ", " + componentWeightExpr() + ") AS " + candidateColumn("src_event_id"),
	}
	agentColumns := []string{
		"project_id",
		"natural_id",
		"'agent_hook' AS source_channel",
		"argMax(" + componentColumn("occurred_at") + ", " + componentWeightExpr() + ") AS " + candidateColumn("occurred_at"),
		"argMax(" + componentColumn("observed_at") + ", " + componentWeightExpr() + ") AS " + candidateColumn("observed_at"),
		"argMax(" + componentColumn("src_event_id") + ", " + componentWeightExpr() + ") AS " + candidateColumn("src_event_id"),
	}
	for _, field := range fields {
		if field.Role == FieldRoleMeasure {
			providerColumns = append(
				providerColumns,
				fmt.Sprintf(
					"if(countIf(isNotNull(%s)) > 0, sumIf(%s, isNotNull(%s)), NULL) AS %s",
					componentColumn(field.Name),
					componentColumn(field.Name),
					componentColumn(field.Name),
					candidateColumn(field.Name),
				),
			)
		} else {
			providerColumns = append(
				providerColumns,
				fmt.Sprintf(
					"argMaxIf(%s, %s, isNotNull(%s)) AS %s",
					componentColumn(field.Name),
					componentWeightExpr(),
					componentColumn(field.Name),
					candidateColumn(field.Name),
				),
			)
		}

		agentColumns = append(
			agentColumns,
			fmt.Sprintf(
				"argMaxIf(%s, %s, isNotNull(%s)) AS %s",
				componentColumn(field.Name),
				componentWeightExpr(),
				componentColumn(field.Name),
				candidateColumn(field.Name),
			),
		)
	}

	providerSQL := sq.Select(providerColumns...).
		From("components").
		Where("source_channel = 'provider_otel'").
		Where("observation_kind = 'component'").
		GroupBy("project_id", "natural_id")
	agentSQL := sq.Select(agentColumns...).
		From("components").
		Where("source_channel = 'agent_hook'").
		Where("observation_kind = 'total'").
		GroupBy("project_id", "natural_id")

	providerQuery, _, err := providerSQL.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building provider turn candidate query: %w", err)
	}
	agentQuery, _, err := agentSQL.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building agent turn candidate query: %w", err)
	}

	finalColumns := []string{
		"project_id",
		"natural_id",
		"argMax(" + candidateColumn("occurred_at") + ", " + candidateAuthorityWeightExpr("occurred_at") + ") AS " + canonicalColumn("occurred_at"),
	}
	for _, field := range fields {
		finalColumns = append(finalColumns, fmt.Sprintf(
			"argMaxIf(%s, %s, isNotNull(%s)) AS %s",
			candidateColumn(field.Name),
			candidateAuthorityWeightExpr(field.Name),
			candidateColumn(field.Name),
			canonicalColumn(field.Name),
		))
	}

	canonicalSQL := fmt.Sprintf(
		"WITH components AS (%s), source_candidates AS (%s UNION ALL %s) SELECT %s FROM source_candidates GROUP BY project_id, natural_id",
		componentSQL,
		providerQuery,
		agentQuery,
		strings.Join(finalColumns, ", "),
	)
	return canonicalSQL, args, nil
}

// compile turns a validated plan into ClickHouse SQL. User filters and
// conditional groups operate only on the final canonical rows.
func compile(queryPlan *queryPlan) (string, []any, error) {
	query := queryPlan.Query
	canonicalSQL, canonicalArgs, err := compileCanonical(queryPlan.Dataset, query)
	if err != nil {
		return "", nil, err
	}

	var builder squirrel.SelectBuilder
	timeseries := query.GranularitySeconds > 0
	if timeseries {
		bucketExpression := "toInt64(toStartOfInterval(" +
			queryPlan.TimeColumn +
			", toIntervalSecond(?))) * 1000000000 AS bucket_time_unix_nano"
		builder = sq.Select().Column(squirrel.Expr(bucketExpression, query.GranularitySeconds))
	} else {
		builder = sq.Select()
	}

	groupColumns := make([]string, 0, len(queryPlan.GroupBy)+len(queryPlan.GroupExpressions))
	for i, dimension := range queryPlan.GroupBy {
		alias := fmt.Sprintf("g_%d", i)
		builder = builder.Column("ifNull(" + queryPlan.Dimensions[dimension] + ", '') AS " + alias)
		groupColumns = append(groupColumns, alias)
	}
	for i, expression := range queryPlan.GroupExpressions {
		alias := fmt.Sprintf("g_%d", len(queryPlan.GroupBy)+i)
		predicate, err := expression.Predicate.predicate()
		if err != nil {
			return "", nil, fmt.Errorf("building group expression %q: %w", expression.Name, err)
		}
		predicateSQL, predicateArgs, err := predicate.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("building group expression %q predicate: %w", expression.Name, err)
		}
		builder = builder.Column(squirrel.Expr(
			"if("+predicateSQL+", 'true', 'false') AS "+alias,
			predicateArgs...,
		))
		groupColumns = append(groupColumns, alias)
	}
	for i := range queryPlan.Calculations {
		builder = builder.Column(fmt.Sprintf(
			"%s AS m_%d",
			queryPlan.Calculations[i].sql(),
			i,
		))
	}

	builder = builder.
		Prefix("WITH canonical AS ("+canonicalSQL+")", canonicalArgs...).
		From("canonical").
		Where(queryPlan.TimeColumn+" >= fromUnixTimestamp64Nano(?)", query.TimeStart).
		Where(queryPlan.TimeColumn+" <= fromUnixTimestamp64Nano(?)", query.TimeEnd)

	for _, filter := range queryPlan.Filters {
		predicate, err := filter.predicate()
		if err != nil {
			return "", nil, fmt.Errorf("building filter predicate: %w", err)
		}
		builder = builder.Where(predicate)
	}

	switch {
	case timeseries:
		builder = builder.
			GroupBy(append([]string{"bucket_time_unix_nano"}, groupColumns...)...).
			OrderBy(append([]string{"bucket_time_unix_nano"}, groupColumns...)...)
	case len(groupColumns) > 0:
		builder = builder.GroupBy(groupColumns...)
		if queryPlan.SortIndex >= 0 {
			direction := " ASC"
			if query.SortDesc {
				direction = " DESC"
			}
			builder = builder.OrderBy(append(
				[]string{fmt.Sprintf("m_%d%s", queryPlan.SortIndex, direction)},
				groupColumns...,
			)...)
		} else {
			builder = builder.OrderBy(groupColumns...)
		}
		if query.Limit > 0 {
			builder = builder.Limit(uint64(query.Limit)) // #nosec G115 -- design validates non-negative values
		}
	}

	sql, args, err := builder.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("building explore query: %w", err)
	}
	return sql, args, nil
}

type resultRow struct {
	BucketUnixNano int64
	Group          []string
	Values         map[string]float64
}

type queryResult struct {
	Rows []resultRow

	Dataset string
}

func execute(ctx context.Context, conn Conn, queryPlan *queryPlan) (*queryResult, error) {
	sql, args, err := compile(queryPlan)
	if err != nil {
		return nil, err
	}

	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("executing explore query: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return rows.Close() })

	timeseries := queryPlan.Query.GranularitySeconds > 0
	resultRows := make([]resultRow, 0)
	for rows.Next() {
		row, err := scanRow(rows, queryPlan, timeseries)
		if err != nil {
			return nil, err
		}
		resultRows = append(resultRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading explore query rows: %w", err)
	}

	return &queryResult{Rows: resultRows, Dataset: queryPlan.Dataset.Name}, nil
}

func scanRow(rows driver.Rows, queryPlan *queryPlan, timeseries bool) (resultRow, error) {
	var bucket int64
	group := make([]string, len(queryPlan.GroupBy)+len(queryPlan.GroupExpressions))
	values := make([]float64, len(queryPlan.Calculations))

	destinations := make([]any, 0, 1+len(group)+len(values))
	if timeseries {
		destinations = append(destinations, &bucket)
	}
	for i := range group {
		destinations = append(destinations, &group[i])
	}
	for i := range values {
		destinations = append(destinations, &values[i])
	}

	if err := rows.Scan(destinations...); err != nil {
		return resultRow{}, fmt.Errorf("scanning explore query row: %w", err)
	}

	resultValues := make(map[string]float64, len(queryPlan.Calculations))
	for i := range queryPlan.Calculations {
		resultValues[queryPlan.Calculations[i].Canonical] = values[i]
	}

	return resultRow{
		BucketUnixNano: bucket,
		Group:          group,
		Values:         resultValues,
	}, nil
}
