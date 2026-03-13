package chq

import (
	"fmt"
	"strings"
)

// ----------------------------------------------------------------------------
// InsertBuilder
// ----------------------------------------------------------------------------

// InsertBuilder builds a ClickHouse INSERT INTO statement.
type InsertBuilder struct {
	table    string
	columns  []string
	rows     [][]any
	settings []Setting
}

// Insert starts a new InsertBuilder targeting table.
func Insert(table string) InsertBuilder {
	return InsertBuilder{table: table, columns: nil, rows: nil, settings: nil}
}

func (ib InsertBuilder) copy() InsertBuilder {
	c := ib
	c.columns = append([]string(nil), ib.columns...)
	c.rows = make([][]any, len(ib.rows))
	for i, r := range ib.rows {
		c.rows[i] = append([]any(nil), r...)
	}
	c.settings = append([]Setting(nil), ib.settings...)
	return c
}

// Columns sets the column list for the INSERT.
func (ib InsertBuilder) Columns(cols ...string) InsertBuilder {
	c := ib.copy()
	c.columns = cols
	return c
}

// Values appends a row of values.  The number of values must match Columns.
func (ib InsertBuilder) Values(vals ...any) InsertBuilder {
	c := ib.copy()
	c.rows = append(c.rows, vals)
	return c
}

// Settings appends SETTINGS key=value pairs to the INSERT.
func (ib InsertBuilder) Settings(kvs ...Setting) InsertBuilder {
	c := ib.copy()
	c.settings = append(c.settings, kvs...)
	return c
}

// ToSql renders the INSERT statement.
func (ib InsertBuilder) ToSql() (string, []any, error) {
	if ib.table == "" {
		return "", nil, fmt.Errorf("chq: INSERT requires a table name")
	}
	if len(ib.columns) == 0 {
		return "", nil, fmt.Errorf("chq: INSERT requires at least one column")
	}
	if len(ib.rows) == 0 {
		return "", nil, fmt.Errorf("chq: INSERT requires at least one row of values")
	}
	for i, row := range ib.rows {
		if len(row) != len(ib.columns) {
			return "", nil, fmt.Errorf("chq: INSERT row %d has %d values but %d columns declared", i, len(row), len(ib.columns))
		}
	}

	var b strings.Builder
	var args []any

	b.WriteString("INSERT INTO ")
	b.WriteString(ib.table)
	b.WriteString(" (")
	b.WriteString(strings.Join(ib.columns, ", "))
	b.WriteString(") VALUES ")

	placeholder := "(" + strings.Repeat(",?", len(ib.columns))[1:] + ")"
	rowParts := make([]string, len(ib.rows))
	for i, row := range ib.rows {
		rowParts[i] = placeholder
		args = append(args, row...)
	}
	b.WriteString(strings.Join(rowParts, ", "))

	if len(ib.settings) > 0 {
		b.WriteString(" SETTINGS ")
		settingParts := make([]string, len(ib.settings))
		for i, s := range ib.settings {
			settingParts[i] = fmt.Sprintf("%s = %v", s.Key, s.Val)
		}
		b.WriteString(strings.Join(settingParts, ", "))
	}

	return b.String(), args, nil
}
