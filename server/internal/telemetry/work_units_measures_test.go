package telemetry_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

// insertWorkUnitsScoreLog inserts the synthetic per-session score row the chat
// analysis publisher emits (gram_urn chat_analysis:work_units:score), carrying
// the work-units measures and the session's identity/request dimensions.
func insertWorkUnitsScoreLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string, workUnits, scoredCost float64, scoredTokens int, model, email, department string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attributes := map[string]any{
		"gen_ai.conversation.id":           chatID,
		"gen_ai.response.model":            model,
		"gram.hook.source":                 "claude-code",
		"gram.account_type":                "team",
		"gram.chat_analysis.work_units":    workUnits,
		"gram.chat_analysis.scored_cost":   scoredCost,
		"gram.chat_analysis.scored_tokens": scoredTokens,
		"user.email":                       email,
		"user.attributes.department_name":  department,
	}
	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat_analysis.work_units_score",
		nil, nil, string(attrsJSON), "{}",
		projectID, "chat_analysis:work_units:score", "gram-server")
	require.NoError(t, err)
}

// TestQuery_WorkUnitsEfficiencyMeasures proves the score rows feed the
// work-units measures without disturbing the spend measures: units, scored
// cost and scored tokens land in the scored session's dimension groups, while
// total_cost and total_chats come only from usage rows.
func TestQuery_WorkUnitsEfficiencyMeasures(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.July, 14, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)
	chatA, chatB, chatC := uuid.NewString(), uuid.NewString(), uuid.NewString()

	// Spend: Engineering $0.40 across two chats, Sales $0.50 across one.
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, chatA, 0.25, 15, 0, 0, 0, "opus", "a@x.com", "Engineering", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, chatB, 0.15, 10, 0, 0, 0, "opus", "a@x.com", "Engineering", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, ts, chatC, 0.50, 50, 0, 0, 0, "sonnet", "c@x.com", "Sales", nil, "main", "", "", "", "")

	// Only chatA is scored: 20 units, restating its $0.25 / 15-token usage.
	insertWorkUnitsScoreLog(t, ctx, projectID, ts, chatA, 20, 0.25, 15, "opus", "a@x.com", "Engineering")

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var result *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
			TopN:    10,
			SortBy:  "total_work_units",
		})
		if err != nil || res == nil || len(res.Table) != 2 {
			return false
		}
		result = res
		return true
	}, 10*time.Second, 200*time.Millisecond)

	eng := rowByGroup(t, result.Table, "Engineering")
	require.InDelta(t, 20.0, eng.Measures.TotalWorkUnits, 1e-9)
	require.InDelta(t, 0.25, eng.Measures.ScoredCost, 1e-9, "scored cost restates only the scored session's spend")
	require.Equal(t, int64(15), eng.Measures.ScoredTokens)
	require.InDelta(t, 0.40, eng.Measures.TotalCost, 1e-9, "score rows must not add cost")
	require.Equal(t, int64(2), eng.Measures.TotalChats, "score rows must not add chats")

	sales := rowByGroup(t, result.Table, "Sales")
	require.InDelta(t, 0.0, sales.Measures.TotalWorkUnits, 1e-9)
	require.InDelta(t, 0.0, sales.Measures.ScoredCost, 1e-9)
	require.InDelta(t, 0.50, sales.Measures.TotalCost, 1e-9)

	// The score row's stamped model attributes the units in a by-model cut.
	var modelResult *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("model"),
			TopN:    10,
			SortBy:  "total_work_units",
		})
		if err != nil || res == nil || len(res.Table) != 2 {
			return false
		}
		modelResult = res
		return true
	}, 10*time.Second, 200*time.Millisecond)

	opus := rowByGroup(t, modelResult.Table, "opus")
	require.InDelta(t, 20.0, opus.Measures.TotalWorkUnits, 1e-9)
	sonnet := rowByGroup(t, modelResult.Table, "sonnet")
	require.InDelta(t, 0.0, sonnet.Measures.TotalWorkUnits, 1e-9)
}
