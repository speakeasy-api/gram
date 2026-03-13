package chq

import (
	"fmt"
	"strings"
)

// ----------------------------------------------------------------------------
// SelectBuilder
// ----------------------------------------------------------------------------

// WithFillClause describes the ClickHouse ORDER BY ... WITH FILL range.
// ClickHouse uses WITH FILL to generate missing rows in a result set, which
// is essential for time-series gap-filling.
type WithFillClause struct {
	// Column is the ORDER BY column this fill applies to.
	Column string
	// Desc reverses the fill direction (ORDER BY col DESC WITH FILL FROM high TO low).
	Desc bool
	// From / To / Step are all optional Sqlizers so they can be literal values
	// or parametric expressions.
	From Sqlizer
	To   Sqlizer
	Step Sqlizer
}

func (w WithFillClause) render() (string, []any, error) {
	var b strings.Builder
	var args []any

	dir := "ASC"
	if w.Desc {
		dir = "DESC"
	}
	b.WriteString(w.Column)
	b.WriteString(" ")
	b.WriteString(dir)
	b.WriteString(" WITH FILL")

	if w.From != nil {
		s, a, err := w.From.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("WithFill FROM: %w", err)
		}
		b.WriteString(" FROM ")
		b.WriteString(s)
		args = append(args, a...)
	}
	if w.To != nil {
		s, a, err := w.To.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("WithFill TO: %w", err)
		}
		b.WriteString(" TO ")
		b.WriteString(s)
		args = append(args, a...)
	}
	if w.Step != nil {
		s, a, err := w.Step.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("WithFill STEP: %w", err)
		}
		b.WriteString(" STEP ")
		b.WriteString(s)
		args = append(args, a...)
	}
	return b.String(), args, nil
}

// ArrayJoinKind controls whether an ARRAY JOIN is inner (default) or LEFT.
type ArrayJoinKind int

const (
	ArrayJoinInner ArrayJoinKind = iota
	ArrayJoinLeft
)

// ArrayJoinClause represents ARRAY JOIN / LEFT ARRAY JOIN expressions.
type ArrayJoinClause struct {
	Kind  ArrayJoinKind
	Exprs []Sqlizer
}

// LimitByClause represents LIMIT n BY col1, col2, ...
type LimitByClause struct {
	N    uint64
	Cols []string
}

// SampleClause represents SAMPLE ratio or SAMPLE n OFFSET m.
type SampleClause struct {
	Ratio  float64 // e.g. 0.1 for SAMPLE 0.1
	N      *uint64 // absolute row count alternative; mutually exclusive with Ratio
	Offset *uint64 // optional OFFSET (absolute rows only)
}

// Setting is a single SETTINGS key=value pair.
type Setting struct {
	Key string
	Val any
}

// SelectBuilder builds a ClickHouse SELECT statement.
// All mutating methods return a new copy, making the builder immutable /
// safe for reuse as a sub-query base.
type SelectBuilder struct {
	columns    []Sqlizer
	from       string
	fromAlias  string
	subquery   *SelectBuilder // FROM (subquery) AS alias
	final      bool
	sample     *SampleClause
	arrayJoins []ArrayJoinClause
	preWhere   []Sqlizer
	where      []Sqlizer
	groupBy    []string
	withTotals bool
	having     []Sqlizer
	orderBy    []string         // plain ORDER BY columns (no fill)
	withFill   []WithFillClause // columns with WITH FILL
	limit      *uint64
	limitBy    *LimitByClause
	offset     *uint64
	settings   []Setting
}

// Select starts a new SelectBuilder with the given column expressions.
func Select(columns ...Sqlizer) SelectBuilder {
	return SelectBuilder{
		columns:    columns,
		from:       "",
		fromAlias:  "",
		subquery:   nil,
		final:      false,
		sample:     nil,
		arrayJoins: nil,
		preWhere:   nil,
		where:      nil,
		groupBy:    nil,
		withTotals: false,
		having:     nil,
		orderBy:    nil,
		withFill:   nil,
		limit:      nil,
		limitBy:    nil,
		offset:     nil,
		settings:   nil,
	}
}

// SelectRaw is a convenience for Select(Expr(col1), Expr(col2), ...).
func SelectRaw(columns ...string) SelectBuilder {
	exprs := make([]Sqlizer, len(columns))
	for i, c := range columns {
		exprs[i] = rawExpr{sql: c, args: nil}
	}
	return SelectBuilder{
		columns:    exprs,
		from:       "",
		fromAlias:  "",
		subquery:   nil,
		final:      false,
		sample:     nil,
		arrayJoins: nil,
		preWhere:   nil,
		where:      nil,
		groupBy:    nil,
		withTotals: false,
		having:     nil,
		orderBy:    nil,
		withFill:   nil,
		limit:      nil,
		limitBy:    nil,
		offset:     nil,
		settings:   nil,
	}
}

// copy produces a shallow copy so mutations don't affect the original.
func (sb SelectBuilder) copy() SelectBuilder {
	c := sb
	c.columns = append([]Sqlizer(nil), sb.columns...)
	c.arrayJoins = append([]ArrayJoinClause(nil), sb.arrayJoins...)
	c.preWhere = append([]Sqlizer(nil), sb.preWhere...)
	c.where = append([]Sqlizer(nil), sb.where...)
	c.groupBy = append([]string(nil), sb.groupBy...)
	c.having = append([]Sqlizer(nil), sb.having...)
	c.orderBy = append([]string(nil), sb.orderBy...)
	c.withFill = append([]WithFillClause(nil), sb.withFill...)
	c.settings = append([]Setting(nil), sb.settings...)
	return c
}

// Column appends additional SELECT expressions.
func (sb SelectBuilder) Column(cols ...Sqlizer) SelectBuilder {
	c := sb.copy()
	c.columns = append(c.columns, cols...)
	return c
}

// ColumnRaw appends raw SQL column expressions.
func (sb SelectBuilder) ColumnRaw(cols ...string) SelectBuilder {
	exprs := make([]Sqlizer, len(cols))
	for i, col := range cols {
		exprs[i] = rawExpr{sql: col, args: nil}
	}
	return sb.Column(exprs...)
}

// From sets the table name (and optional alias).
//
//	sb.From("telemetry_logs")
//	sb.From("telemetry_logs", "tl")
func (sb SelectBuilder) From(table string, alias ...string) SelectBuilder {
	c := sb.copy()
	c.from = table
	if len(alias) > 0 {
		c.fromAlias = alias[0]
	}
	return c
}

// FromSubquery uses a SelectBuilder as the FROM clause with a mandatory alias.
//
//	chq.Select(chq.Expr("*")).FromSubquery(inner, "sub")
func (sb SelectBuilder) FromSubquery(sub SelectBuilder, alias string) SelectBuilder {
	c := sb.copy()
	c.subquery = &sub
	c.fromAlias = alias
	c.from = ""
	return c
}

// Final appends the FINAL modifier to the FROM clause.
// Used with ReplacingMergeTree / CollapsingMergeTree to force deduplication.
func (sb SelectBuilder) Final() SelectBuilder {
	c := sb.copy()
	c.final = true
	return c
}

// Sample sets the SAMPLE clause.
//
//	sb.Sample(SampleClause{Ratio: 0.1})
//	sb.Sample(SampleClause{N: ptr(uint64(1000)), Offset: ptr(uint64(500))})
func (sb SelectBuilder) Sample(s SampleClause) SelectBuilder {
	c := sb.copy()
	c.sample = &s
	return c
}

// ArrayJoin appends an ARRAY JOIN clause.
func (sb SelectBuilder) ArrayJoin(exprs ...Sqlizer) SelectBuilder {
	c := sb.copy()
	c.arrayJoins = append(c.arrayJoins, ArrayJoinClause{Kind: ArrayJoinInner, Exprs: exprs})
	return c
}

// LeftArrayJoin appends a LEFT ARRAY JOIN clause.
func (sb SelectBuilder) LeftArrayJoin(exprs ...Sqlizer) SelectBuilder {
	c := sb.copy()
	c.arrayJoins = append(c.arrayJoins, ArrayJoinClause{Kind: ArrayJoinLeft, Exprs: exprs})
	return c
}

// PreWhere appends conditions to the PREWHERE clause (AND-combined).
// PREWHERE is applied before reading column data — a major ClickHouse
// performance lever when filtering on a highly selective indexed column.
func (sb SelectBuilder) PreWhere(preds ...Sqlizer) SelectBuilder {
	c := sb.copy()
	c.preWhere = append(c.preWhere, preds...)
	return c
}

// Where appends conditions to the WHERE clause (AND-combined).
func (sb SelectBuilder) Where(preds ...Sqlizer) SelectBuilder {
	c := sb.copy()
	c.where = append(c.where, preds...)
	return c
}

// GroupBy appends GROUP BY columns.
func (sb SelectBuilder) GroupBy(cols ...string) SelectBuilder {
	c := sb.copy()
	c.groupBy = append(c.groupBy, cols...)
	return c
}

// WithTotals adds WITH TOTALS to the GROUP BY clause.
func (sb SelectBuilder) WithTotals() SelectBuilder {
	c := sb.copy()
	c.withTotals = true
	return c
}

// Having appends HAVING conditions (AND-combined).
func (sb SelectBuilder) Having(preds ...Sqlizer) SelectBuilder {
	c := sb.copy()
	c.having = append(c.having, preds...)
	return c
}

// OrderBy appends plain ORDER BY expressions (raw strings, no WITH FILL).
func (sb SelectBuilder) OrderBy(cols ...string) SelectBuilder {
	c := sb.copy()
	c.orderBy = append(c.orderBy, cols...)
	return c
}

// OrderByWithFill appends a WITH FILL ORDER BY clause.
// Multiple calls accumulate fill columns; a query may mix plain and fill columns.
func (sb SelectBuilder) OrderByWithFill(clause WithFillClause) SelectBuilder {
	c := sb.copy()
	c.withFill = append(c.withFill, clause)
	return c
}

// Limit sets LIMIT n.
func (sb SelectBuilder) Limit(n uint64) SelectBuilder {
	c := sb.copy()
	c.limit = &n
	return c
}

// Offset sets OFFSET n.
func (sb SelectBuilder) Offset(n uint64) SelectBuilder {
	c := sb.copy()
	c.offset = &n
	return c
}

// LimitBy sets LIMIT n BY col1, col2, ...
// Evaluated before LIMIT — truncates each group identified by the BY columns.
func (sb SelectBuilder) LimitBy(n uint64, cols ...string) SelectBuilder {
	c := sb.copy()
	c.limitBy = &LimitByClause{N: n, Cols: cols}
	return c
}

// Settings appends SETTINGS key=value pairs.
func (sb SelectBuilder) Settings(kvs ...Setting) SelectBuilder {
	c := sb.copy()
	c.settings = append(c.settings, kvs...)
	return c
}

// ToSql renders the builder into a SQL string and argument slice.
func (sb SelectBuilder) ToSql() (string, []any, error) {
	if len(sb.columns) == 0 {
		return "", nil, fmt.Errorf("chq: SELECT requires at least one column")
	}

	var b strings.Builder
	var args []any

	// SELECT
	b.WriteString("SELECT ")
	colSqls := make([]string, 0, len(sb.columns))
	for _, col := range sb.columns {
		s, a, err := col.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: column: %w", err)
		}
		colSqls = append(colSqls, s)
		args = append(args, a...)
	}
	b.WriteString(strings.Join(colSqls, ", "))

	// FROM
	if sb.subquery != nil {
		subSQL, subArgs, err := sb.subquery.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: subquery: %w", err)
		}
		b.WriteString(" FROM (")
		b.WriteString(subSQL)
		b.WriteString(") AS ")
		b.WriteString(sb.fromAlias)
		args = append(args, subArgs...)
	} else if sb.from != "" {
		b.WriteString(" FROM ")
		b.WriteString(sb.from)
		if sb.final {
			b.WriteString(" FINAL")
		}
		if sb.fromAlias != "" {
			b.WriteString(" AS ")
			b.WriteString(sb.fromAlias)
		}
	}

	// SAMPLE
	if sb.sample != nil {
		s, sArgs, err := renderSample(sb.sample)
		if err != nil {
			return "", nil, err
		}
		b.WriteString(s)
		args = append(args, sArgs...)
	}

	// ARRAY JOIN
	for _, aj := range sb.arrayJoins {
		if aj.Kind == ArrayJoinLeft {
			b.WriteString(" LEFT ARRAY JOIN ")
		} else {
			b.WriteString(" ARRAY JOIN ")
		}
		exprSqls := make([]string, 0, len(aj.Exprs))
		for _, e := range aj.Exprs {
			s, a, err := e.ToSql()
			if err != nil {
				return "", nil, fmt.Errorf("chq: ARRAY JOIN expr: %w", err)
			}
			exprSqls = append(exprSqls, s)
			args = append(args, a...)
		}
		b.WriteString(strings.Join(exprSqls, ", "))
	}

	// PREWHERE
	if len(sb.preWhere) > 0 {
		s, a, err := And(sb.preWhere).ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: PREWHERE: %w", err)
		}
		b.WriteString(" PREWHERE ")
		b.WriteString(s)
		args = append(args, a...)
	}

	// WHERE
	if len(sb.where) > 0 {
		s, a, err := And(sb.where).ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: WHERE: %w", err)
		}
		b.WriteString(" WHERE ")
		b.WriteString(s)
		args = append(args, a...)
	}

	// GROUP BY
	if len(sb.groupBy) > 0 {
		b.WriteString(" GROUP BY ")
		b.WriteString(strings.Join(sb.groupBy, ", "))
		if sb.withTotals {
			b.WriteString(" WITH TOTALS")
		}
	}

	// HAVING
	if len(sb.having) > 0 {
		s, a, err := And(sb.having).ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: HAVING: %w", err)
		}
		b.WriteString(" HAVING ")
		b.WriteString(s)
		args = append(args, a...)
	}

	// ORDER BY (plain + WITH FILL)
	if len(sb.orderBy) > 0 || len(sb.withFill) > 0 {
		b.WriteString(" ORDER BY ")
		parts := make([]string, 0, len(sb.orderBy)+len(sb.withFill))
		parts = append(parts, sb.orderBy...)
		for _, wf := range sb.withFill {
			s, a, err := wf.render()
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, s)
			args = append(args, a...)
		}
		b.WriteString(strings.Join(parts, ", "))
	}

	// LIMIT BY
	if sb.limitBy != nil {
		fmt.Fprintf(&b, " LIMIT %d BY %s", sb.limitBy.N, strings.Join(sb.limitBy.Cols, ", "))
	}

	// LIMIT / OFFSET
	if sb.limit != nil {
		fmt.Fprintf(&b, " LIMIT %d", *sb.limit)
	}
	if sb.offset != nil {
		fmt.Fprintf(&b, " OFFSET %d", *sb.offset)
	}

	// SETTINGS
	if len(sb.settings) > 0 {
		b.WriteString(" SETTINGS ")
		settingParts := make([]string, len(sb.settings))
		for i, s := range sb.settings {
			settingParts[i] = fmt.Sprintf("%s = %v", s.Key, s.Val)
		}
		b.WriteString(strings.Join(settingParts, ", "))
	}

	return b.String(), args, nil
}

func renderSample(s *SampleClause) (string, []any, error) {
	if s.N != nil {
		if s.Offset != nil {
			return fmt.Sprintf(" SAMPLE %d OFFSET %d", *s.N, *s.Offset), nil, nil
		}
		return fmt.Sprintf(" SAMPLE %d", *s.N), nil, nil
	}
	return fmt.Sprintf(" SAMPLE %g", s.Ratio), nil, nil
}
