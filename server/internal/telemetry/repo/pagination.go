package repo

import (
	"github.com/Masterminds/squirrel"
)

// withPagination adds cursor-based WHERE conditions for simple (non-aggregated) queries.
// Uses composite tuple comparison: (time_unix_nano, id) > or < (cursor_time, cursor_id).
// This replaces the complex IF() conditional cursor logic from raw SQL.
func withPagination(sb squirrel.SelectBuilder, cursor, sortOrder string) squirrel.SelectBuilder {
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

type TableName string

const (
	TableNameTraceSummaries TableName = "trace_summaries"
)

// withHavingPagination adds cursor-based HAVING conditions for aggregation queries.
// Used when the query has GROUP BY and cursor pagination must use HAVING instead of WHERE.
// Parameters:
//   - cursor: the cursor value (e.g., trace_id or chat_id)
//   - sortOrder: "asc" or "desc"
//   - projectID: required for scoping the subquery to the correct project
//   - groupColumn: the column used in GROUP BY (e.g., "trace_id", "gram_chat_id")
//   - havingTimeExpr: the time expression for the outer HAVING clause (e.g., "start_time_unix_nano" alias)
//   - subqueryTimeExpr: the time expression for the subquery SELECT (e.g., "min(start_time_unix_nano)")
//   - tableName: the table to query in the subquery (e.g., "telemetry_logs" or "trace_summaries")
func withHavingPagination(sb squirrel.SelectBuilder, cursor, sortOrder, projectID, groupColumn, havingTimeExpr, subqueryTimeExpr string, tableName TableName) squirrel.SelectBuilder {
	if cursor == "" {
		return sb
	}

	subquery := "(SELECT " + subqueryTimeExpr + " FROM " + string(tableName) + " WHERE gram_project_id = ? AND " + groupColumn + " = ? GROUP BY " + groupColumn + " LIMIT 1)"

	if sortOrder == "asc" {
		return sb.Having(havingTimeExpr+" > "+subquery, projectID, cursor)
	}
	return sb.Having(havingTimeExpr+" < "+subquery, projectID, cursor)
}

// withHavingTuplePagination adds cursor-based HAVING conditions with tuple comparison for tie-breaking.
// Used for queries like ListChats where multiple records might have the same start time.
// The tuple comparison includes both the time expression and the group column for stable ordering.
// leftJoin/joinArgs, when non-empty, add a LEFT JOIN to the cursor-lookup subquery so
// group expressions that reference a join alias (e.g. SearchUsers' known_emails)
// stay valid inside it; pass "" and nil when the group column is a plain column.
func withHavingTuplePagination(sb squirrel.SelectBuilder, cursor, sortOrder, projectID, groupColumn, timeExpr, leftJoin string, joinArgs []any) squirrel.SelectBuilder {
	if cursor == "" {
		return sb
	}

	join := ""
	if leftJoin != "" {
		join = " LEFT JOIN " + leftJoin
	}
	subquery := "(SELECT " + timeExpr + " FROM telemetry_logs" + join + " WHERE gram_project_id = ? AND " + groupColumn + " = ? GROUP BY " + groupColumn + " LIMIT 1)"

	args := make([]any, 0, len(joinArgs)+3)
	args = append(args, joinArgs...)
	args = append(args, projectID, cursor, cursor)

	if sortOrder == "asc" {
		return sb.Having("("+timeExpr+", "+groupColumn+") > ("+subquery+", ?)", args...)
	}
	return sb.Having("("+timeExpr+", "+groupColumn+") < ("+subquery+", ?)", args...)
}

// withOrdering adds ORDER BY clauses based on sort direction.
// Supports an optional secondary column for tie-breaking.
func withOrdering(sb squirrel.SelectBuilder, sortOrder, primaryCol, secondaryCol string) squirrel.SelectBuilder {
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
