package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestListAttributeKeys_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Keys)
}

func TestListAttributeKeys_LogsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	ctx = switchOrganizationInCtx(t, ctx, ti.disabledLogsOrgID)

	now := time.Now().UTC()
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	_, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "logs are not enabled")
}

func TestListAttributeKeys_ReturnsSystemAndUserKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	baseTime := now.Add(-30 * time.Minute)

	// Insert a log with both system attributes and user attributes (app. prefix)
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    baseTime.Add(1 * time.Minute),
		gramURN:      "urn:gram:func:test",
		severity:     "INFO",
		serviceName:  "gram-functions",
		customAttrs: map[string]any{
			"http.route":      "/api/test",
			"app.user.region": "us-east-1",
			"app.env":         "production",
		},
	})

	time.Sleep(200 * time.Millisecond)

	from := baseTime.Add(-10 * time.Minute).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should contain system key (bare) and user keys (@-prefixed)
	require.Contains(t, result.Keys, "http.route")
	require.Contains(t, result.Keys, "@user.region")
	require.Contains(t, result.Keys, "@env")

	// Should NOT contain the raw app. prefix
	for _, key := range result.Keys {
		require.NotContains(t, key, "app.", "keys should not contain raw app. prefix, got %q", key)
	}
}

func TestListAttributeKeys_Deduplicated(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	baseTime := now.Add(-30 * time.Minute)

	// Insert multiple logs with the same attribute paths
	for i := range 3 {
		insertTelemetryLogWithParams(t, ctx, testLogParams{
			projectID:    projectID,
			deploymentID: deploymentID,
			timestamp:    baseTime.Add(time.Duration(i+1) * time.Minute),
			gramURN:      "urn:gram:func:test",
			severity:     "INFO",
			serviceName:  "gram-functions",
			customAttrs: map[string]any{
				"app.user.region": "us-east-1",
				"http.route":      "/api/test",
			},
		})
	}

	time.Sleep(200 * time.Millisecond)

	from := baseTime.Add(-10 * time.Minute).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Count occurrences of each key — should appear exactly once
	keyCounts := map[string]int{}
	for _, key := range result.Keys {
		keyCounts[key]++
	}

	require.Equal(t, 1, keyCounts["@user.region"], "user key should appear exactly once")
	require.Equal(t, 1, keyCounts["http.route"], "system key should appear exactly once")
}

func TestListAttributeKeys_SortedAlphabetically(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	baseTime := now.Add(-30 * time.Minute)

	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    baseTime.Add(1 * time.Minute),
		gramURN:      "urn:gram:func:test",
		severity:     "INFO",
		serviceName:  "gram-functions",
		customAttrs: map[string]any{
			"app.zebra":   "z",
			"app.alpha":   "a",
			"http.method": "GET",
		},
	})

	time.Sleep(200 * time.Millisecond)

	from := baseTime.Add(-10 * time.Minute).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, len(result.Keys), 3)

	// Keys should be sorted
	for i := 1; i < len(result.Keys); i++ {
		require.LessOrEqual(t, result.Keys[i-1], result.Keys[i],
			"keys should be sorted: %q should come before %q", result.Keys[i-1], result.Keys[i])
	}
}

func TestListAttributeKeys_TimeRangeFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()

	// Insert a log within the time range
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-30 * time.Minute),
		gramURN:      "urn:gram:func:test",
		severity:     "INFO",
		serviceName:  "gram-functions",
		customAttrs: map[string]any{
			"app.in.range": "yes",
		},
	})

	// Insert a log outside the time range (2 hours ago)
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    now.Add(-2 * time.Hour),
		gramURN:      "urn:gram:func:test",
		severity:     "INFO",
		serviceName:  "gram-functions",
		customAttrs: map[string]any{
			"app.out.of.range": "yes",
		},
	})

	time.Sleep(200 * time.Millisecond)

	// Query for last hour only
	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	result, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should contain the in-range attribute
	require.Contains(t, result.Keys, "@in.range")

	// Should NOT contain the out-of-range attribute
	require.NotContains(t, result.Keys, "@out.of.range")
}

func TestListAttributeKeys_ScopedByProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	otherProjectID := uuid.New().String()
	deploymentID := uuid.New().String()

	now := time.Now().UTC()
	baseTime := now.Add(-30 * time.Minute)

	// Insert log for the authenticated project
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    projectID,
		deploymentID: deploymentID,
		timestamp:    baseTime.Add(1 * time.Minute),
		gramURN:      "urn:gram:func:test",
		severity:     "INFO",
		serviceName:  "gram-functions",
		customAttrs: map[string]any{
			"app.my.attr": "yes",
		},
	})

	// Insert log for a different project
	insertTelemetryLogWithParams(t, ctx, testLogParams{
		projectID:    otherProjectID,
		deploymentID: deploymentID,
		timestamp:    baseTime.Add(2 * time.Minute),
		gramURN:      "urn:gram:func:test",
		severity:     "INFO",
		serviceName:  "gram-functions",
		customAttrs: map[string]any{
			"app.other.project.attr": "yes",
		},
	})

	time.Sleep(200 * time.Millisecond)

	from := baseTime.Add(-10 * time.Minute).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	result, err := ti.service.ListAttributeKeys(ctx, &gen.ListAttributeKeysPayload{
		From: from,
		To:   to,
	})

	require.NoError(t, err)
	require.NotNil(t, result)

	// Should contain our project's attribute
	require.Contains(t, result.Keys, "@my.attr")

	// Should NOT contain the other project's attribute
	require.NotContains(t, result.Keys, "@other.project.attr")
}
