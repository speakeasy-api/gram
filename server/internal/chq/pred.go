package chq

import (
	"fmt"
	"strings"
)

// ----------------------------------------------------------------------------
// Predicate helpers — combinators and comparison types
// ----------------------------------------------------------------------------

// And combines multiple Sqlizers with AND.
type And []Sqlizer

func (a And) ToSql() (string, []any, error) {
	if len(a) == 0 {
		return "1=1", nil, nil
	}
	parts := make([]string, 0, len(a))
	var args []any
	for _, p := range a {
		s, a2, err := p.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("AND predicate: %w", err)
		}
		if s == "" {
			continue
		}
		parts = append(parts, s)
		args = append(args, a2...)
	}
	switch len(parts) {
	case 0:
		return "1=1", nil, nil
	case 1:
		return parts[0], args, nil
	default:
		return "(" + strings.Join(parts, " AND ") + ")", args, nil
	}
}

// Or combines multiple Sqlizers with OR.
type Or []Sqlizer

func (o Or) ToSql() (string, []any, error) {
	if len(o) == 0 {
		return "1=0", nil, nil
	}
	parts := make([]string, 0, len(o))
	var args []any
	for _, p := range o {
		s, a, err := p.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("OR predicate: %w", err)
		}
		if s == "" {
			continue
		}
		parts = append(parts, s)
		args = append(args, a...)
	}
	switch len(parts) {
	case 0:
		return "1=0", nil, nil
	case 1:
		return parts[0], args, nil
	default:
		return "(" + strings.Join(parts, " OR ") + ")", args, nil
	}
}

// Not negates a Sqlizer.
type Not struct{ Pred Sqlizer }

func (n Not) ToSql() (string, []any, error) {
	s, args, err := n.Pred.ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("NOT predicate: %w", err)
	}
	return "NOT (" + s + ")", args, nil
}

// ----------------------------------------------------------------------------
// Eq / NotEq / Lt / Lte / Gt / Gte
// ----------------------------------------------------------------------------

// Eq generates col = ? or col IN (?,?,?) when the value is a slice.
// Pass a nil value to generate col IS NULL.
type Eq[T comparable] struct {
	Col string
	Val T
}

func (e Eq[T]) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s = ?", e.Col), []any{e.Val}, nil
}

// EqNullable generates col = ? or col IS NULL.
type EqNullable[T any] struct {
	Col string
	Val *T
}

func (e EqNullable[T]) ToSql() (string, []any, error) {
	if e.Val == nil {
		return e.Col + " IS NULL", nil, nil
	}
	return fmt.Sprintf("%s = ?", e.Col), []any{*e.Val}, nil
}

// NotEq generates col != ? or col IS NOT NULL.
type NotEq[T comparable] struct {
	Col string
	Val T
}

func (n NotEq[T]) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s != ?", n.Col), []any{n.Val}, nil
}

// Lt / Lte / Gt / Gte — ordered comparison predicates.

type Lt[T any] struct {
	Col string
	Val T
}
type Lte[T any] struct {
	Col string
	Val T
}
type Gt[T any] struct {
	Col string
	Val T
}
type Gte[T any] struct {
	Col string
	Val T
}

func (p Lt[T]) ToSql() (string, []any, error) { return fmt.Sprintf("%s < ?", p.Col), []any{p.Val}, nil }
func (p Lte[T]) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s <= ?", p.Col), []any{p.Val}, nil
}
func (p Gt[T]) ToSql() (string, []any, error) { return fmt.Sprintf("%s > ?", p.Col), []any{p.Val}, nil }
func (p Gte[T]) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s >= ?", p.Col), []any{p.Val}, nil
}

// ----------------------------------------------------------------------------
// In / NotIn
// ----------------------------------------------------------------------------

// In generates col IN (?,?,?).  An empty slice generates 1=0 (always false).
type In[T any] struct {
	Col  string
	Vals []T
}

func (i In[T]) ToSql() (string, []any, error) {
	if len(i.Vals) == 0 {
		return "1=0", nil, nil
	}
	placeholders := strings.Repeat(",?", len(i.Vals))[1:]
	args := make([]any, len(i.Vals))
	for idx, v := range i.Vals {
		args[idx] = v
	}
	return fmt.Sprintf("%s IN (%s)", i.Col, placeholders), args, nil
}

// NotIn generates col NOT IN (?,?,?).  An empty slice generates 1=1 (always true).
type NotIn[T any] struct {
	Col  string
	Vals []T
}

func (n NotIn[T]) ToSql() (string, []any, error) {
	if len(n.Vals) == 0 {
		return "1=1", nil, nil
	}
	placeholders := strings.Repeat(",?", len(n.Vals))[1:]
	args := make([]any, len(n.Vals))
	for idx, v := range n.Vals {
		args[idx] = v
	}
	return fmt.Sprintf("%s NOT IN (%s)", n.Col, placeholders), args, nil
}

// ----------------------------------------------------------------------------
// Between
// ----------------------------------------------------------------------------

// Between generates col BETWEEN ? AND ?.
type Between[T any] struct {
	Col  string
	Low  T
	High T
}

func (b Between[T]) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s BETWEEN ? AND ?", b.Col), []any{b.Low, b.High}, nil
}

// ----------------------------------------------------------------------------
// LikePred / ILikePred
// Named with the Pred suffix to avoid collision with the chq.Like() function helper.
// ----------------------------------------------------------------------------

type LikePred struct{ Col, Pattern string }
type ILikePred struct{ Col, Pattern string }

func (l LikePred) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s LIKE ?", l.Col), []any{l.Pattern}, nil
}
func (l ILikePred) ToSql() (string, []any, error) {
	return fmt.Sprintf("%s ILIKE ?", l.Col), []any{l.Pattern}, nil
}

// ----------------------------------------------------------------------------
// TupleGt / TupleGte — ClickHouse tuple comparison for cursor pagination
// (a, b) > (x, y)
// ----------------------------------------------------------------------------

// TupleGt generates (col1, col2, ...) > (?, ?, ...).
// ClickHouse supports tuple comparison as a single operator — invaluable for
// stable multi-column cursor pagination without an OR explosion.
type TupleGt struct {
	Cols []string
	Vals []any
}

func (t TupleGt) ToSql() (string, []any, error) {
	if len(t.Cols) != len(t.Vals) {
		return "", nil, fmt.Errorf("TupleGt: len(Cols)=%d != len(Vals)=%d", len(t.Cols), len(t.Vals))
	}
	placeholders := strings.Repeat(",?", len(t.Vals))[1:]
	return fmt.Sprintf("(%s) > (%s)", strings.Join(t.Cols, ", "), placeholders), t.Vals, nil
}

// TupleGte generates (col1, col2, ...) >= (?, ?, ...).
type TupleGte struct {
	Cols []string
	Vals []any
}

func (t TupleGte) ToSql() (string, []any, error) {
	if len(t.Cols) != len(t.Vals) {
		return "", nil, fmt.Errorf("TupleGte: len(Cols)=%d != len(Vals)=%d", len(t.Cols), len(t.Vals))
	}
	placeholders := strings.Repeat(",?", len(t.Vals))[1:]
	return fmt.Sprintf("(%s) >= (%s)", strings.Join(t.Cols, ", "), placeholders), t.Vals, nil
}

// TupleLt generates (col1, col2, ...) < (?, ?, ...).
type TupleLt struct {
	Cols []string
	Vals []any
}

func (t TupleLt) ToSql() (string, []any, error) {
	if len(t.Cols) != len(t.Vals) {
		return "", nil, fmt.Errorf("TupleLt: len(Cols)=%d != len(Vals)=%d", len(t.Cols), len(t.Vals))
	}
	placeholders := strings.Repeat(",?", len(t.Vals))[1:]
	return fmt.Sprintf("(%s) < (%s)", strings.Join(t.Cols, ", "), placeholders), t.Vals, nil
}
