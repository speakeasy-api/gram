// Package chq is a ClickHouse query builder with first-class support for
// ClickHouse-specific SQL syntax. It uses ? placeholders (ClickHouse native)
// and provides typed helpers for ClickHouse functions, clauses, and aggregates.
package chq

import (
	"fmt"
	"strings"
)

// Sqlizer is the interface implemented by anything that can produce a SQL
// fragment with associated positional ? arguments.
type Sqlizer interface {
	ToSql() (string, []any, error)
}

// rawExpr is a pre-formed SQL fragment with args — the escape hatch for
// anything the builder doesn't natively model.
type rawExpr struct {
	sql  string
	args []any
}

// Expr creates a raw SQL fragment with positional ? args.
// Use sparingly; prefer typed builder methods.
//
//	chq.Expr("toUUID(?)", id)
func Expr(sql string, args ...any) Sqlizer {
	return rawExpr{sql: sql, args: args}
}

func (e rawExpr) ToSql() (string, []any, error) {
	return e.sql, e.args, nil
}

// Col is a simple unquoted column reference.  Use it where a Sqlizer is
// required but you only have a column name.
func Col(name string) Sqlizer {
	return rawExpr{sql: name, args: nil}
}

// concatSqlizers renders a slice of Sqlizers joined by sep.
func concatSqlizers(parts []Sqlizer, sep string) (string, []any, error) {
	sqls := make([]string, 0, len(parts))
	var args []any
	for _, p := range parts {
		s, a, err := p.ToSql()
		if err != nil {
			return "", nil, fmt.Errorf("rendering sqlizer: %w", err)
		}
		sqls = append(sqls, s)
		args = append(args, a...)
	}
	return strings.Join(sqls, sep), args, nil
}
