package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
)

// insertGramHostedJudgeLog writes a completion row the way the platform's
// risk-analysis judges produce them: gram-hosted inference logged under the
// session owner's email. These are the platform's spend, never the
// employee's usage.
func insertGramHostedJudgeLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, email string, inputTokens, outputTokens int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source":          string(tm.EventSourceChatCompletion),
		"gram.hook.source":           "risk-analysis",
		"user.email":                 email,
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.total_tokens":  inputTokens + outputTokens,
		"gen_ai.usage.cost":          cost,
		"gen_ai.response.model":      "judge-model",
		"gen_ai.conversation.id":     uuid.New().String(),
		"gen_ai.response.id":         uuid.New().String(),
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	require.NoError(t, conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, "chat:completions", "gram-server"))
}

// The per-user surfaces must never count Gram-hosted inference as the
// employee's usage: judge completions log under the session owner's email,
// and before the exclusion one employee's page showed 60M+ judge tokens as
// their own. Org-scope reads deliberately keep counting them, matching the
// summaries fast path.
func TestEmployeeSurfaces_ExcludeGramHostedInference(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	email := "judged-" + uuid.NewString()[:8] + "@example.com"

	now := time.Now().UTC()
	// One real cursor usage row and one enormous judge row for the same
	// person: the judge tokens must be invisible on every per-user surface.
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), email, 100, 50, 1.5)
	insertGramHostedJudgeLog(t, ctx, projectID, now.Add(-9*time.Minute), email, 1_000_000, 1_000_000, 99.0)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	// The employee page tiles.
	metrics, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:   from,
		To:     to,
		UserID: &email,
	})
	require.NoError(t, err)
	require.Equal(t, int64(150), metrics.Metrics.TotalTokens)
	require.InDelta(t, 1.5, metrics.Metrics.TotalCost, 0.001)

	// The employees list / per-user summary path over raw logs.
	users, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter:   &gen.SearchUsersFilter{From: from, To: to, UserIds: []string{email}},
		UserType: "internal",
		Limit:    10,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, users.Users, 1)
	require.Equal(t, int64(100), users.Users[0].TotalInputTokens)
	require.Equal(t, int64(150), users.Users[0].TotalTokens)

	// The user-scoped overview: the judge conversation must not count.
	scoped, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:   from,
		To:     to,
		UserID: &email,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), scoped.Summary.TotalChats)

	// The org-scope overview keeps current semantics: gram-hosted rows stay
	// counted there (the summaries fast path cannot express the exclusion;
	// changing org totals is a separate decision, pinned here on purpose).
	org, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From: from,
		To:   to,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), org.Summary.TotalChats)
}
