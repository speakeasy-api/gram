package telemetry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestToolUsage_IdentityChipsMatchRawPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	now := time.Now().UTC()

	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-25 * time.Minute),
		toolsetSlug: "payments",
		toolName:    "charge",
		userEmail:   "alice@example.com",
		statusCode:  200,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:      projectID,
		timestamp:      now.Add(-20 * time.Minute),
		toolsetSlug:    "payments",
		toolName:       "refund",
		externalUserID: "ext-1",
		statusCode:     500,
	})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{
		projectID:   projectID,
		timestamp:   now.Add(-15 * time.Minute),
		toolsetSlug: "billing",
		toolName:    "invoice",
		userID:      "user-1",
		statusCode:  200,
	})
	insertHookEvent(t, ctx, hookEventParams{
		projectID:      projectID,
		deploymentID:   uuid.New().String(),
		timestamp:      now.Add(-10 * time.Minute),
		traceID:        uuid.New().String(),
		userEmail:      "bob@example.com",
		hookSource:     "claude-code",
		toolName:       "Read",
		result:         `"ok"`,
		conversationID: "conv-local",
	})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)
	// ":" is in every seeded gram_urn, so it forces the raw path without
	// changing which traces match.
	rawQuery := ":"

	cases := []struct {
		name   string
		filter *gen.LogFilter
		want   int
	}{
		{"email in", &gen.LogFilter{Path: "user.email", Operator: "in", Values: []string{"alice@example.com", "bob@example.com"}}, 2},
		{"user id eq", &gen.LogFilter{Path: "user.id", Operator: "eq", Values: []string{"user-1"}}, 1},
		{"external user id not_eq", &gen.LogFilter{Path: "gram.external_user.id", Operator: "not_eq", Values: []string{"ext-1"}}, 3},
	}
	for _, tc := range cases {
		filters := []*gen.LogFilter{tc.filter}

		summaryTraces := requireToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{
			From: from, To: to, Filters: filters, Limit: 10, Sort: "desc",
		}, func(r *gen.ListToolUsageTracesResult) bool { return len(r.Traces) == tc.want })
		rawTraces := requireToolUsageTraces(t, ctx, ti, &gen.ListToolUsageTracesPayload{
			From: from, To: to, Filters: filters, Query: &rawQuery, Limit: 10, Sort: "desc",
		}, func(r *gen.ListToolUsageTracesResult) bool { return len(r.Traces) == tc.want })
		require.Equal(t, normalizeToolUsageTraces(rawTraces.Traces), normalizeToolUsageTraces(summaryTraces.Traces), tc.name)

		summary, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
			From: from, To: to, Filters: filters,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		raw, err := ti.service.GetToolUsageSummary(ctx, &gen.GetToolUsageSummaryPayload{
			From: from, To: to, Filters: filters, Query: &rawQuery,
		})
		require.NoError(t, err, "cause: %v", errors.Unwrap(err))
		require.Equal(t, int64(tc.want), summary.Totals.EventCount, tc.name)
		require.Equal(t, raw.Totals, summary.Totals, tc.name)
	}
}
