package repo

import (
	sq "github.com/Masterminds/squirrel"
)

// withPagination adds cursor-based WHERE conditions for simple (non-aggregated) queries.
// Uses composite tuple comparison: (time_unix_nano, id) > or < (cursor_time, cursor_id).
// This replaces the complex IF() conditional cursor logic from raw SQL.
func withPagination(sb sq.SelectBuilder, cursor, sortOrder string) sq.SelectBuilder {
	if cursor == "" {
		return sb
	}

	if sortOrder == "asc" {
		return sb.Where(
			"(time_unix_nano, toUUID(id)) > (SELECT time_unix_nano, toUUID(id) FROM telemetry_logs WHERE id = toUUID(?) LIMIT 1)",
			cursor,
		)
	}
	return sb.Where(
		"(time_unix_nano, toUUID(id)) < (SELECT time_unix_nano, toUUID(id) FROM telemetry_logs WHERE id = toUUID(?) LIMIT 1)",
		cursor,
	)
}

// withHavingPagination adds cursor-based HAVING conditions for aggregation queries.
// Used when the query has GROUP BY and cursor pagination must use HAVING instead of WHERE.
// Parameters:
//   - cursor: the cursor value (e.g., trace_id or chat_id)
//   - sortOrder: "asc" or "desc"
//   - projectID: required for scoping the subquery to the correct project
//   - groupColumn: the column used in GROUP BY (e.g., "trace_id", "gram_chat_id")
//   - timeExpr: the time expression to compare (e.g., "min(time_unix_nano)")
func withHavingPagination(sb sq.SelectBuilder, cursor, sortOrder, projectID, groupColumn, timeExpr string) sq.SelectBuilder {
	if cursor == "" {
		return sb
	}

	subquery := "(SELECT " + timeExpr + " FROM telemetry_logs WHERE gram_project_id = ? AND " + groupColumn + " = ? GROUP BY " + groupColumn + " LIMIT 1)"

	if sortOrder == "asc" {
		return sb.Having(timeExpr+" > "+subquery, projectID, cursor)
	}
	return sb.Having(timeExpr+" < "+subquery, projectID, cursor)
}

// withHavingTuplePagination adds cursor-based HAVING conditions with tuple comparison for tie-breaking.
// Used for queries like ListChats where multiple records might have the same start time.
// The tuple comparison includes both the time expression and the group column for stable ordering.
func withHavingTuplePagination(sb sq.SelectBuilder, cursor, sortOrder, projectID, groupColumn, timeExpr string) sq.SelectBuilder {
	if cursor == "" {
		return sb
	}

	subquery := "(SELECT " + timeExpr + " FROM telemetry_logs WHERE gram_project_id = ? AND " + groupColumn + " = ? GROUP BY " + groupColumn + " LIMIT 1)"

	if sortOrder == "asc" {
		return sb.Having("("+timeExpr+", "+groupColumn+") > ("+subquery+", ?)", projectID, cursor, cursor)
	}
	return sb.Having("("+timeExpr+", "+groupColumn+") < ("+subquery+", ?)", projectID, cursor, cursor)
}

// withOrdering adds ORDER BY clauses based on sort direction.
// Supports an optional secondary column for tie-breaking.
func withOrdering(sb sq.SelectBuilder, sortOrder, primaryCol, secondaryCol string) sq.SelectBuilder {
	if sortOrder == "asc" {
		sb = sb.OrderBy(primaryCol + " ASC")
		if secondaryCol != "" {
			sb = sb.OrderBy(secondaryCol + " ASC")
		}
	} else {
		sb = sb.OrderBy(primaryCol + " DESC")
		if secondaryCol != "" {
			sb = sb.OrderBy(secondaryCol + " DESC")
		}
	}
	return sb
}
