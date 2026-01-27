package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestSearchLogs_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 50,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs, "should return no logs when feature is disabled")
	require.False(t, result.Enabled, "Enabled should be false when logs feature is disabled")
}

func TestSearchLogs_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 50,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs)
	require.Nil(t, result.NextCursor)
}

func TestSearchLogs_SortDescending(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	// Insert 5 logs
	insertTestTelemetryLogs(t, ctx, projectID, deploymentID, 5)

	now := time.Now().UTC()
	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 10,
		// Empty sort should default to desc
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 5)
	require.Nil(t, result.NextCursor)

	// Verify descending order
	for i := 0; i < len(result.Logs)-1; i++ {
		require.GreaterOrEqual(t, result.Logs[i].TimeUnixNano, result.Logs[i+1].TimeUnixNano,
			"logs should be sorted descending by time")
	}
}

func TestSearchLogs_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	// Insert 10 logs
	insertTestTelemetryLogs(t, ctx, projectID, deploymentID, 10)

	now := time.Now().UTC()
	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Get first page (limit 4)
	page1, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 4,
		Sort:  "desc",
	})
	require.NoError(t, err)
	require.Len(t, page1.Logs, 4)
	require.NotNil(t, page1.NextCursor, "should have next cursor when more results exist")

	// Get second page using cursor
	page2, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page1.NextCursor,
		Limit:  4,
		Sort:   "desc",
	})
	require.NoError(t, err)
	require.Len(t, page2.Logs, 4)
	require.NotNil(t, page2.NextCursor, "should have next cursor for third page")

	// Get third page (remaining logs)
	page3, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Cursor: page2.NextCursor,
		Limit:  4,
		Sort:   "desc",
	})
	require.NoError(t, err)
	require.Len(t, page3.Logs, 2)
	require.Nil(t, page3.NextCursor, "should not have next cursor on last page")

	// Verify all logs are in descending order across pages
	allLogs := append(append(page1.Logs, page2.Logs...), page3.Logs...)
	for i := 0; i < len(allLogs)-1; i++ {
		require.Greater(t, allLogs[i].TimeUnixNano, allLogs[i+1].TimeUnixNano,
			"logs should be sorted descending across pages")
	}
}

func TestSearchLogs_FilterByTraceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	traceID1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	traceID2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), &traceID1, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-4*time.Minute), &traceID2, "urn:gram:test", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-3*time.Minute), &traceID1, "urn:gram:test", "ERROR")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From:    &from,
			To:      &to,
			TraceID: &traceID1,
		},
		Limit: 10,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 2)

	// Verify all logs have the correct trace ID
	for _, log := range result.Logs {
		require.NotNil(t, log.TraceID)
		require.Equal(t, traceID1, *log.TraceID)
	}
}

func TestSearchLogs_AttributesAreJSON(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	insertTelemetryLog(t, ctx, projectID, deploymentID, now, nil, "urn:gram:test", "INFO")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
		Filter: &gen.SearchLogsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 10,
		Sort:  "desc",
	})

	require.True(t, result.Enabled)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 1)

	log := result.Logs[0]
	require.NotNil(t, log.Attributes)
	require.NotNil(t, log.ResourceAttributes)

	// Attributes should be parsed as map, not string
	_, ok := log.Attributes.(map[string]any)
	require.True(t, ok, "attributes should be a map[string]any, not a string")

	_, ok = log.ResourceAttributes.(map[string]any)
	require.True(t, ok, "resource_attributes should be a map[string]any, not a string")
}

func TestSearchLogs_Filters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()

	// Create test data with diverse characteristics
	deployment1 := uuid.New().String()
	deployment2 := uuid.New().String()
	function1 := uuid.New().String()
	function2 := uuid.New().String()
	traceID1 := "11111111111111111111111111111111"
	traceID2 := "22222222222222222222222222222222"

	now := time.Now().UTC()
	baseTime := now.Add(-30 * time.Minute)

	// Insert diverse logs covering all filterable dimensions
	testLogs := []testLogParams{
		// Deployment 1, Function 1 - HTTP GET logs
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function1,
			timestamp:    baseTime.Add(1 * time.Minute),
			traceID:      &traceID1,
			gramURN:      "urn:gram:http:api:get-users",
			severity:     "INFO",
			httpMethod:   stringPtr("GET"),
			httpStatus:   int32Ptr(200),
			httpRoute:    stringPtr("/api/users"),
			serviceName:  "gram-http-gateway",
		},
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function1,
			timestamp:    baseTime.Add(2 * time.Minute),
			traceID:      &traceID1,
			gramURN:      "urn:gram:http:api:get-users",
			severity:     "DEBUG",
			httpMethod:   stringPtr("GET"),
			httpStatus:   int32Ptr(200),
			httpRoute:    stringPtr("/api/users"),
			serviceName:  "gram-http-gateway",
		},
		// Deployment 1, Function 1 - HTTP POST logs with errors
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function1,
			timestamp:    baseTime.Add(3 * time.Minute),
			traceID:      &traceID2,
			gramURN:      "urn:gram:http:api:create-order",
			severity:     "ERROR",
			httpMethod:   stringPtr("POST"),
			httpStatus:   int32Ptr(500),
			httpRoute:    stringPtr("/api/orders"),
			serviceName:  "gram-http-gateway",
		},
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function1,
			timestamp:    baseTime.Add(4 * time.Minute),
			traceID:      &traceID2,
			gramURN:      "urn:gram:http:api:create-order",
			severity:     "WARN",
			httpMethod:   stringPtr("POST"),
			httpStatus:   int32Ptr(500),
			httpRoute:    stringPtr("/api/orders"),
			serviceName:  "gram-http-gateway",
		},
		// Deployment 1, Function 2 - Function execution logs
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function2,
			timestamp:    baseTime.Add(5 * time.Minute),
			traceID:      nil,
			gramURN:      "urn:gram:function:utils:hash-password",
			severity:     "INFO",
			serviceName:  "gram-functions",
		},
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function2,
			timestamp:    baseTime.Add(6 * time.Minute),
			traceID:      nil,
			gramURN:      "urn:gram:function:utils:hash-password",
			severity:     "DEBUG",
			serviceName:  "gram-functions",
		},
		// Deployment 2, Function 2 - Different deployment
		{
			projectID:    projectID,
			deploymentID: deployment2,
			functionID:   &function2,
			timestamp:    baseTime.Add(7 * time.Minute),
			traceID:      nil,
			gramURN:      "urn:gram:function:auth:verify-token",
			severity:     "INFO",
			serviceName:  "gram-functions",
		},
		{
			projectID:    projectID,
			deploymentID: deployment2,
			functionID:   &function2,
			timestamp:    baseTime.Add(8 * time.Minute),
			traceID:      nil,
			gramURN:      "urn:gram:function:auth:verify-token",
			severity:     "ERROR",
			serviceName:  "gram-functions",
		},
		// Deployment 2, no function - HTTP DELETE
		{
			projectID:    projectID,
			deploymentID: deployment2,
			functionID:   nil,
			timestamp:    baseTime.Add(9 * time.Minute),
			traceID:      nil,
			gramURN:      "urn:gram:http:api:delete-user",
			severity:     "INFO",
			httpMethod:   stringPtr("DELETE"),
			httpStatus:   int32Ptr(204),
			httpRoute:    stringPtr("/api/users/:id"),
			serviceName:  "gram-http-gateway",
		},
		// Edge case: FATAL severity
		{
			projectID:    projectID,
			deploymentID: deployment1,
			functionID:   &function1,
			timestamp:    baseTime.Add(10 * time.Minute),
			traceID:      nil,
			gramURN:      "urn:gram:http:api:crash",
			severity:     "FATAL",
			httpMethod:   stringPtr("POST"),
			httpStatus:   int32Ptr(500),
			httpRoute:    stringPtr("/api/crash"),
			serviceName:  "gram-http-gateway",
		},
	}

	for _, log := range testLogs {
		insertTelemetryLogWithParams(t, ctx, log)
	}

	// Wait for ClickHouse eventual consistency
	time.Sleep(200 * time.Millisecond)

	from := baseTime.Add(-10 * time.Minute).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	tests := []struct {
		name          string
		filter        *gen.SearchLogsFilter
		expectedCount int
	}{
		{
			name: "filter by deployment_id returns only matching logs",
			filter: &gen.SearchLogsFilter{
				From:         &from,
				To:           &to,
				DeploymentID: &deployment1,
			},
			expectedCount: 7,
		},
		{
			name: "filter by function_id returns only matching logs",
			filter: &gen.SearchLogsFilter{
				From:       &from,
				To:         &to,
				FunctionID: &function2,
			},
			expectedCount: 4,
		},
		{
			name: "filter by gram_urn returns only matching logs",
			filter: &gen.SearchLogsFilter{
				From:    &from,
				To:      &to,
				GramUrn: stringPtr("urn:gram:http:api:get-users"),
			},
			expectedCount: 2,
		},
		{
			name: "filter by trace_id returns only matching logs",
			filter: &gen.SearchLogsFilter{
				From:    &from,
				To:      &to,
				TraceID: &traceID1,
			},
			expectedCount: 2,
		},
		{
			name: "filter by severity_text ERROR returns only error logs",
			filter: &gen.SearchLogsFilter{
				From:         &from,
				To:           &to,
				SeverityText: stringPtr("ERROR"),
			},
			expectedCount: 2,
		},
		{
			name: "filter by http_status_code 500 returns only 500 responses",
			filter: &gen.SearchLogsFilter{
				From:           &from,
				To:             &to,
				HTTPStatusCode: int32Ptr(500),
			},
			expectedCount: 3,
		},
		{
			name: "filter by http_route returns only matching routes",
			filter: &gen.SearchLogsFilter{
				From:      &from,
				To:        &to,
				HTTPRoute: stringPtr("/api/users"),
			},
			expectedCount: 2,
		},
		{
			name: "filter by http_method POST returns only POST requests",
			filter: &gen.SearchLogsFilter{
				From:       &from,
				To:         &to,
				HTTPMethod: stringPtr("POST"),
			},
			expectedCount: 3,
		},
		{
			name: "filter by name returns only matching service",
			filter: &gen.SearchLogsFilter{
				From:        &from,
				To:          &to,
				ServiceName: stringPtr("gram-functions"),
			},
			expectedCount: 4,
		},
		{
			name: "combine deployment_id and severity filters",
			filter: &gen.SearchLogsFilter{
				From:         &from,
				To:           &to,
				DeploymentID: &deployment1,
				SeverityText: stringPtr("INFO"),
			},
			expectedCount: 2,
		},
		{
			name: "combine function_id and gram_urn filters",
			filter: &gen.SearchLogsFilter{
				From:       &from,
				To:         &to,
				FunctionID: &function2,
				GramUrn:    stringPtr("urn:gram:function:utils:hash-password"),
			},
			expectedCount: 2,
		},
		{
			name: "time range filter excludes logs outside range",
			filter: &gen.SearchLogsFilter{
				From: stringPtr(baseTime.Add(5 * time.Minute).Format(time.RFC3339)),
				To:   stringPtr(baseTime.Add(8 * time.Minute).Format(time.RFC3339)),
			},
			expectedCount: 3,
		},
		{
			name: "filter by gram_urns with single URN returns matching logs",
			filter: &gen.SearchLogsFilter{
				From:     &from,
				To:       &to,
				GramUrns: []string{"urn:gram:http:api:get-users"},
			},
			expectedCount: 2,
		},
		{
			name: "filter by gram_urns with multiple URNs returns all matching logs",
			filter: &gen.SearchLogsFilter{
				From:     &from,
				To:       &to,
				GramUrns: []string{"urn:gram:http:api:get-users", "urn:gram:http:api:create-order"},
			},
			expectedCount: 4,
		},
		{
			name: "filter by gram_urns across different services",
			filter: &gen.SearchLogsFilter{
				From:     &from,
				To:       &to,
				GramUrns: []string{"urn:gram:http:api:get-users", "urn:gram:function:utils:hash-password"},
			},
			expectedCount: 4,
		},
		{
			name: "filter by gram_urns with empty array returns all logs",
			filter: &gen.SearchLogsFilter{
				From:     &from,
				To:       &to,
				GramUrns: []string{},
			},
			expectedCount: 10,
		},
		{
			name: "filter by gram_urns with non-matching URN returns empty",
			filter: &gen.SearchLogsFilter{
				From:     &from,
				To:       &to,
				GramUrns: []string{"urn:gram:nonexistent:tool"},
			},
			expectedCount: 0,
		},
		{
			name: "gram_urns takes precedence over gram_urn when both provided",
			filter: &gen.SearchLogsFilter{
				From:     &from,
				To:       &to,
				GramUrn:  stringPtr("urn:gram:http:api:get-users"),
				GramUrns: []string{"urn:gram:http:api:create-order"},
			},
			expectedCount: 2, // Only create-order logs, not get-users
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ti.service.SearchLogs(ctx, &gen.SearchLogsPayload{
				Filter: tt.filter,
				Limit:  100,
				Sort:   "desc",
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, result.Logs, tt.expectedCount, "expected %d logs but got %d", tt.expectedCount, len(result.Logs))

			// Verify all logs have valid OTel structure
			for _, log := range result.Logs {
				require.NotEmpty(t, log.ID)
				require.Positive(t, log.TimeUnixNano)
				require.NotEmpty(t, log.Body)
				require.NotNil(t, log.Attributes)
				require.NotNil(t, log.ResourceAttributes)
				require.NotNil(t, log.Service)
			}
		})
	}
}

// SearchToolCalls tests

func TestSearchToolCalls_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchToolCalls(ctx, &gen.SearchToolCallsPayload{
		Filter: &gen.SearchToolCallsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 50,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.ToolCalls, "should return no tool calls when feature is disabled")
	require.False(t, result.Enabled, "Enabled should be false when logs feature is disabled")
}

func TestSearchToolCalls_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchToolCalls(ctx, &gen.SearchToolCallsPayload{
		Filter: &gen.SearchToolCallsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 50,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.ToolCalls)
	require.Nil(t, result.NextCursor)
}

func TestSearchToolCalls_AggregatesByTraceID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	traceID1 := "cccccccccccccccccccccccccccccccc"
	traceID2 := "dddddddddddddddddddddddddddddddd"

	// Insert 3 logs for trace 1
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), &traceID1, "urn:gram:test1", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), &traceID1, "urn:gram:test1", "WARN")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), &traceID1, "urn:gram:test1", "ERROR")

	// Insert 2 logs for trace 2
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), &traceID2, "urn:gram:test2", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), &traceID2, "urn:gram:test2", "INFO")

	// Insert log with no trace ID (should be excluded)
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), nil, "urn:gram:test3", "INFO")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.SearchToolCalls(ctx, &gen.SearchToolCallsPayload{
		Filter: &gen.SearchToolCallsFilter{
			From: &from,
			To:   &to,
		},
		Limit: 100,
		Sort:  "desc",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.ToolCalls, 2)

	// Find both tool calls
	var toolCall1, toolCall2 *gen.ToolCallSummary
	for i := range result.ToolCalls {
		switch result.ToolCalls[i].TraceID {
		case traceID1:
			toolCall1 = result.ToolCalls[i]
		case traceID2:
			toolCall2 = result.ToolCalls[i]
		}
	}

	require.NotNil(t, toolCall1)
	require.Equal(t, uint64(3), toolCall1.LogCount)
	require.Positive(t, toolCall1.StartTimeUnixNano)
	require.Equal(t, "urn:gram:test1", toolCall1.GramUrn)

	require.NotNil(t, toolCall2)
	require.Equal(t, uint64(2), toolCall2.LogCount)
	require.Positive(t, toolCall2.StartTimeUnixNano)
	require.Equal(t, "urn:gram:test2", toolCall2.GramUrn)
}

func TestSearchToolCalls_FilterByGramURN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	traceID1 := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	traceID2 := "ffffffffffffffffffffffffffffffff"
	traceID3 := "11111111111111111111111111111111"

	// Insert logs with different gram URNs
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), &traceID1, "tools:http:petstore:listPets", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), &traceID2, "tools:http:petstore:getPet", "INFO")
	insertTelemetryLog(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), &traceID3, "tools:http:weather:getForecast", "INFO")

	// Wait for ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name          string
		gramUrn       string
		expectedCount int
		expectedURNs  []string
	}{
		{
			name:          "exact match returns single result",
			gramUrn:       "tools:http:petstore:listPets",
			expectedCount: 1,
			expectedURNs:  []string{"tools:http:petstore:listPets"},
		},
		{
			name:          "partial match on source returns multiple results",
			gramUrn:       "petstore",
			expectedCount: 2,
			expectedURNs:  []string{"tools:http:petstore:listPets", "tools:http:petstore:getPet"},
		},
		{
			name:          "partial match on tool name",
			gramUrn:       "getPet",
			expectedCount: 1,
			expectedURNs:  []string{"tools:http:petstore:getPet"},
		},
		{
			name:          "no match returns empty",
			gramUrn:       "nonexistent",
			expectedCount: 0,
			expectedURNs:  []string{},
		},
		{
			name:          "partial match on type",
			gramUrn:       "http",
			expectedCount: 3,
			expectedURNs:  []string{"tools:http:petstore:listPets", "tools:http:petstore:getPet", "tools:http:weather:getForecast"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ti.service.SearchToolCalls(ctx, &gen.SearchToolCallsPayload{
				Filter: &gen.SearchToolCallsFilter{
					From:    &from,
					To:      &to,
					GramUrn: &tt.gramUrn,
				},
				Limit: 100,
				Sort:  "desc",
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, result.ToolCalls, tt.expectedCount, "expected %d tool calls but got %d", tt.expectedCount, len(result.ToolCalls))

			// Verify all returned URNs are in the expected list
			for _, toolCall := range result.ToolCalls {
				require.Contains(t, tt.expectedURNs, toolCall.GramUrn, "unexpected gram_urn: %s", toolCall.GramUrn)
			}
		})
	}
}

func insertTestTelemetryLogs(t *testing.T, ctx context.Context, projectID, deploymentID string, count int) {
	t.Helper()

	now := time.Now().UTC().Add(-1 * time.Hour)

	for i := range count {
		timestamp := now.Add(time.Duration(i) * time.Minute)
		insertTelemetryLog(t, ctx, projectID, deploymentID, timestamp, nil, "urn:gram:test", "INFO")
	}

	// ClickHouse eventual consistency - sleep once at the end
	time.Sleep(100 * time.Millisecond)
}

func insertTelemetryLog(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, traceID *string, gramURN, severityText string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), severityText, "test log body",
		traceID, nil, "{}", "{}",
		projectID, deploymentID, gramURN, "test-service")
	require.NoError(t, err)

	// ClickHouse eventual consistency
	time.Sleep(100 * time.Millisecond)
}

type testLogParams struct {
	projectID    string
	deploymentID string
	functionID   *string
	timestamp    time.Time
	traceID      *string
	gramURN      string
	severity     string
	httpMethod   *string
	httpStatus   *int32
	httpRoute    *string
	serviceName  string
}

func insertTelemetryLogWithParams(t *testing.T, ctx context.Context, params testLogParams) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_function_id, gram_urn,
			service_name, service_version,
			http_request_method, http_response_status_code, http_route
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), params.timestamp.UnixNano(), params.timestamp.UnixNano(),
		params.severity, "test log body",
		params.traceID, nil, "{}", "{}",
		params.projectID, params.deploymentID, params.functionID, params.gramURN,
		params.serviceName, nil,
		params.httpMethod, params.httpStatus, params.httpRoute)
	require.NoError(t, err)
}

func stringPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}
