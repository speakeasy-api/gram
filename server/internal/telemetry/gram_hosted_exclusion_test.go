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
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// insertGramHostedCompletionLog writes a completion row the way Gram-hosted
// inference produces them (completionTelemetryIdentity in the openrouter
// client): resource URN chat:completion, a non-empty hook_source naming the
// surface, and — for platform-initiated sources like the risk-analysis
// judges — the nil-UUID conversation id, since those completions run outside
// any chat.
func insertGramHostedCompletionLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, hookSource, email, externalUserID, conversationID string, inputTokens, outputTokens int, cost float64) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source":          string(tm.EventSourceChatCompletion),
		"gram.hook.source":           hookSource,
		"gram.resource.urn":          "chat:completion",
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.total_tokens":  inputTokens + outputTokens,
		"gen_ai.usage.cost":          cost,
		"gen_ai.response.model":      "judge-model",
		"gen_ai.conversation.id":     conversationID,
		"gen_ai.response.id":         uuid.New().String(),
	}
	if email != "" {
		attributes["user.email"] = email
	}
	if externalUserID != "" {
		attributes["gram.external_user.id"] = externalUserID
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
		projectID, "chat:completion", "gram-server"))
}

// insertGramHostedJudgeLog writes a completion row the way the platform's
// risk-analysis judges produce them: gram-hosted inference logged under the
// session owner's email. These are the platform's spend, never the
// employee's usage.
func insertGramHostedJudgeLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, email string, inputTokens, outputTokens int, cost float64) {
	t.Helper()
	insertGramHostedCompletionLog(t, ctx, projectID, timestamp, "risk-analysis", email, "", uuid.Nil.String(), inputTokens, outputTokens, cost)
}

// insertGramHostedJudgeLogWithUserID writes a judge completion attributed by
// user_id with no email - the shape that could surface a phantom identity in
// the enrollment directory's email-less supplement.
func insertGramHostedJudgeLogWithUserID(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, userID string, inputTokens, outputTokens int) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gram.event.source":          string(tm.EventSourceChatCompletion),
		"gram.hook.source":           "risk-analysis",
		"user.id":                    userID,
		"gen_ai.usage.input_tokens":  inputTokens,
		"gen_ai.usage.output_tokens": outputTokens,
		"gen_ai.usage.total_tokens":  inputTokens + outputTokens,
		"gen_ai.response.model":      "judge-model",
		"gen_ai.conversation.id":     "00000000-0000-0000-0000-000000000000",
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
		projectID, "chat:completion", "gram-server"))
}

// The enrollment directory's email-less supplement scans raw telemetry_logs;
// a gram-hosted completion carrying a user_id but no email must not surface
// a phantom identity there (tokens were never at risk - the summaries MV
// excludes gram-hosted rows by provenance - but presence is presence).
func TestEnrollmentDirectory_ExcludesGramHostedPhantomIdentities(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	phantomID := "judge-only-" + uuid.NewString()

	now := time.Now().UTC()
	insertGramHostedJudgeLogWithUserID(t, ctx, projectID, now.Add(-10*time.Minute), phantomID, 5000, 5000)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter:   &gen.SearchUsersFilter{From: now.Add(-time.Hour).Format(time.RFC3339), To: now.Add(time.Hour).Format(time.RFC3339)},
		UserType: "internal",
		Source:   "agent_metrics",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	for _, u := range res.Users {
		require.NotEqual(t, phantomID, u.UserID, "judge-only identity must not appear in the enrollment directory")
	}
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

	// An explicit hook_source filter overrides the exclusion: the caller
	// named a specific source, so the judge row is exactly what they asked
	// for — not a silently empty result.
	judgeSource := "risk-analysis"
	judgeOnly, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:       from,
		To:         to,
		UserID:     &email,
		HookSource: &judgeSource,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2_000_000), judgeOnly.Metrics.TotalTokens)
	require.InDelta(t, 99.0, judgeOnly.Metrics.TotalCost, 0.001)

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

	// The role rollup aggregates the same per-user summaries, so judge
	// tokens must not inflate the (here Unassigned) role totals either.
	roles, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter:   &gen.SearchUsersFilter{From: from, To: to, UserIds: []string{email}},
		UserType: "internal",
		GroupBy:  "role",
		Limit:    10,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, roles.Roles, 1)
	require.Equal(t, "Unassigned", roles.Roles[0].RoleName)
	require.Equal(t, int64(100), roles.Roles[0].TotalInputTokens)

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

// An external user (Elements / hosted-chat consumer) has NO token-bearing
// rows other than Gram-hosted completions — every hosted completion carries
// a hook_source from the exclusion set. The exclusion therefore applies to
// employee scope only; applying it to external scope would erase these users
// entirely.
func TestExternalUserSurfaces_KeepGramHostedInference(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	externalUserID := "ext-hosted-" + uuid.NewString()

	now := time.Now().UTC()
	insertGramHostedCompletionLog(t, ctx, projectID, now.Add(-10*time.Minute), "elements", "", externalUserID, uuid.New().String(), 100, 50, 1.5)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	// The external-user metrics tiles.
	metrics, err := ti.service.GetUserMetricsSummary(ctx, &gen.GetUserMetricsSummaryPayload{
		From:           from,
		To:             to,
		ExternalUserID: &externalUserID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(150), metrics.Metrics.TotalTokens)
	require.InDelta(t, 1.5, metrics.Metrics.TotalCost, 0.001)

	// The external users list.
	users, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter:   &gen.SearchUsersFilter{From: from, To: to, UserIds: []string{externalUserID}},
		UserType: "external",
		Limit:    10,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, users.Users, 1)
	require.Equal(t, externalUserID, users.Users[0].UserID)
	require.Equal(t, int64(150), users.Users[0].TotalTokens)

	// The external-user-scoped overview.
	scoped, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:           from,
		To:             to,
		ExternalUserID: &externalUserID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), scoped.Summary.TotalChats)
}
