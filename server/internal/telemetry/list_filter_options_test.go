package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListFilterOptions_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
		From:       from,
		To:         to,
		FilterType: "api_key",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Options)
}

func TestListFilterOptions_FilterByAPIKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	apiKey1 := "key-" + uuid.New().String()[:8]
	apiKey2 := "key-" + uuid.New().String()[:8]

	// Insert logs with different API keys
	// apiKey1 has 3 unique chats
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), uuid.New().String(), apiKey1)
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), apiKey1)
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), apiKey1)

	// apiKey2 has 1 unique chat
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), uuid.New().String(), apiKey2)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
			From:       from,
			To:         to,
			FilterType: "api_key",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Options, 2)

		// Results should be ordered by count descending
		assert.Equal(c, apiKey1, res.Options[0].ID)
		assert.Equal(c, int64(3), res.Options[0].Count)
		assert.Equal(c, apiKey2, res.Options[1].ID)
		assert.Equal(c, int64(1), res.Options[1].Count)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestListFilterOptions_FilterByUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	user1 := "user-" + uuid.New().String()[:8]
	user2 := "user-" + uuid.New().String()[:8]

	// Insert logs with different external user IDs
	// user1 has 2 unique chats
	insertLogWithExternalUser(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), uuid.New().String(), user1)
	insertLogWithExternalUser(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), uuid.New().String(), user1)

	// user2 has 3 unique chats
	insertLogWithExternalUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), uuid.New().String(), user2)
	insertLogWithExternalUser(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), uuid.New().String(), user2)
	insertLogWithExternalUser(t, ctx, projectID, deploymentID, now.Add(-6*time.Minute), uuid.New().String(), user2)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
			From:       from,
			To:         to,
			FilterType: "user",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Options, 2)

		// Results should be ordered by count descending
		assert.Equal(c, user2, res.Options[0].ID)
		assert.Equal(c, int64(3), res.Options[0].Count)
		assert.Equal(c, user1, res.Options[1].ID)
		assert.Equal(c, int64(2), res.Options[1].Count)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestListFilterOptions_InvalidFilterType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
		From:       from,
		To:         to,
		FilterType: "invalid_type",
	})

	require.Error(t, err)
	// Error is wrapped, so check for the outer message
	require.Contains(t, err.Error(), "error listing filter options")
}

func TestListFilterOptions_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
		From:       from,
		To:         to,
		FilterType: "api_key",
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestListFilterOptions_CountsUniqueChatSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	apiKey := "key-" + uuid.New().String()[:8]
	chatID := uuid.New().String()

	// Insert multiple logs for the SAME chat session with the same API key
	// This should count as only 1 unique chat, not 3
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), chatID, apiKey)
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), chatID, apiKey)
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), chatID, apiKey)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
			From:       from,
			To:         to,
			FilterType: "api_key",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Options, 1)

		// Should count as 1 unique chat, not 3
		assert.Equal(c, apiKey, res.Options[0].ID)
		assert.Equal(c, int64(1), res.Options[0].Count)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestListFilterOptions_TimeRangeFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	apiKeyInRange := "key-in-range-" + uuid.New().String()[:8]
	apiKeyOutOfRange := "key-out-of-range-" + uuid.New().String()[:8]

	// Insert log within the time range
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-30*time.Minute), uuid.New().String(), apiKeyInRange)

	// Insert log outside the time range (2 hours ago)
	insertLogWithAPIKey(t, ctx, projectID, deploymentID, now.Add(-2*time.Hour), uuid.New().String(), apiKeyOutOfRange)

	// Query for last hour only
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		res, err := ti.service.ListFilterOptions(ctx, &gen.ListFilterOptionsPayload{
			From:       from,
			To:         to,
			FilterType: "api_key",
		})
		if !assert.NoError(c, err) {
			return
		}
		if !assert.NotNil(c, res) {
			return
		}
		assert.Len(c, res.Options, 1)

		// Only the in-range API key should be returned
		assert.Equal(c, apiKeyInRange, res.Options[0].ID)
	}, 10*time.Second, 200*time.Millisecond)
}

// insertLogWithAPIKey inserts a log with the gram.api_key.id attribute set.
func insertLogWithAPIKey(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID, apiKeyID string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.api_key.id": apiKeyID,
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_chat_id, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "api call",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, chatID, "gram-api")
	require.NoError(t, err)
}

// insertLogWithExternalUser inserts a log with the gram.external_user.id attribute set.
func insertLogWithExternalUser(t *testing.T, ctx context.Context, projectID, deploymentID string, timestamp time.Time, chatID, externalUserID string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.external_user.id": externalUserID,
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_deployment_id, gram_chat_id, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "user action",
		nil, nil, string(attrsJSON), "{}",
		projectID, deploymentID, chatID, "gram-api")
	require.NoError(t, err)
}
