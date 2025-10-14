package logs_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/gen/logs"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	logsvc "github.com/speakeasy-api/gram/server/internal/logs"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics"
	"github.com/stretchr/testify/require"
)

var (
	infra *testenv.Environment
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	res, cleanup, err := testenv.Launch(ctx)
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err = cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type testInstance struct {
	service        *logsvc.Service
	conn           *pgxpool.Pool
	chClient       *toolmetrics.Queries
	sessionManager *sessions.Manager
}

func newTestLogsService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()

	logger := testenv.NewLogger(t)

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	billingClient := billing.NewStubClient(logger, testenv.NewTracerProvider(t))

	sessionManager, err := sessions.NewUnsafeManager(logger, conn, redisClient, cache.Suffix("gram-test"), "", billingClient)
	require.NoError(t, err)

	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)

	chClient := toolmetrics.New(logger, chConn, tracerProvider, func(ctx context.Context, log toolmetrics.ToolHTTPRequest) (bool, error) {
		return true, nil
	})

	svc := logsvc.NewService(logger, conn, sessionManager, chClient)

	return ctx, &testInstance{
		service:        svc,
		conn:           conn,
		chClient:       chClient,
		sessionManager: sessionManager,
	}
}

func TestListLogs_EmptyResult(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := uuid.New().String()

	result, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID,
		ToolID:           nil,
		TsStart:          nil,
		TsEnd:            nil,
		Cursor:           nil,
		PerPage:          20,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Logs)
	require.NotNil(t, result.Pagination)
	require.Equal(t, 20, *result.Pagination.PerPage)
	require.False(t, *result.Pagination.HasNextPage)
	require.Nil(t, result.Pagination.NextPageCursor)
}

func TestListLogs_SinglePage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	// Query logs
	result, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        "0199a9f5-5659-76e1-be43-1da0b881dcff",
		ToolID:           nil,
		TsStart:          nil,
		TsEnd:            nil,
		Cursor:           nil,
		PerPage:          10,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 10)
	require.NotNil(t, result.Pagination)
	require.Equal(t, 10, *result.Pagination.PerPage)
	require.False(t, *result.Pagination.HasNextPage)
	require.Nil(t, result.Pagination.NextPageCursor)

	// Verify logs are sorted descending by timestamp
	for i := 0; i < len(result.Logs)-1; i++ {
		ts1, err1 := time.Parse(time.RFC3339, result.Logs[i].Ts)
		ts2, err2 := time.Parse(time.RFC3339, result.Logs[i+1].Ts)
		require.NoError(t, err1)
		require.NoError(t, err2)
		require.True(t, ts1.After(ts2) || ts1.Equal(ts2), "logs should be sorted descending")
	}
}

func TestListLogs_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	projectID := "0199a9f5-5659-76e1-be43-1da0b881dcff"

	// Query first page
	result, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID,
		ToolID:           nil,
		TsStart:          nil,
		TsEnd:            nil,
		Cursor:           nil,
		PerPage:          3,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 3)
	require.NotNil(t, result.Pagination)
	require.Equal(t, 3, *result.Pagination.PerPage)
	require.True(t, *result.Pagination.HasNextPage)
	require.NotNil(t, result.Pagination.NextPageCursor)

	// Query second page using cursor
	cursor := result.Pagination.NextPageCursor
	result2, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID,
		ToolID:           nil,
		TsStart:          nil,
		TsEnd:            nil,
		Cursor:           cursor,
		PerPage:          3,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result2)
	require.Len(t, result2.Logs, 3)
	require.NotNil(t, result2.Pagination)
	require.Equal(t, 3, *result2.Pagination.PerPage)
	require.True(t, *result2.Pagination.HasNextPage)
	require.NotNil(t, result2.Pagination.NextPageCursor)

	// Query third page
	cursor = result2.Pagination.NextPageCursor
	result3, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID,
		ToolID:           nil,
		TsStart:          nil,
		TsEnd:            nil,
		Cursor:           cursor,
		PerPage:          4,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result3)
	require.Len(t, result3.Logs, 4) // Remaining logs
	require.NotNil(t, result3.Pagination)
	require.Equal(t, 4, *result3.Pagination.PerPage)
	require.False(t, *result3.Pagination.HasNextPage)
	require.Nil(t, result3.Pagination.NextPageCursor)

	// Verify no duplicate logs across pages
	firstPageIDs := make(map[string]bool)
	for _, toolLog := range result.Logs {
		firstPageIDs[toolLog.Ts] = true
	}

	for _, toolLog := range result2.Logs {
		require.False(t, firstPageIDs[toolLog.Ts], "found duplicate log in second page")
	}
}

func TestListLogs_TimeRangeFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	// Insert test data
	projectID := uuid.New().String()
	orgID := uuid.New().String()
	deploymentID := uuid.New().String()
	toolID := uuid.New().String()

	baseTime := time.Now().UTC().Add(-10 * time.Hour)

	// Insert 10 logs over 10 minutes
	for i := 0; i < 10; i++ {
		id, err := fromTimeV7(baseTime.Add(time.Duration(i) * time.Minute))
		require.NoError(t, err)

		err = ti.chClient.Log(ctx, toolmetrics.ToolHTTPRequest{
			ID:                id.String(),
			Ts:                baseTime.Add(time.Duration(i) * time.Minute),
			OrganizationID:    orgID,
			ProjectID:         projectID,
			DeploymentID:      deploymentID,
			ToolID:            toolID,
			ToolURN:           "urn:tool:test",
			ToolType:          toolmetrics.HTTPToolType,
			TraceID:           id.String()[:32],
			SpanID:            id.String()[:16],
			HTTPMethod:        "GET",
			HTTPRoute:         "/test",
			StatusCode:        200,
			DurationMs:        100.0,
			UserAgent:         "test-agent",
			RequestHeaders:    map[string]string{"Content-Type": "application/json"},
			RequestBodyBytes:  12,
			ResponseHeaders:   map[string]string{"Content-Type": "application/json"},
			ResponseBodyBytes: 13,
		})
		require.NoError(t, err)
	}

	// Query logs with time range (should get logs from index 2-7, i.e., 6 logs)
	tsStart := baseTime.Add(3 * time.Minute).Format(time.RFC3339)
	tsEnd := baseTime.Add(8 * time.Minute).Format(time.RFC3339)

	result, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID,
		ToolID:           nil,
		TsStart:          &tsStart,
		TsEnd:            &tsEnd,
		Cursor:           nil,
		PerPage:          20,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 6)

	// Verify all logs are within time range
	startTime := baseTime.Add(2 * time.Minute)
	endTime := baseTime.Add(8 * time.Minute)
	for _, toolLog := range result.Logs {
		ts, err := time.Parse(time.RFC3339, toolLog.Ts)
		require.NoError(t, err)
		require.True(t, ts.After(startTime) || ts.Equal(startTime))
		require.True(t, ts.Before(endTime) || ts.Equal(endTime))
	}
}

func TestListLogs_DifferentPageSizes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	testCases := []struct {
		perPage         int
		expectedCount   int
		expectedHasNext bool
	}{
		{perPage: 5, expectedCount: 5, expectedHasNext: true},
		{perPage: 10, expectedCount: 10, expectedHasNext: false},
		{perPage: 20, expectedCount: 10, expectedHasNext: false},
	}

	for _, tc := range testCases {
		result, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
			ApikeyToken:      nil,
			SessionToken:     nil,
			ProjectSlugInput: nil,
			ProjectID:        "0199a9f5-5659-76e1-be43-1da0b881dcff",
			ToolID:           nil,
			TsStart:          nil,
			TsEnd:            nil,
			Cursor:           nil,
			PerPage:          tc.perPage,
			Direction:        "next",
			Sort:             "DESC",
		})

		mapTime := func(ts []*logs.HTTPToolLog) []time.Time {
			times := make([]time.Time, len(result.Logs))
			for i, toolLog := range result.Logs {
				times[i], err = time.Parse(time.RFC3339, toolLog.Ts)
				require.NoError(t, err)
			}
			return times
		}

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, mapTime(result.Logs), tc.expectedCount, "perPage=%d", tc.perPage)
		require.Equal(t, tc.perPage, *result.Pagination.PerPage, "perPage=%d", tc.perPage)
		require.Equal(t, tc.expectedHasNext, *result.Pagination.HasNextPage, "perPage=%d", tc.perPage)
	}
}

func TestListLogs_VerifyLogFields(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	// Insert test data with specific values
	projectID := uuid.New().String()
	orgID := uuid.New().String()
	deploymentID := uuid.New().String()
	toolID := uuid.New().String()
	toolURN := "urn:tool:specific"
	traceID := "0123456789abcdef0123456789abcdef"
	spanID := "0123456789abcdef"

	id, err := uuid.NewV7()
	require.NoError(t, err)

	baseTime := time.Unix(id.Time().UnixTime()).
		UTC().Add(-10 * time.Minute)

	err = ti.chClient.Log(ctx, toolmetrics.ToolHTTPRequest{
		ID:                id.String(),
		Ts:                baseTime,
		OrganizationID:    orgID,
		ProjectID:         projectID,
		DeploymentID:      deploymentID,
		ToolID:            toolID,
		ToolURN:           toolURN,
		ToolType:          toolmetrics.HTTPToolType,
		TraceID:           traceID,
		SpanID:            spanID,
		HTTPMethod:        "POST",
		HTTPRoute:         "/api/users",
		StatusCode:        201,
		DurationMs:        250.5,
		UserAgent:         "Mozilla/5.0",
		RequestHeaders:    map[string]string{"Content-Type": "application/json", "Authorization": "Bearer token"},
		RequestBodyBytes:  17,
		ResponseHeaders:   map[string]string{"Content-Type": "application/json"},
		ResponseBodyBytes: 18,
	})
	require.NoError(t, err)

	// Query logs
	result, err := ti.service.ListLogs(ctx, &logs.ListLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ProjectID:        projectID,
		ToolID:           nil,
		TsStart:          nil,
		TsEnd:            nil,
		Cursor:           nil,
		PerPage:          10,
		Direction:        "next",
		Sort:             "DESC",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Logs, 1)

	toolLog := result.Logs[0]
	require.Equal(t, orgID, toolLog.OrganizationID)
	require.Equal(t, projectID, toolLog.ProjectID)
	require.Equal(t, deploymentID, toolLog.DeploymentID)
	require.Equal(t, toolID, toolLog.ToolID)
	require.Equal(t, toolURN, toolLog.ToolUrn)
	require.Equal(t, logs.ToolType("http"), toolLog.ToolType)
	require.Equal(t, traceID, toolLog.TraceID)
	require.Equal(t, spanID, toolLog.SpanID)
	require.Equal(t, "POST", toolLog.HTTPMethod)
	require.Equal(t, "/api/users", toolLog.HTTPRoute)
	require.Equal(t, int64(201), toolLog.StatusCode)
	require.InEpsilon(t, 250.5, toolLog.DurationMs, 0.001)
	require.Equal(t, "Mozilla/5.0", toolLog.UserAgent)
	require.NotNil(t, toolLog.RequestBodyBytes)
	require.Equal(t, int64(17), *toolLog.RequestBodyBytes)
	require.NotNil(t, toolLog.ResponseBodyBytes)
	require.Equal(t, int64(18), *toolLog.ResponseBodyBytes)
	require.Len(t, toolLog.RequestHeaders, 2)
	require.Equal(t, "application/json", toolLog.RequestHeaders["Content-Type"])
	require.Equal(t, "Bearer token", toolLog.RequestHeaders["Authorization"])
	require.Len(t, toolLog.ResponseHeaders, 1)
	require.Equal(t, "application/json", toolLog.ResponseHeaders["Content-Type"])
}

func fromTimeV7(t time.Time) (uuid.UUID, error) {
	var u uuid.UUID

	// 48-bit big-endian Unix time in milliseconds
	ms64 := t.UnixMilli()
	if ms64 < 0 {
		return uuid.Nil, fmt.Errorf("negative Unix milliseconds: %d", ms64)
	}
	ms := uint64(ms64)

	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)

	// 2) Fill remaining bytes [6..15] with cryptographic randomness
	if _, err := rand.Read(u[6:16]); err != nil {
		return uuid.Nil, fmt.Errorf("rand.Read: %w", err)
	}

	// 3) Set version (v7)
	u[6] = (u[6] & 0x0F) | 0x70

	// 4) Set variant (RFC 4122)
	u[8] = (u[8] & 0x3F) | 0x80

	return u, nil
}
