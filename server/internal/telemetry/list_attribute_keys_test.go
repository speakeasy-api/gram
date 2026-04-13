package telemetry_test

import (
	"context"
	"slices"
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

	payload := &gen.ListAttributeKeysPayload{
		From: baseTime.Add(-10 * time.Minute).Format(time.RFC3339),
		To:   now.Format(time.RFC3339),
	}

	result := waitForAttributeKey(t, ctx, ti.service, payload, "http.route")

	require.Contains(t, result.Keys, "http.route")
	require.Contains(t, result.Keys, "@user.region")
	require.Contains(t, result.Keys, "@env")

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

	payload := &gen.ListAttributeKeysPayload{
		From: baseTime.Add(-10 * time.Minute).Format(time.RFC3339),
		To:   now.Format(time.RFC3339),
	}

	result := waitForAttributeKey(t, ctx, ti.service, payload, "http.route")

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

	payload := &gen.ListAttributeKeysPayload{
		From: baseTime.Add(-10 * time.Minute).Format(time.RFC3339),
		To:   now.Format(time.RFC3339),
	}

	result := waitForAttributeKey(t, ctx, ti.service, payload, "http.method")

	require.GreaterOrEqual(t, len(result.Keys), 3)

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

	payload := &gen.ListAttributeKeysPayload{
		From: now.Add(-1 * time.Hour).Format(time.RFC3339),
		To:   now.Add(1 * time.Hour).Format(time.RFC3339),
	}

	result := waitForAttributeKey(t, ctx, ti.service, payload, "@in.range")

	require.Contains(t, result.Keys, "@in.range")
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

	payload := &gen.ListAttributeKeysPayload{
		From: baseTime.Add(-10 * time.Minute).Format(time.RFC3339),
		To:   now.Format(time.RFC3339),
	}

	result := waitForAttributeKey(t, ctx, ti.service, payload, "@my.attr")

	require.Contains(t, result.Keys, "@my.attr")
	require.NotContains(t, result.Keys, "@other.project.attr")
}

// waitForAttributeKey polls ListAttributeKeys until the given key appears in results.
func waitForAttributeKey(t *testing.T, ctx context.Context, service gen.Service, payload *gen.ListAttributeKeysPayload, key string) *gen.ListAttributeKeysResult {
	t.Helper()

	var result *gen.ListAttributeKeysResult
	require.Eventually(t, func() bool {
		var err error
		result, err = service.ListAttributeKeys(ctx, payload)
		if err != nil || result == nil {
			return false
		}
		return slices.Contains(result.Keys, key)
	}, 2*time.Second, 50*time.Millisecond, "expected attribute key %q to appear in ClickHouse", key)

	return result
}
