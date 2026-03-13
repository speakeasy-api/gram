package repo

import (
	"github.com/speakeasy-api/gram/server/internal/chq"
)

// withPagination adds cursor-based WHERE conditions for simple (non-aggregated) queries.
// Uses composite tuple comparison: (time_unix_nano, id) > or < (cursor_time, cursor_id).
func withPagination(sb chq.SelectBuilder, cursor, sortOrder string) chq.SelectBuilder {
	if cursor == "" {
		return sb
	}

	if sortOrder == "asc" {
		return sb.Where(chq.Expr(
			"(time_unix_nano, toUUID(id)) > (SELECT time_unix_nano, toUUID(id) FROM telemetry_logs WHERE id = toUUID(?) LIMIT 1)",
			cursor,
		))
	}
	return sb.Where(chq.Expr(
		"(time_unix_nano, toUUID(id)) < (SELECT time_unix_nano, toUUID(id) FROM telemetry_logs WHERE id = toUUID(?) LIMIT 1)",
		cursor,
	))
}

// TableName is the name of a ClickHouse table.
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
func withHavingPagination(sb chq.SelectBuilder, cursor, sortOrder, projectID, groupColumn, havingTimeExpr, subqueryTimeExpr string, tableName TableName) chq.SelectBuilder {
	if cursor == "" {
		return sb
	}

	subquery := "(SELECT " + subqueryTimeExpr + " FROM " + string(tableName) + " WHERE gram_project_id = ? AND " + groupColumn + " = ? GROUP BY " + groupColumn + " LIMIT 1)"

	if sortOrder == "asc" {
		return sb.Having(chq.Expr(havingTimeExpr+" > "+subquery, projectID, cursor))
	}
	return sb.Having(chq.Expr(havingTimeExpr+" < "+subquery, projectID, cursor))
}

// withHavingTuplePagination adds cursor-based HAVING conditions with tuple comparison for tie-breaking.
// Used for queries like ListChats where multiple records might have the same start time.
// The tuple comparison includes both the time expression and the group column for stable ordering.
func withHavingTuplePagination(sb chq.SelectBuilder, cursor, sortOrder, projectID, groupColumn, timeExpr string) chq.SelectBuilder {
	if cursor == "" {
		return sb
	}

	subquery := "(SELECT " + timeExpr + " FROM telemetry_logs WHERE gram_project_id = ? AND " + groupColumn + " = ? GROUP BY " + groupColumn + " LIMIT 1)"

	if sortOrder == "asc" {
		return sb.Having(chq.Expr("("+timeExpr+", "+groupColumn+") > ("+subquery+", ?)", projectID, cursor, cursor))
	}
	return sb.Having(chq.Expr("("+timeExpr+", "+groupColumn+") < ("+subquery+", ?)", projectID, cursor, cursor))
}

// withOrdering adds ORDER BY clauses based on sort direction.
// Supports an optional secondary column for tie-breaking.
func withOrdering(sb chq.SelectBuilder, sortOrder, primaryCol, secondaryCol string) chq.SelectBuilder {
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
