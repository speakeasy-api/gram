package chq

import (
	"fmt"
	"strings"
)

// ----------------------------------------------------------------------------
// ClickHouse function helpers
//
// These return Sqlizer values so they compose naturally with Select/Where/etc.
// All helpers that accept runtime values take them as Sqlizer so callers can
// choose between Expr("?", val) for a bound parameter and a raw column name.
// ----------------------------------------------------------------------------

// call is a generic N-ary function application.
func call(fn string, args ...Sqlizer) Sqlizer {
	return fnExpr{fn: fn, args: args}
}

type fnExpr struct {
	fn   string
	args []Sqlizer
}

func (f fnExpr) ToSql() (string, []any, error) {
	s, a, err := concatSqlizers(f.args, ", ")
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", f.fn, err)
	}
	return f.fn + "(" + s + ")", a, nil
}

// ----------------------------------------------------------------------------
// Type coercions
// ----------------------------------------------------------------------------

func ToUUID(expr Sqlizer) Sqlizer          { return call("toUUID", expr) }
func ToUUIDOrNull(expr Sqlizer) Sqlizer    { return call("toUUIDOrNull", expr) }
func ToString(expr Sqlizer) Sqlizer        { return call("toString", expr) }
func ToInt32(expr Sqlizer) Sqlizer         { return call("toInt32", expr) }
func ToInt32OrZero(expr Sqlizer) Sqlizer   { return call("toInt32OrZero", expr) }
func ToInt64(expr Sqlizer) Sqlizer         { return call("toInt64", expr) }
func ToInt64OrZero(expr Sqlizer) Sqlizer   { return call("toInt64OrZero", expr) }
func ToFloat64(expr Sqlizer) Sqlizer       { return call("toFloat64", expr) }
func ToFloat64OrZero(expr Sqlizer) Sqlizer { return call("toFloat64OrZero", expr) }
func ToFloat32(expr Sqlizer) Sqlizer       { return call("toFloat32", expr) }
func ToFloat32OrZero(expr Sqlizer) Sqlizer { return call("toFloat32OrZero", expr) }
func ToDate(expr Sqlizer) Sqlizer          { return call("toDate", expr) }
func ToDateTime(expr Sqlizer) Sqlizer      { return call("toDateTime", expr) }
func ToDateTime64(expr Sqlizer, precision int) Sqlizer {
	return call("toDateTime64", expr, Expr(fmt.Sprintf("%d", precision)))
}

// ----------------------------------------------------------------------------
// Timestamp helpers
// ----------------------------------------------------------------------------

// FromUnixTimestamp returns toDateTime(expr).
func FromUnixTimestamp(expr Sqlizer) Sqlizer { return call("toDateTime", expr) }

// FromUnixTimestamp64Milli/Micro/Nano convert integer timestamps to DateTime64.
func FromUnixTimestamp64Milli(expr Sqlizer) Sqlizer {
	return call("fromUnixTimestamp64Milli", expr)
}
func FromUnixTimestamp64Micro(expr Sqlizer) Sqlizer {
	return call("fromUnixTimestamp64Micro", expr)
}
func FromUnixTimestamp64Nano(expr Sqlizer) Sqlizer {
	return call("fromUnixTimestamp64Nano", expr)
}

// ToIntervalSecond/Minute/Hour/Day/Week/Month/Year — interval constructors.
func ToIntervalSecond(expr Sqlizer) Sqlizer { return call("toIntervalSecond", expr) }
func ToIntervalMinute(expr Sqlizer) Sqlizer { return call("toIntervalMinute", expr) }
func ToIntervalHour(expr Sqlizer) Sqlizer   { return call("toIntervalHour", expr) }
func ToIntervalDay(expr Sqlizer) Sqlizer    { return call("toIntervalDay", expr) }
func ToIntervalWeek(expr Sqlizer) Sqlizer   { return call("toIntervalWeek", expr) }
func ToIntervalMonth(expr Sqlizer) Sqlizer  { return call("toIntervalMonth", expr) }
func ToIntervalYear(expr Sqlizer) Sqlizer   { return call("toIntervalYear", expr) }

// ToStartOfInterval buckets a datetime by the given interval.
//
//	chq.ToStartOfInterval(chq.Col("ts"), chq.ToIntervalSecond(chq.Param(60)))
func ToStartOfInterval(datetime, interval Sqlizer) Sqlizer {
	return call("toStartOfInterval", datetime, interval)
}

func ToStartOfHour(expr Sqlizer) Sqlizer   { return call("toStartOfHour", expr) }
func ToStartOfDay(expr Sqlizer) Sqlizer    { return call("toStartOfDay", expr) }
func ToStartOfWeek(expr Sqlizer) Sqlizer   { return call("toStartOfWeek", expr) }
func ToStartOfMonth(expr Sqlizer) Sqlizer  { return call("toStartOfMonth", expr) }
func ToStartOfMinute(expr Sqlizer) Sqlizer { return call("toStartOfMinute", expr) }
func ToYYYYMMDD(expr Sqlizer) Sqlizer      { return call("toYYYYMMDD", expr) }
func ToYYYYMM(expr Sqlizer) Sqlizer        { return call("toYYYYMM", expr) }

// Now / Today / Yesterday — zero-arg time functions.
func Now() Sqlizer       { return rawExpr{sql: "now()", args: nil} }
func Today() Sqlizer     { return rawExpr{sql: "today()", args: nil} }
func Yesterday() Sqlizer { return rawExpr{sql: "yesterday()", args: nil} }

// ----------------------------------------------------------------------------
// Conditional / branching
// ----------------------------------------------------------------------------

// If renders IF(cond, then, else).
func If(cond, then, els Sqlizer) Sqlizer {
	return call("if", cond, then, els)
}

// IfNull renders ifNull(expr, default).
func IfNull(expr, def Sqlizer) Sqlizer { return call("ifNull", expr, def) }

// NullIf renders nullIf(a, b).
func NullIf(a, b Sqlizer) Sqlizer { return call("nullIf", a, b) }

// Coalesce renders coalesce(a, b, ...).
func Coalesce(exprs ...Sqlizer) Sqlizer { return call("coalesce", exprs...) }

// Greatest / Least
func Greatest(a, b Sqlizer) Sqlizer { return call("greatest", a, b) }
func Least(a, b Sqlizer) Sqlizer    { return call("least", a, b) }

// ----------------------------------------------------------------------------
// String functions
// ----------------------------------------------------------------------------

func StartsWith(str, prefix Sqlizer) Sqlizer    { return call("startsWith", str, prefix) }
func EndsWith(str, suffix Sqlizer) Sqlizer      { return call("endsWith", str, suffix) }
func Position(haystack, needle Sqlizer) Sqlizer { return call("position", haystack, needle) }
func Like(str, pattern Sqlizer) Sqlizer         { return call("like", str, pattern) }
func Lower(expr Sqlizer) Sqlizer                { return call("lower", expr) }
func Upper(expr Sqlizer) Sqlizer                { return call("upper", expr) }
func Trim(expr Sqlizer) Sqlizer                 { return call("trim", expr) }
func Concat(exprs ...Sqlizer) Sqlizer           { return call("concat", exprs...) }
func SubstringIndex(str, delim Sqlizer, count int) Sqlizer {
	return call("substringIndex", str, delim, Expr(fmt.Sprintf("%d", count)))
}
func Extract(part string, expr Sqlizer) Sqlizer {
	return Expr(fmt.Sprintf("extract(%s, ?)", part), mustSql(expr))
}

// ----------------------------------------------------------------------------
// Array functions
// ----------------------------------------------------------------------------

// Has renders has(arr, elem) — tests if array contains element.
func Has(arr, elem Sqlizer) Sqlizer           { return call("has", arr, elem) }
func HasAll(arr, elems Sqlizer) Sqlizer       { return call("hasAll", arr, elems) }
func HasAny(arr, elems Sqlizer) Sqlizer       { return call("hasAny", arr, elems) }
func ArrayJoinFn(arr Sqlizer) Sqlizer         { return call("arrayJoin", arr) }
func ArrayLength(arr Sqlizer) Sqlizer         { return call("length", arr) }
func ArrayDistinct(arr Sqlizer) Sqlizer       { return call("arrayDistinct", arr) }
func ArrayFilter(lambda, arr Sqlizer) Sqlizer { return call("arrayFilter", lambda, arr) }
func ArrayMap(lambda, arr Sqlizer) Sqlizer    { return call("arrayMap", lambda, arr) }

// ----------------------------------------------------------------------------
// JSON / dynamic column helpers
// ----------------------------------------------------------------------------

// JSONAllPaths returns all paths in a JSON column.
func JSONAllPaths(expr Sqlizer) Sqlizer { return call("JSONAllPaths", expr) }

// JSONExtractString/Int/Float/Bool — typed JSON extraction.
func JSONExtractString(json, path Sqlizer) Sqlizer {
	return call("JSONExtractString", json, path)
}
func JSONExtractInt(json, path Sqlizer) Sqlizer {
	return call("JSONExtractInt", json, path)
}
func JSONExtractFloat(json, path Sqlizer) Sqlizer {
	return call("JSONExtractFloat", json, path)
}
func JSONExtractBool(json, path Sqlizer) Sqlizer {
	return call("JSONExtractBool", json, path)
}

// ----------------------------------------------------------------------------
// Aggregate functions
// ----------------------------------------------------------------------------

func Count() Sqlizer                     { return rawExpr{sql: "count()", args: nil} }
func CountCol(expr Sqlizer) Sqlizer      { return call("count", expr) }
func CountDistinct(expr Sqlizer) Sqlizer { return call("countDistinct", expr) }
func Sum(expr Sqlizer) Sqlizer           { return call("sum", expr) }
func Avg(expr Sqlizer) Sqlizer           { return call("avg", expr) }
func Min(expr Sqlizer) Sqlizer           { return call("min", expr) }
func Max(expr Sqlizer) Sqlizer           { return call("max", expr) }
func Any(expr Sqlizer) Sqlizer           { return call("any", expr) }
func AnyLast(expr Sqlizer) Sqlizer       { return call("anyLast", expr) }
func Median(expr Sqlizer) Sqlizer        { return call("median", expr) }
func Quantile(level float64, expr Sqlizer) Sqlizer {
	return Expr(fmt.Sprintf("quantile(%g)(%s)", level, mustSqlNoArgs(expr)), mustSqlArgs(expr)...)
}

// Uniq / UniqExact
func Uniq(exprs ...Sqlizer) Sqlizer      { return call("uniq", exprs...) }
func UniqExact(exprs ...Sqlizer) Sqlizer { return call("uniqExact", exprs...) }

// ----------------------------------------------------------------------------
// Conditional aggregates  (*If variants)
// ----------------------------------------------------------------------------

func CountIf(cond Sqlizer) Sqlizer           { return call("countIf", cond) }
func SumIf(expr, cond Sqlizer) Sqlizer       { return call("sumIf", expr, cond) }
func AvgIf(expr, cond Sqlizer) Sqlizer       { return call("avgIf", expr, cond) }
func MinIf(expr, cond Sqlizer) Sqlizer       { return call("minIf", expr, cond) }
func MaxIf(expr, cond Sqlizer) Sqlizer       { return call("maxIf", expr, cond) }
func AnyIf(expr, cond Sqlizer) Sqlizer       { return call("anyIf", expr, cond) }
func AnyLastIf(expr, cond Sqlizer) Sqlizer   { return call("anyLastIf", expr, cond) }
func UniqExactIf(expr, cond Sqlizer) Sqlizer { return call("uniqExactIf", expr, cond) }

// ----------------------------------------------------------------------------
// AggregatingMergeTree helpers  (*State / *Merge variants)
//
// These are used when writing to / reading from AggregatingMergeTree tables.
// ----------------------------------------------------------------------------

// State returns the aggregate *State variant, e.g. sumState(x).
func State(aggFn string, args ...Sqlizer) Sqlizer {
	return call(aggFn+"State", args...)
}

// Merge returns the aggregate *Merge variant, e.g. sumMerge(state_col).
func Merge(aggFn string, args ...Sqlizer) Sqlizer {
	return call(aggFn+"Merge", args...)
}

// IfState returns the conditional aggregate *IfState variant.
// Pass the base aggregate name without the "If" suffix:
//
//	chq.IfState("sum", val, cond)   → sumIfState(val, cond)
//	chq.IfState("uniqExact", col, cond) → uniqExactIfState(col, cond)
func IfState(aggFn string, expr, cond Sqlizer) Sqlizer {
	return call(aggFn+"IfState", expr, cond)
}

// IfMerge returns the conditional aggregate *IfMerge variant.
// Pass the base aggregate name without the "If" suffix:
//
//	chq.IfMerge("count", stateCol)   → countIfMerge(stateCol)
//	chq.IfMerge("sumIf", stateCol)   → sumIfMerge(stateCol)  // "If" already in name
//
// For convenience, if aggFn already ends in "If" the suffix is not doubled.
func IfMerge(aggFn string, args ...Sqlizer) Sqlizer {
	name := aggFn
	if len(name) < 2 || name[len(name)-2:] != "If" {
		name += "If"
	}
	return call(name+"Merge", args...)
}

// SumMapIf renders sumMapIf(map(k1,v1,...), cond) for map aggregation.
func SumMapIf(kvMap Sqlizer, cond Sqlizer) Sqlizer {
	return call("sumMapIf", kvMap, cond)
}

// SumMapIfMerge renders sumMapIfMerge(state_col).
func SumMapIfMerge(stateCol Sqlizer) Sqlizer {
	return call("sumMapIfMerge", stateCol)
}

// MapFn builds a map(k1, v1, k2, v2, ...) literal.
func MapFn(pairs ...Sqlizer) Sqlizer { return call("map", pairs...) }

// ----------------------------------------------------------------------------
// Math
// ----------------------------------------------------------------------------

func Abs(expr Sqlizer) Sqlizer      { return call("abs", expr) }
func Ceil(expr Sqlizer) Sqlizer     { return call("ceil", expr) }
func Floor(expr Sqlizer) Sqlizer    { return call("floor", expr) }
func Round(expr Sqlizer) Sqlizer    { return call("round", expr) }
func Log(expr Sqlizer) Sqlizer      { return call("log", expr) }
func Sqrt(expr Sqlizer) Sqlizer     { return call("sqrt", expr) }
func Pow(base, exp Sqlizer) Sqlizer { return call("pow", base, exp) }
func Divide(a, b Sqlizer) Sqlizer   { return call("divide", a, b) }
func Multiply(a, b Sqlizer) Sqlizer { return call("multiply", a, b) }
func Modulo(a, b Sqlizer) Sqlizer   { return call("modulo", a, b) }
func IntDiv(a, b Sqlizer) Sqlizer   { return call("intDiv", a, b) }

// GenerateUUIDv7 returns the generateUUIDv7() zero-arg function.
func GenerateUUIDv7() Sqlizer { return rawExpr{sql: "generateUUIDv7()", args: nil} }

// ----------------------------------------------------------------------------
// Param — a convenience wrapper so callers don't have to type Expr("?", v).
// ----------------------------------------------------------------------------

// Param wraps a value as a single ? placeholder.
func Param(v any) Sqlizer { return rawExpr{sql: "?", args: []any{v}} }

// ----------------------------------------------------------------------------
// Alias — wraps any Sqlizer as "expr AS alias".
// ----------------------------------------------------------------------------

// As wraps an expression as "expr AS alias".
func As(expr Sqlizer, alias string) Sqlizer {
	return aliasExpr{inner: expr, alias: alias}
}

type aliasExpr struct {
	inner Sqlizer
	alias string
}

func (a aliasExpr) ToSql() (string, []any, error) {
	s, args, err := a.inner.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("alias %s: %w", a.alias, err)
	}
	return s + " AS " + a.alias, args, nil
}

// ----------------------------------------------------------------------------
// internal helpers
// ----------------------------------------------------------------------------

// mustSql renders a Sqlizer to its SQL string, panicking on error.
// Only used in format strings where error propagation is impossible.
func mustSql(s Sqlizer) any {
	sql, args, err := s.ToSql()
	if err != nil {
		panic(err)
	}
	if len(args) > 0 {
		return sql
	}
	return sql
}

func mustSqlNoArgs(s Sqlizer) string {
	sql, _, err := s.ToSql()
	if err != nil {
		panic(err)
	}
	return sql
}

func mustSqlArgs(s Sqlizer) []any {
	_, args, err := s.ToSql()
	if err != nil {
		panic(err)
	}
	return args
}

// BinaryOp wraps two Sqlizers with an infix operator.
// Useful for arithmetic or comparison expressions not covered by predicates.
func BinaryOp(left Sqlizer, op string, right Sqlizer) Sqlizer {
	return binaryOpExpr{left: left, op: op, right: right}
}

type binaryOpExpr struct {
	left, right Sqlizer
	op          string
}

func (b binaryOpExpr) ToSql() (string, []any, error) {
	ls, la, err := b.left.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("binary op left: %w", err)
	}
	rs, ra, err := b.right.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("binary op right: %w", err)
	}
	return fmt.Sprintf("(%s %s %s)", ls, b.op, rs), append(la, ra...), nil
}

// CaseWhen builds CASE WHEN cond THEN val ... [ELSE default] END.
type CaseExpr struct {
	whens []caseWhen
	els   Sqlizer
}

type caseWhen struct{ cond, val Sqlizer }

func Case() CaseExpr { return CaseExpr{whens: nil, els: nil} }

func (c CaseExpr) When(cond, val Sqlizer) CaseExpr {
	c2 := c
	c2.whens = append(append([]caseWhen(nil), c.whens...), caseWhen{cond, val})
	return c2
}

func (c CaseExpr) Else(val Sqlizer) CaseExpr {
	c2 := c
	c2.els = val
	return c2
}

func (c CaseExpr) ToSql() (string, []any, error) {
	if len(c.whens) == 0 {
		return "", nil, fmt.Errorf("chq: CASE expression requires at least one WHEN clause")
	}
	var b strings.Builder
	var args []any
	b.WriteString("CASE")
	for _, w := range c.whens {
		cs, ca, err := w.cond.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: CASE WHEN condition: %w", err)
		}
		vs, va, err := w.val.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: CASE THEN value: %w", err)
		}
		fmt.Fprintf(&b, " WHEN %s THEN %s", cs, vs)
		args = append(args, ca...)
		args = append(args, va...)
	}
	if c.els != nil {
		es, ea, err := c.els.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("chq: CASE ELSE: %w", err)
		}
		fmt.Fprintf(&b, " ELSE %s", es)
		args = append(args, ea...)
	}
	b.WriteString(" END")
	return b.String(), args, nil
}
