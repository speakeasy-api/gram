package repo

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildListLogsQuery(t *testing.T) {
	t.Parallel()
	projectID := "test-project-id"
	tsStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tsEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)
	cursor := "test-cursor-id"

	tests := []struct {
		name          string
		opts          ListToolLogsOptions
		expectedQuery string
		expectedArgs  []any
	}{
		{
			name: "basic query with DESC sort order",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) order by ts desc limit $5",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, 21},
		},
		{
			name: "basic query with ASC sort order",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "asc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts > UUIDv7ToDateTime(toUUID($4)) order by ts limit $5",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, 21},
		},
		{
			name: "query with success status filter",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				Status:    "success",
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and status_code <= 399 order by ts desc limit $5",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, 21},
		},
		{
			name: "query with failure status filter",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				Status:    "failure",
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and status_code >= 400 order by ts desc limit $5",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, 21},
		},
		{
			name: "query with server name filter",
			opts: ListToolLogsOptions{
				ProjectID:  projectID,
				TsStart:    tsStart,
				TsEnd:      tsEnd,
				Cursor:     cursor,
				ServerName: "my-server",
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and tool_urn LIKE $5 order by ts desc limit $6",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "%my-server%", 21},
		},
		{
			name: "query with tool name filter",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				ToolName:  "my-tool",
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and tool_urn LIKE $5 order by ts desc limit $6",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "%my-tool%", 21},
		},
		{
			name: "query with tool type filter",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				ToolType:  "http",
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and tool_type = $5 order by ts desc limit $6",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "http", 21},
		},
		{
			name: "query with single tool URN filter",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				ToolURNs:  []string{"tool-urn-1"},
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and tool_urn IN ($5) order by ts desc limit $6",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "tool-urn-1", 21},
		},
		{
			name: "query with multiple tool URN filters",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				ToolURNs:  []string{"tool-urn-1", "tool-urn-2", "tool-urn-3"},
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and tool_urn IN ($5, $6, $7) order by ts desc limit $8",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "tool-urn-1", "tool-urn-2", "tool-urn-3", 21},
		},
		{
			name: "query with server name and tool name filters",
			opts: ListToolLogsOptions{
				ProjectID:  projectID,
				TsStart:    tsStart,
				TsEnd:      tsEnd,
				Cursor:     cursor,
				ServerName: "my-server",
				ToolName:   "my-tool",
				Pagination: &Pagination{
					PerPage: 20,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) and tool_urn LIKE $5 and tool_urn LIKE $6 order by ts desc limit $7",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "%my-server%", "%my-tool%", 21},
		},
		{
			name: "query with all filters combined",
			opts: ListToolLogsOptions{
				ProjectID:  projectID,
				TsStart:    tsStart,
				TsEnd:      tsEnd,
				Cursor:     cursor,
				Status:     "success",
				ServerName: "my-server",
				ToolName:   "my-tool",
				ToolType:   "http",
				ToolURNs:   []string{"tool-urn-1", "tool-urn-2"},
				Pagination: &Pagination{
					PerPage: 50,
					Sort:    "asc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts > UUIDv7ToDateTime(toUUID($4)) and status_code <= 399 and tool_urn LIKE $5 and tool_urn LIKE $6 and tool_type = $7 and tool_urn IN ($8, $9) order by ts limit $10",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, "%my-server%", "%my-tool%", "http", "tool-urn-1", "tool-urn-2", 51},
		},
		{
			name: "query with custom limit",
			opts: ListToolLogsOptions{
				ProjectID: projectID,
				TsStart:   tsStart,
				TsEnd:     tsEnd,
				Cursor:    cursor,
				Pagination: &Pagination{
					PerPage: 100,
					Sort:    "desc",
				},
			},
			expectedQuery: "select * from http_requests_raw where project_id = $1 and ts >= $2 and ts <= $3 and ts < UUIDv7ToDateTime(toUUID($4)) order by ts desc limit $5",
			expectedArgs:  []any{projectID, tsStart, tsEnd, cursor, 101},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, args := buildListHTTPRequestsQuery(tt.opts)

			// Normalize whitespace for comparison
			normalizedQuery := strings.Join(strings.Fields(query), " ")
			normalizedExpected := strings.Join(strings.Fields(tt.expectedQuery), " ")

			require.Equal(t, normalizedExpected, normalizedQuery, "query mismatch")
			require.Equal(t, tt.expectedArgs, args, "args mismatch")
		})
	}
}

func TestBuildListLogsQuery_ParameterIndexing(t *testing.T) {
	t.Parallel()
	// This test verifies that parameter indices are correctly incremented
	// when multiple filters are applied
	projectID := "test-project-id"
	tsStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tsEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	opts := ListToolLogsOptions{
		ProjectID:  projectID,
		TsStart:    tsStart,
		TsEnd:      tsEnd,
		Cursor:     "cursor-1",
		ServerName: "server-1",
		ToolName:   "tool-1",
		ToolType:   "http",
		ToolURNs:   []string{"urn-1", "urn-2"},
		Pagination: &Pagination{
			PerPage: 20,
			Sort:    "DESC",
		},
	}

	query, args := buildListHTTPRequestsQuery(opts)

	// Verify that all parameter indices are present and sequential
	require.Contains(t, query, "$1")
	require.Contains(t, query, "$2")
	require.Contains(t, query, "$3")
	require.Contains(t, query, "$4")
	require.Contains(t, query, "$5")
	require.Contains(t, query, "$6")
	require.Contains(t, query, "$7")
	require.Contains(t, query, "$8")
	require.Contains(t, query, "$9")
	require.Contains(t, query, "$10")

	// Verify correct number of arguments
	require.Len(t, args, 10)
}

func TestBuildListLogsQuery_EmptyToolURNs(t *testing.T) {
	t.Parallel()
	// Verify that empty ToolURNs slice doesn't add an IN clause
	projectID := "test-project-id"
	tsStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tsEnd := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	opts := ListToolLogsOptions{
		ProjectID: projectID,
		TsStart:   tsStart,
		TsEnd:     tsEnd,
		Cursor:    "cursor-1",
		ToolURNs:  []string{},
		Pagination: &Pagination{
			PerPage: 20,
			Sort:    "DESC",
		},
	}

	query, args := buildListHTTPRequestsQuery(opts)

	require.NotContains(t, query, " IN (")
	require.Len(t, args, 5) // project_id, ts_start, ts_end, cursor, limit
}
