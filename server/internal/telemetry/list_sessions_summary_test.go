package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/stretchr/testify/require"
)

// Windows at least repo.SessionSummaryMinWindow wide route ListSessions onto
// chat_session_summaries; the ±1h windows used by list_sessions_test.go stay
// on the raw telemetry_logs path. These tests insert rows at recent
// timestamps (inside the MV's live-ingestion range) and query them through
// wide windows so the summary path serves them. The window width derives from
// the routing constant so a retuned threshold cannot silently flip these
// tests back onto the raw path.
func summaryWindow(now time.Time) (string, string) {
	return now.Add(-(repo.SessionSummaryMinWindow + 24*time.Hour)).Format(time.RFC3339),
		now.Add(1 * time.Hour).Format(time.RFC3339)
}

// TestListSessions_SummaryPathMatchesRawPath asserts the two ListSessions
// paths return identical sessions for the same data: the raw path via a
// narrow window, the summary path via a wide one. Both windows fully contain
// every inserted row, so any difference is a divergence between the raw
// aggregation and the MV.
func TestListSessions_SummaryPathMatchesRawPath(t *testing.T) {
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
	claudeChatID := uuid.NewString()
	agentChatID := uuid.NewString()
	failedChatID := uuid.NewString()

	// A Claude session: two turns (different models) plus a successful tool
	// call.
	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-10 * time.Minute),
		chatID:       claudeChatID,
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
	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-9 * time.Minute),
		chatID:       claudeChatID,
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
	insertListSessionClaudeToolResultLog(t, ctx, listSessionLogParams{
		projectID:  projectID,
		timestamp:  now.Add(-8 * time.Minute),
		chatID:     claudeChatID,
		email:      "olivia@example.com",
		department: "Engineering",
		roles:      []string{"dev"},
		statusCode: 200,
		toolURN:    "mcp__petstore__listPets",
	})
	// A Cursor usage session.
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-6 * time.Minute),
		chatID:       agentChatID,
		email:        "sam@example.com",
		department:   "Sales",
		roles:        []string{"seller"},
		hookSource:   "cursor",
		model:        "opus",
		inputTokens:  500,
		outputTokens: 100,
		totalTokens:  600,
		cost:         5.0,
	})
	// A Claude session whose only tool call failed.
	insertListSessionClaudeToolResultLog(t, ctx, listSessionLogParams{
		projectID:  projectID,
		timestamp:  now.Add(-5 * time.Minute),
		chatID:     failedChatID,
		email:      "olivia@example.com",
		department: "Engineering",
		roles:      []string{"dev"},
		statusCode: 500,
		toolURN:    "mcp__petstore__createPet",
	})

	narrowFrom := now.Add(-1 * time.Hour).Format(time.RFC3339)
	narrowTo := now.Add(1 * time.Hour).Format(time.RFC3339)
	wideFrom, wideTo := summaryWindow(now)

	rawRes := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   narrowFrom,
		To:     narrowTo,
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3
	})

	summaryRes := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   wideFrom,
		To:     wideTo,
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3
	})

	require.Equal(t, rawRes.Sessions, summaryRes.Sessions,
		"summary-backed sessions must be identical to the raw-log derivation")

	// Spot-check the merged measures on the summary result directly.
	byChat := map[string]*gen.SessionSummary{}
	for _, s := range summaryRes.Sessions {
		byChat[s.GramChatID] = s
	}
	claude := byChat[claudeChatID]
	require.NotNil(t, claude)
	require.Equal(t, int64(2), claude.MessageCount)
	require.Equal(t, int64(1), claude.ToolCallCount)
	require.Equal(t, int64(300), claude.TotalInputTokens)
	require.InDelta(t, 2.0, claude.TotalCost, 1e-9)
	require.Equal(t, "success", claude.Status)
	require.NotNil(t, claude.Model)
	require.Equal(t, "sonnet", *claude.Model, "argMax must pick the latest non-empty model")

	failed := byChat[failedChatID]
	require.NotNil(t, failed)
	require.Equal(t, "error", failed.Status)
}

// TestListSessions_SummaryPathFilters covers the filter translation onto the
// summary table: scalar HAVING over merged value arrays, the "(unset)"
// bucket, and the co-located Claude attribution tuple match.
func TestListSessions_SummaryPathFilters(t *testing.T) {
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
	engChatID := uuid.NewString()
	salesChatID := uuid.NewString()
	anonChatID := uuid.NewString()
	skillChatID := uuid.NewString()
	skillWithAgentChatID := uuid.NewString()

	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-10 * time.Minute),
		chatID:       engChatID,
		email:        "olivia@example.com",
		department:   "Engineering",
		roles:        []string{"dev"},
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  100,
		outputTokens: 50,
		cost:         1.0,
	})
	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-9 * time.Minute),
		chatID:       salesChatID,
		email:        "sam@example.com",
		department:   "Sales",
		roles:        []string{"seller"},
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  100,
		outputTokens: 50,
		cost:         2.0,
	})
	// An identity-less session: no email on any row, so it must land in the
	// email dimension's "(unset)" bucket.
	insertListSessionCompletionLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-8 * time.Minute),
		chatID:       anonChatID,
		email:        "",
		department:   "",
		hookSource:   "cursor",
		model:        "opus",
		inputTokens:  100,
		outputTokens: 50,
		cost:         3.0,
	})
	// Co-located attribution: skillChatID has skill=golang with no agent on
	// one row (matches the drilled slice), plus a later row with an agent.
	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-7 * time.Minute),
		chatID:       skillChatID,
		email:        "skill@example.com",
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  100,
		outputTokens: 50,
		cost:         1.0,
		skillName:    "golang",
	})
	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-6 * time.Minute),
		chatID:       skillChatID,
		email:        "skill@example.com",
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  50,
		outputTokens: 25,
		cost:         0.5,
		skillName:    "claude-api",
		agentName:    "Explore",
	})
	// skillWithAgentChatID carries the requested skill only on a row WITH an
	// agent — it must not match the skill=golang AND agent="" slice.
	insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
		projectID:    projectID,
		timestamp:    now.Add(-5 * time.Minute),
		chatID:       skillWithAgentChatID,
		email:        "skill@example.com",
		hookSource:   "claude-code",
		model:        "opus",
		inputTokens:  200,
		outputTokens: 50,
		cost:         2.0,
		skillName:    "golang",
		agentName:    "Explore",
	})

	wideFrom, wideTo := summaryWindow(now)

	deptFiltered := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From: wideFrom,
		To:   wideTo,
		Filters: []*gen.QueryFilter{
			{Dimension: "department_name", Values: []string{"Engineering"}},
		},
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 1 && res.Sessions[0].GramChatID == engChatID
	})
	require.Len(t, deptFiltered.Sessions, 1)

	unsetEmail := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From: wideFrom,
		To:   wideTo,
		Filters: []*gen.QueryFilter{
			{Dimension: "email", Values: []string{""}},
		},
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 1 && res.Sessions[0].GramChatID == anonChatID
	})
	require.Len(t, unsetEmail.Sessions, 1)

	roleFiltered := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From: wideFrom,
		To:   wideTo,
		Filters: []*gen.QueryFilter{
			{Dimension: "role", Values: []string{"seller"}},
		},
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 1 && res.Sessions[0].GramChatID == salesChatID
	})
	require.Len(t, roleFiltered.Sessions, 1)

	coLocated := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From: wideFrom,
		To:   wideTo,
		Filters: []*gen.QueryFilter{
			{Dimension: "skill_name", Values: []string{"golang"}},
			{Dimension: "agent_name", Values: []string{""}},
		},
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 1 && res.Sessions[0].GramChatID == skillChatID
	})
	require.Len(t, coLocated.Sessions, 1)
	require.InDelta(t, 1.5, coLocated.Sessions[0].TotalCost, 1e-9)
	require.Equal(t, int64(2), coLocated.Sessions[0].MessageCount)
}

// TestListSessions_SummaryPathCursorPagination mirrors the raw-path
// pagination test over the summary route.
func TestListSessions_SummaryPathCursorPagination(t *testing.T) {
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
		insertListSessionClaudeAPIRequestLog(t, ctx, listSessionLogParams{
			projectID:    projectID,
			timestamp:    now.Add(-time.Duration(10-i) * time.Minute),
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

	wideFrom, wideTo := summaryWindow(now)

	waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
		From:   wideFrom,
		To:     wideTo,
		SortBy: "total_cost",
		Limit:  10,
	}, func(res *gen.ListSessionsResult) bool {
		return len(res.Sessions) == 3 &&
			res.Sessions[0].GramChatID == chatIDs[0] &&
			res.Sessions[1].GramChatID == chatIDs[1] &&
			res.Sessions[2].GramChatID == chatIDs[2]
	})

	var cursor *string
	for i, wantChatID := range chatIDs {
		page, err := ti.service.ListSessions(ctx, &gen.ListSessionsPayload{
			From:   wideFrom,
			To:     wideTo,
			SortBy: "total_cost",
			Limit:  1,
			Cursor: cursor,
		})
		require.NoError(t, err)
		require.Len(t, page.Sessions, 1)
		require.Equal(t, wantChatID, page.Sessions[0].GramChatID)
		if i < len(chatIDs)-1 {
			require.NotNil(t, page.NextCursor)
		} else {
			require.Nil(t, page.NextCursor)
		}
		cursor = page.NextCursor
	}
}
