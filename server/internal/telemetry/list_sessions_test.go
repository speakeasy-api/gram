package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/stretchr/testify/require"
)

func TestListSessions_OrgScopedFiltersAndAggregates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "sessions-" + uuid.NewString()[:8],
		Slug:           "sessions-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	now := time.Now().UTC()
	chatID1 := uuid.NewString()
	chatID2 := uuid.NewString()
	chatID3 := uuid.NewString()
	chatID4 := uuid.NewString()

	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-10 * time.Minute),
		chatID:       chatID1,
		email:        "olivia@example.com",
		department:   "Engineering",
		roles:        []string{"dev"},
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  100,
		outputTokens: 50,
		totalTokens:  150,
		cost:         1.25,
	})
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-9 * time.Minute),
		chatID:       chatID1,
		email:        "olivia@example.com",
		department:   "Engineering",
		roles:        []string{"dev"},
		hookSource:   "claude-code",
		model:        "sonnet",
		inputTokens:  200,
		outputTokens: 100,
		totalTokens:  300,
		cost:         0.75,
	})
	insertListSessionToolLog(t, ctx, listSessionLogParams{
		projectID:  projectID,
		timestamp:  now.Add(-8 * time.Minute),
		chatID:     chatID1,
		email:      "olivia@example.com",
		department: "Engineering",
		roles:      []string{"dev"},
		hookSource: "claude-code",
		statusCode: 200,
		toolURN:    "tools:http:petstore:listPets",
	})
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		projectID:    otherProject.ID.String(),
		timestamp:    now.Add(-7 * time.Minute),
		chatID:       chatID2,
		email:        "sam@example.com",
		department:   "Sales",
		roles:        []string{"seller"},
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  300,
		outputTokens: 100,
		totalTokens:  400,
		cost:         3.0,
	})
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-6 * time.Minute),
		chatID:       chatID3,
		email:        "olivia@example.com",
		department:   "Engineering",
		roles:        []string{"dev"},
		hookSource:   "cursor",
		model:        "opus",
		inputTokens:  500,
		outputTokens: 100,
		totalTokens:  600,
		cost:         5.0,
	})
	insertListSessionRawChatCompletionLog(t, ctx, listSessionLogParams{
		projectID:  projectID,
		timestamp:  now.Add(-5 * time.Minute),
		chatID:     chatID4,
		email:      "raw@example.com",
		department: "Engineering",
		roles:      []string{"dev"},
		hookSource: "claude-code",
		model:      "opus",
	})

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	allSessions := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3 &&
			res.Sessions[0].GramChatID == chatID3 &&
			res.Sessions[1].GramChatID == chatID2 &&
			res.Sessions[2].GramChatID == chatID1
	})
	require.Len(t, allSessions.Sessions, 3)
	require.Equal(t, chatID3, allSessions.Sessions[0].GramChatID)
	require.Equal(t, chatID2, allSessions.Sessions[1].GramChatID)
	require.Equal(t, otherProject.ID.String(), allSessions.Sessions[1].ProjectID)
	require.Equal(t, chatID1, allSessions.Sessions[2].GramChatID)

	filtered := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From: from,
		To:   to,
		Filters: []*gen.QueryFilter{
			{Dimension: "email", Values: []string{"olivia@example.com"}},
			{Dimension: "hook_source", Values: []string{"claude-code"}},
			{Dimension: "role", Values: []string{"dev"}},
		},
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		if len(res.Sessions) != 1 {
			return false
		}
		session := res.Sessions[0]
		return session.GramChatID == chatID1 &&
			session.MessageCount == 2 &&
			session.ToolCallCount == 1
	})
	require.Len(t, filtered.Sessions, 1)

	session := filtered.Sessions[0]
	require.Equal(t, chatID1, session.GramChatID)
	require.Equal(t, projectID, session.ProjectID)
	require.NotNil(t, session.UserEmail)
	require.Equal(t, "olivia@example.com", *session.UserEmail)
	require.NotNil(t, session.HookSource)
	require.Equal(t, "claude-code", *session.HookSource)
	require.Equal(t, int64(2), session.MessageCount)
	require.Equal(t, int64(1), session.ToolCallCount)
	require.Equal(t, int64(300), session.TotalInputTokens)
	require.Equal(t, int64(150), session.TotalOutputTokens)
	require.Equal(t, int64(450), session.TotalTokens)
	require.InDelta(t, 2.0, session.TotalCost, 1e-9)
	require.Equal(t, "success", session.Status)
	require.NotEmpty(t, session.StartTimeUnixNano)
	require.NotEmpty(t, session.EndTimeUnixNano)
	require.Greater(t, session.DurationSeconds, 0.0)

	byToolCalls := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "tool_call_count",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3 &&
			res.Sessions[0].GramChatID == chatID1 &&
			res.Sessions[0].ToolCallCount == 1
	})
	require.Equal(t, chatID1, byToolCalls.Sessions[0].GramChatID)

	byMessages := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "message_count",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3 &&
			res.Sessions[0].GramChatID == chatID1 &&
			res.Sessions[0].MessageCount == 2
	})
	require.Equal(t, chatID1, byMessages.Sessions[0].GramChatID)

	byDuration := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "duration_seconds",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3 &&
			res.Sessions[0].GramChatID == chatID1 &&
			res.Sessions[0].DurationSeconds > 0
	})
	require.Equal(t, chatID1, byDuration.Sessions[0].GramChatID)
}

func TestListSessions_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Now().UTC()
	chatIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	costs := []float64{3.0, 2.0, 1.0}
	for i, chatID := range chatIDs {
		insertListSessionCompletionLog(t, ctx, listSessionLogParams{
			projectID:    projectID,
			timestamp:    now.Add(time.Duration(i) * time.Minute),
			chatID:       chatID,
			email:        "user@example.com",
			department:   "Engineering",
			roles:        []string{"dev"},
			hookSource:   "claude-code",
			model:        "opus",
			inputTokens:  100,
			outputTokens: 50,
			totalTokens:  150,
			cost:         costs[i],
		})
	}

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3 &&
			res.Sessions[0].GramChatID == chatIDs[0] &&
			res.Sessions[1].GramChatID == chatIDs[1] &&
			res.Sessions[2].GramChatID == chatIDs[2]
	})

	page1, err := ti.service.ListSessions(ctx, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "total_cost",
		Limit:  1,
	})
	require.NoError(t, err)
	require.Len(t, page1.Sessions, 1)
	require.NotNil(t, page1.NextCursor)
	require.Equal(t, chatIDs[0], page1.Sessions[0].GramChatID)

	page2, err := ti.service.ListSessions(ctx, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "total_cost",
		Limit:  1,
		Cursor: page1.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, page2.Sessions, 1)
	require.NotNil(t, page2.NextCursor)
	require.Equal(t, chatIDs[1], page2.Sessions[0].GramChatID)

	page3, err := ti.service.ListSessions(ctx, &gen.ListSessionsPayload{
		From:   from,
		To:     to,
		SortBy: "total_cost",
		Limit:  1,
		Cursor: page2.NextCursor,
	})
	require.NoError(t, err)
	require.Len(t, page3.Sessions, 1)
	require.Nil(t, page3.NextCursor)
	require.Equal(t, chatIDs[2], page3.Sessions[0].GramChatID)
}

func waitForListSessions(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	payload *gen.ListSessionsPayload,
	ready func(*gen.ListSessionsResult) bool,
) *gen.ListSessionsResult {
	t.Helper()

	var result *gen.ListSessionsResult
	var err error
	require.Eventually(t, func() bool {
		result, err = ti.service.ListSessions(ctx, payload)
		return err == nil && result != nil && ready(result)
	}, 10*time.Second, 200*time.Millisecond, "expected list sessions result to become query-ready, err: %v", errors.Unwrap(err))
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	return result
}

type listSessionLogParams struct {
	projectID    string
	timestamp    time.Time
	chatID       string
	email        string
	department   string
	roles        []string
	hookSource   string
	model        string
	inputTokens  int
	outputTokens int
	totalTokens  int
	cost         float64
	statusCode   int32
	toolURN      string
}

func insertListSessionCompletionLog(t *testing.T, ctx context.Context, p listSessionLogParams) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	usageURN := p.hookSource + ":usage:metrics"
	attributes := map[string]any{
		"gen_ai.conversation.id":          p.chatID,
		"gen_ai.response.id":              uuid.NewString(),
		"gen_ai.response.model":           p.model,
		"gen_ai.usage.input_tokens":       p.inputTokens,
		"gen_ai.usage.output_tokens":      p.outputTokens,
		"gen_ai.usage.total_tokens":       p.totalTokens,
		"gen_ai.usage.cost":               p.cost,
		"gram.hook.source":                p.hookSource,
		"gram.resource.urn":               usageURN,
		"user.email":                      p.email,
		"user.attributes.department_name": p.department,
		"user.roles":                      p.roles,
	}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name, gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), p.timestamp.UnixNano(), p.timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		p.projectID, usageURN, "gram-agents", p.chatID)
	require.NoError(t, err)
}

func insertListSessionToolLog(t *testing.T, ctx context.Context, p listSessionLogParams) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	hookEvent := "PostToolUse"
	if p.statusCode >= 400 {
		hookEvent = "PostToolUseFailure"
	}
	hookURN := p.hookSource + ":hook:" + hookEvent
	toolName := p.toolURN
	if toolName == "" {
		toolName = "Bash"
	}
	attributes := map[string]any{
		"gen_ai.conversation.id":          p.chatID,
		"gram.hook.source":                p.hookSource,
		"gram.hook.event":                 hookEvent,
		"gram.resource.urn":               hookURN,
		"gram.tool.name":                  toolName,
		"http.response.status_code":       p.statusCode,
		"user.email":                      p.email,
		"user.attributes.department_name": p.department,
		"user.roles":                      p.roles,
	}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name, gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), p.timestamp.UnixNano(), p.timestamp.UnixNano(), "INFO", "tool call",
		nil, nil, string(attrsJSON), "{}",
		p.projectID, hookURN, "gram-agents", p.chatID)
	require.NoError(t, err)
}

func insertListSessionRawChatCompletionLog(t *testing.T, ctx context.Context, p listSessionLogParams) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":          p.chatID,
		"gen_ai.response.id":              uuid.NewString(),
		"gen_ai.response.model":           p.model,
		"gram.hook.source":                p.hookSource,
		"gram.resource.urn":               "agents:chat:completion",
		"user.email":                      p.email,
		"user.attributes.department_name": p.department,
		"user.roles":                      p.roles,
	}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name, gram_chat_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), p.timestamp.UnixNano(), p.timestamp.UnixNano(), "INFO", "raw chat completion",
		nil, nil, string(attrsJSON), "{}",
		p.projectID, "agents:chat:completion", "gram-agents", p.chatID)
	require.NoError(t, err)
}
