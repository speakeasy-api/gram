package repo

import (
	sq "github.com/Masterminds/squirrel"
)

// chBuilder is the base statement builder for ClickHouse queries.
// ClickHouse uses ? placeholders (not $1, $2, etc. like PostgreSQL).
var chBuilder = sq.StatementBuilder.PlaceholderFormat(sq.Question)

// chSelect creates a new SELECT builder for ClickHouse queries.
func chSelect(columns ...string) sq.SelectBuilder {
	return chBuilder.Select(columns...)
}

// chInsert creates a new INSERT builder for ClickHouse queries.
func chInsert(table string) sq.InsertBuilder {
	return chBuilder.Insert(table)
}

// OptionalEq adds a WHERE condition only if the value is non-empty.
// This replaces the (? = '' OR field = ?) pattern from raw SQL.
func OptionalEq(sb sq.SelectBuilder, column string, value string) sq.SelectBuilder {
	if value == "" {
		return sb
	}
	return sb.Where(sq.Eq{column: value})
}

// OptionalEqInt32 adds a WHERE condition only if the value is non-zero.
// This replaces the (? = 0 OR field = ?) pattern from raw SQL.
func OptionalEqInt32(sb sq.SelectBuilder, column string, value int32) sq.SelectBuilder {
	if value == 0 {
		return sb
	}
	return sb.Where(sq.Eq{column: value})
}

// OptionalHas adds a has(?, column) condition for ClickHouse array membership.
// This replaces the (length(?) = 0 OR has(?, column)) pattern from raw SQL.
func OptionalHas(sb sq.SelectBuilder, column string, values []string) sq.SelectBuilder {
	if len(values) == 0 {
		return sb
	}
	return sb.Where("has(?, "+column+")", values)
}

// OptionalPosition adds a position() > 0 condition for ClickHouse substring matching.
// This replaces the if(? = '', true, position(column, ?) > 0) pattern from raw SQL.
func OptionalPosition(sb sq.SelectBuilder, column string, value string) sq.SelectBuilder {
	if value == "" {
		return sb
	}
	return sb.Where("position("+column+", ?) > 0", value)
}

// OptionalUUIDEq adds a UUID equality check using ClickHouse's toUUIDOrNull().
// This replaces the (? = '' OR column = toUUIDOrNull(?)) pattern from raw SQL.
func OptionalUUIDEq(sb sq.SelectBuilder, column string, value string) sq.SelectBuilder {
	if value == "" {
		return sb
	}
	return sb.Where(column+" = toUUIDOrNull(?)", value)
}

// OptionalAttrEq adds an equality check for a nested JSON attribute in ClickHouse.
// Column should be in the format: toString(attributes.`attr.name`)
// This replaces the (? = '' OR toString(attributes.`attr`) = ?) pattern.
func OptionalAttrEq(sb sq.SelectBuilder, attrExpr string, value string) sq.SelectBuilder {
	if value == "" {
		return sb
	}
	return sb.Where(attrExpr+" = ?", value)
}

// OptionalAttrEqInt32 adds an equality check for a nested JSON attribute with int value.
// Column should be in the format: toInt32OrZero(toString(attributes.`attr.name`))
// This replaces the (? = 0 OR toInt32OrZero(...) = ?) pattern.
func OptionalAttrEqInt32(sb sq.SelectBuilder, attrExpr string, value int32) sq.SelectBuilder {
	if value == 0 {
		return sb
	}
	return sb.Where(attrExpr+" = ?", value)
}
