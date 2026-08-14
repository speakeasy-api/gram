package rule

import (
	"slices"
	"strings"

	pganalyze "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/speakeasy-api/gram/sqlclint/schema"
)

// walk visits every message in a parse tree, skipping any subtree fn declines.
//
// It descends through protobuf fields rather than matching on the Node union
// wrapper, which matters for correctness: InsertStmt, UpdateStmt and DeleteStmt
// hold their target table in a typed relation field, not in a Node. A visitor
// that only looked inside Node unions would miss the table every mutation writes
// to, which is the one that matters most here.
//
// fn reports whether to descend into the message it was given.
func walk(m protoreflect.Message, fn func(protoreflect.Message) bool) {
	if !fn(m) {
		return
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsMap() {
			return true
		}
		if fd.IsList() {
			l := v.List()
			for i := range l.Len() {
				walk(l.Get(i).Message(), fn)
			}
			return true
		}
		walk(v.Message(), fn)
		return true
	})
}

// references is what a single query touches.
type references struct {
	// tables are the relation names the query names, excluding its own CTEs.
	tables []string

	// ctes are the names bound by WITH clauses in this query. They are not real
	// tables and carry no tenancy of their own.
	ctes []string
}

// collectReferences finds the tables and CTE names in a parsed query.
func collectReferences(tree *pganalyze.ParseResult) references {
	var refs references
	seenTable, seenCTE := map[string]bool{}, map[string]bool{}

	walk(tree.ProtoReflect(), func(m protoreflect.Message) bool {
		// "FOR UPDATE OF t" names the alias t, which Postgres represents as a
		// RangeVar. Descending would resolve that alias as if it were a table.
		if _, locking := m.Interface().(*pganalyze.LockingClause); locking {
			return false
		}

		switch v := m.Interface().(type) {
		case *pganalyze.RangeVar:
			if n := v.GetRelname(); n != "" && !seenTable[n] {
				seenTable[n] = true
				refs.tables = append(refs.tables, n)
			}
		case *pganalyze.CommonTableExpr:
			if n := v.GetCtename(); n != "" && !seenCTE[n] {
				seenCTE[n] = true
				refs.ctes = append(refs.ctes, n)
			}
		}
		return true
	})

	refs.tables = slices.DeleteFunc(refs.tables, func(t string) bool { return seenCTE[t] })
	slices.Sort(refs.tables)
	slices.Sort(refs.ctes)
	return refs
}

// binds records which tenancy columns a query bounds, and how.
type binds struct {
	// strict holds tenancy columns compared against a non-nullable parameter:
	// @name, $n, or sqlc.arg(). These are the only binds that satisfy the rule.
	strict map[string]bool

	// nullable holds tenancy columns bound only through sqlc.narg(). Tracked
	// separately so the diagnostic can say the bound exists but can be disabled,
	// rather than reporting no bound at all.
	nullable map[string]bool
}

func (b binds) columns() []string {
	out := make([]string, 0, len(b.strict))
	for c := range b.strict {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

func (b binds) any() bool { return len(b.strict) > 0 || len(b.nullable) > 0 }

// collectBinds finds every tenancy column the query bounds to a parameter.
//
// Position is deliberately not considered. A tenancy bound is equally real in a
// WHERE clause, a JOIN ... ON condition, an EXISTS subquery, the WHERE of an
// UPDATE ... FROM, or a CTE, and requiring a particular position would reject
// correct SQL that scopes a child table through its parent.
func collectBinds(tree *pganalyze.ParseResult) binds {
	b := binds{strict: map[string]bool{}, nullable: map[string]bool{}}

	walk(tree.ProtoReflect(), func(m protoreflect.Message) bool {
		switch v := m.Interface().(type) {
		case *pganalyze.A_Expr:
			if !isEqualityOp(v) {
				return true
			}
			// Either operand may be the column, so both orders are checked.
			record(b, tenancyColumn(v.GetLexpr()), paramKind(v.GetRexpr()))
			record(b, tenancyColumn(v.GetRexpr()), paramKind(v.GetLexpr()))
		case *pganalyze.InsertStmt:
			collectInsertBinds(b, v)
		}
		return true
	})

	return b
}

// collectInsertBinds records tenancy columns written by an INSERT.
//
// An INSERT carries its tenancy in the column list paired with the row it
// writes, rather than in a predicate, so it is invisible to the expression scan
// above. Both source forms are handled: a VALUES row, and a SELECT whose target
// list supplies the columns positionally.
func collectInsertBinds(b binds, ins *pganalyze.InsertStmt) {
	sel := ins.GetSelectStmt().GetSelectStmt()
	if sel == nil {
		return
	}

	// source yields the expression feeding column i of the INSERT.
	var source func(i int) *pganalyze.Node
	if lists := sel.GetValuesLists(); len(lists) > 0 {
		values := lists[0].GetList().GetItems()
		source = func(i int) *pganalyze.Node {
			if i >= len(values) {
				return nil
			}
			return values[i]
		}
	} else {
		targets := sel.GetTargetList()
		source = func(i int) *pganalyze.Node {
			if i >= len(targets) {
				return nil
			}
			return targets[i].GetResTarget().GetVal()
		}
	}

	for i, c := range ins.GetCols() {
		name := c.GetResTarget().GetName()
		if !isTenancyColumn(name) {
			continue
		}
		record(b, name, paramKind(source(i)))
	}
}

func record(b binds, column string, kind paramClass) {
	if column == "" {
		return
	}
	switch kind {
	case paramStrict:
		b.strict[column] = true
	case paramNullable:
		b.nullable[column] = true
	case paramNone:
	}
}

// isEqualityOp reports whether an expression bounds a column to a value.
//
// Only equality and set membership count. A range or inequality comparison on a
// tenancy id is not a boundary.
func isEqualityOp(e *pganalyze.A_Expr) bool {
	switch e.GetKind() {
	case pganalyze.A_Expr_Kind_AEXPR_OP,
		pganalyze.A_Expr_Kind_AEXPR_IN,
		pganalyze.A_Expr_Kind_AEXPR_OP_ANY:
	default:
		return false
	}
	return operator(e) == "=" || operator(e) == "IN"
}

func operator(e *pganalyze.A_Expr) string {
	names := e.GetName()
	if len(names) == 0 {
		return ""
	}
	return names[len(names)-1].GetString_().GetSval()
}

// tenancyColumn returns the tenancy column a node refers to, or "".
//
// Only the final field is considered, so "iss.project_id" and a bare
// "project_id" resolve the same way.
func tenancyColumn(n *pganalyze.Node) string {
	fields := n.GetColumnRef().GetFields()
	if len(fields) == 0 {
		return ""
	}
	name := fields[len(fields)-1].GetString_().GetSval()
	if !isTenancyColumn(name) {
		return ""
	}
	return name
}

func isTenancyColumn(name string) bool {
	return name == schema.ColumnOrganizationID || name == schema.ColumnProjectID
}

// paramClass distinguishes a bind that always applies from one that can be
// switched off at runtime.
type paramClass int

const (
	// paramNone means the node is not a query parameter at all.
	paramNone paramClass = iota

	// paramStrict is @name, $n, or sqlc.arg(): always present.
	paramStrict

	// paramNullable is sqlc.narg(): the caller may pass NULL.
	paramNullable
)

// paramKind classifies the value side of a comparison.
func paramKind(n *pganalyze.Node) paramClass {
	if n == nil {
		return paramNone
	}

	// A cast such as @project_id::uuid wraps the parameter.
	if cast := n.GetTypeCast(); cast != nil {
		return paramKind(cast.GetArg())
	}

	if n.GetParamRef() != nil {
		return paramStrict
	}

	// sqlc writes named parameters as @name, which Postgres parses as its unary
	// "@" operator applied to a column reference.
	if e := n.GetAExpr(); e != nil {
		if operator(e) == "@" && e.GetLexpr() == nil {
			return paramStrict
		}
		return paramNone
	}

	if fn := n.GetFuncCall(); fn != nil {
		switch strings.ToLower(funcName(fn)) {
		case "sqlc.arg":
			return paramStrict
		case "sqlc.narg":
			return paramNullable
		}
	}

	return paramNone
}

func funcName(fn *pganalyze.FuncCall) string {
	parts := make([]string, 0, len(fn.GetFuncname()))
	for _, n := range fn.GetFuncname() {
		parts = append(parts, n.GetString_().GetSval())
	}
	return strings.Join(parts, ".")
}
