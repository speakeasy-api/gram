package repo

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"
)

// projectMcpTopEndUsersLimit is how many end users GetProjectMcpUsage returns.
// The project home card shows the top 5.
const projectMcpTopEndUsersLimit = 5

type GetProjectMcpUsageParams struct {
	GramProjectID string
	TimeStart     int64
	TimeEnd       int64
}

// ProjectMcpEndUserRow is one external end user's tool-call count.
type ProjectMcpEndUserRow struct {
	UserID    string `ch:"user_id"`
	ToolCalls uint64 `ch:"tool_calls"`

	// EndUsersCount is the number of distinct external end users in the
	// window. Every row carries the same value.
	EndUsersCount uint64 `ch:"end_users_count"`
}

// ProjectMcpToolRow is one tool's calls and failures by external end users.
type ProjectMcpToolRow struct {
	ToolURN  string `ch:"tool_urn"`
	Calls    uint64 `ch:"calls"`
	Failures uint64 `ch:"failures"`
}

// ProjectMcpUsage is the external end user and tool summary for one project.
type ProjectMcpUsage struct {
	// EndUsersCount counts every external end user with any row in the
	// window, with or without tool calls.
	EndUsersCount uint64

	// TopEndUsers holds the top end users by tool calls.
	TopEndUsers []ProjectMcpEndUserRow

	// Tools holds every tool called in the window, unordered.
	Tools []ProjectMcpToolRow
}

// GetProjectMcpUsage returns the external end user count, the top end users by
// tool calls, and per-tool calls and failures for a project. Rows count only
// when they carry an external_user_id; unlike SearchUsers' external grouping,
// rows identified only by user_email are not end users here. Tool calls,
// failures and tool keys use SearchUsers' expressions (toolCallExprsFor).
//
// It reads raw telemetry_logs: no summary view is keyed by external_user_id.
//
//nolint:errcheck,wrapcheck // Replicating SQLC syntax which doesn't comply to this lint rule
func (q *Queries) GetProjectMcpUsage(ctx context.Context, arg GetProjectMcpUsageParams) (ProjectMcpUsage, error) {
	tc := toolCallExprsFor("")
	base := sq.Select().
		From("telemetry_logs").
		Where("gram_project_id = ?", arg.GramProjectID).
		Where("time_unix_nano >= ?", arg.TimeStart).
		Where("time_unix_nano <= ?", arg.TimeEnd).
		Where("external_user_id != ''")

	// count() OVER () runs after GROUP BY and before LIMIT, so it counts every
	// end user, not just the returned top rows.
	usersQuery, usersArgs, err := base.Columns(
		"external_user_id AS user_id",
		"countIf("+tc.isCall+") AS tool_calls",
		"count() OVER () AS end_users_count",
	).
		GroupBy("external_user_id").
		OrderBy("tool_calls DESC", "user_id ASC").
		Limit(projectMcpTopEndUsersLimit).
		ToSql()
	if err != nil {
		return ProjectMcpUsage{}, fmt.Errorf("build project mcp end users query: %w", err)
	}

	toolsQuery, toolsArgs, err := base.Columns(
		tc.key+" AS tool_urn",
		"count() AS calls",
		"countIf("+tc.isFailure+") AS failures",
	).
		Where(tc.isCall).
		GroupBy("tool_urn").
		ToSql()
	if err != nil {
		return ProjectMcpUsage{}, fmt.Errorf("build project mcp tools query: %w", err)
	}

	// Independent reads of the same window run concurrently; each goroutine
	// writes a distinct field of usage.
	usage := ProjectMcpUsage{EndUsersCount: 0, TopEndUsers: nil, Tools: nil}
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		rows, err := q.conn.Query(egCtx, usersQuery, usersArgs...)
		if err != nil {
			return fmt.Errorf("query project mcp end users: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var row ProjectMcpEndUserRow
			if err := rows.ScanStruct(&row); err != nil {
				return fmt.Errorf("scan project mcp end user row: %w", err)
			}
			usage.TopEndUsers = append(usage.TopEndUsers, row)
		}
		return rows.Err()
	})
	eg.Go(func() error {
		rows, err := q.conn.Query(egCtx, toolsQuery, toolsArgs...)
		if err != nil {
			return fmt.Errorf("query project mcp tools: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var row ProjectMcpToolRow
			if err := rows.ScanStruct(&row); err != nil {
				return fmt.Errorf("scan project mcp tool row: %w", err)
			}
			usage.Tools = append(usage.Tools, row)
		}
		return rows.Err()
	})
	if err := eg.Wait(); err != nil {
		return ProjectMcpUsage{}, err
	}

	if len(usage.TopEndUsers) > 0 {
		usage.EndUsersCount = usage.TopEndUsers[0].EndUsersCount
	}
	return usage, nil
}
