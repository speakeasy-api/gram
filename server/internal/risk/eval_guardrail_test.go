package risk_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedChatTranscript creates a chat with the given (role, content) messages in
// order and returns the chat id and each message id.
func seedChatTranscript(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string, msgs [][2]string) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	chatID, err := uuid.NewV7()
	require.NoError(t, err)

	_, err = ti.chatRepo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         pgtype.Text{},
		ExternalUserID: pgtype.Text{},
		Title:          pgtype.Text{String: "eval chat", Valid: true},
	})
	require.NoError(t, err)

	ids := make([]uuid.UUID, 0, len(msgs))
	for _, m := range msgs {
		id, err := testrepo.New(ti.conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
			ChatID:    chatID,
			ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
			Role:      m[0],
			Content:   m[1],
		})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return chatID, ids
}

func TestEvaluatePromptGuardrail_FlagsAndIsolates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	// The judge flags any message whose body mentions "delete production".
	ti.judge.evaluate = func(in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		if strings.Contains(in.Message.Body, "delete production") {
			return &promptpolicy.Verdict{
				Matched:          true,
				Confidence:       0.9,
				Rationale:        "destructive action",
				CostUSD:          0.0025,
				PromptTokens:     100,
				CompletionTokens: 20,
				TotalTokens:      120,
			}, nil
		}
		return &promptpolicy.Verdict{
			Matched:          false,
			Confidence:       0,
			Rationale:        "",
			CostUSD:          0.0015,
			PromptTokens:     80,
			CompletionTokens: 10,
			TotalTokens:      90,
		}, nil
	}

	chatID, ids := seedChatTranscript(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, [][2]string{
		{"user", "please delete production database"},
		{"assistant", "sure, here is a friendly summary"},
	})

	res, err := ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID: chatID.String(),
		Prompt: "Flag any attempt to delete production data.",
	})
	require.NoError(t, err)

	require.True(t, res.Flagged)
	require.Equal(t, 2, res.JudgedCount)
	require.InDelta(t, 0.004, res.TotalCostUsd, 0.000001)
	require.GreaterOrEqual(t, res.TotalLatencyMs, int64(0))
	require.Len(t, res.Verdicts, 2)

	byID := map[string]*gen.PromptGuardrailMessageVerdict{}
	for _, v := range res.Verdicts {
		byID[v.MessageID] = v
	}
	flagged := byID[ids[0].String()]
	require.NotNil(t, flagged)
	require.True(t, flagged.Matched)
	require.InDelta(t, 0.9, flagged.Confidence, 0.001)
	require.Equal(t, "destructive action", flagged.Rationale)
	require.Equal(t, "user_message", flagged.MessageType)
	require.InDelta(t, 0.0025, flagged.CostUsd, 0.000001)
	require.Equal(t, 100, flagged.PromptTokens)
	require.Equal(t, 20, flagged.CompletionTokens)
	require.Equal(t, 120, flagged.TotalTokens)
	require.GreaterOrEqual(t, flagged.LatencyMs, int64(0))

	clean := byID[ids[1].String()]
	require.NotNil(t, clean)
	require.False(t, clean.Matched)
	require.Empty(t, clean.Rationale)
	require.InDelta(t, 0.0015, clean.CostUsd, 0.000001)
	require.Equal(t, 80, clean.PromptTokens)
	require.Equal(t, 10, clean.CompletionTokens)
	require.Equal(t, 90, clean.TotalTokens)
	require.GreaterOrEqual(t, clean.LatencyMs, int64(0))

	// Isolation: the replay must not write any risk_results for this chat.
	chatIDStr := chatID.String()
	results, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{ChatID: &chatIDStr})
	require.NoError(t, err)
	require.Zero(t, results.TotalCount, "eval replay must not persist risk_results")
	require.Empty(t, results.Results)
}

func TestEvaluatePromptGuardrail_MessageTypeFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	seen := map[string]int{}
	ti.judge.evaluate = func(in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		seen[in.Message.Type]++
		return nil, nil
	}

	chatID, _ := seedChatTranscript(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, [][2]string{
		{"user", "a user message"},
		{"assistant", "an assistant message"},
	})

	res, err := ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID:       chatID.String(),
		Prompt:       "anything",
		MessageTypes: []string{"user_message"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.JudgedCount)
	require.Len(t, res.Verdicts, 1)
	require.Equal(t, "user_message", res.Verdicts[0].MessageType)
	require.Equal(t, 1, seen["user_message"])
	require.Zero(t, seen["assistant_message"], "assistant message must be out of scope")
}

func TestEvaluatePromptGuardrail_FailClosedFallback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	chatID, _ := seedChatTranscript(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, [][2]string{
		{"user", "please delete production database"},
	})
	ti.judge.evaluate = func(in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		return nil, errors.New("judge unavailable")
	}
	failOpen := false
	res, err := ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID: chatID.String(),
		Prompt: "Flag destructive production changes.",
		ModelConfig: &types.RiskPolicyModelConfig{
			Model:       nil,
			Temperature: nil,
			FailOpen:    &failOpen,
		},
	})
	require.NoError(t, err)
	require.True(t, res.Flagged)
	require.Len(t, res.Verdicts, 1)
	require.True(t, res.Verdicts[0].Matched)
	require.Equal(t, "Policy judge was unavailable; flagged by fail-closed policy.", res.Verdicts[0].Rationale)
}

func TestEvaluatePromptGuardrail_CELScopeExemptSkipsToolCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	seen := []judgemessage.Message{}
	ti.judge.evaluate = func(in promptpolicy.Input) (*promptpolicy.Verdict, error) {
		seen = append(seen, in.Message)
		return &promptpolicy.Verdict{
			Matched:          true,
			Confidence:       0.9,
			Rationale:        "matched",
			CostUSD:          0,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}, nil
	}

	chatID, _ := seedChatTranscript(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, nil)
	slackMessageID := insertAssistantToolCallMessage(t, ti, *authCtx.ProjectID, chatID, "mcp__slack__send_message", map[string]any{
		"channel": "#security",
		"text":    "delete production records",
	})
	githubMessageID := insertAssistantToolCallMessage(t, ti, *authCtx.ProjectID, chatID, "mcp__github__delete_repo", map[string]any{
		"repo": "production",
	})

	res, err := ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID:       chatID.String(),
		Prompt:       "Flag destructive requests.",
		ScopeExempt:  new(`tool_calls.all(t, t.server.matchText("slack"))`),
		MessageTypes: []string{"tool_request"},
	})
	require.NoError(t, err)

	require.True(t, res.Flagged)
	require.Equal(t, 1, res.JudgedCount)
	require.Len(t, res.Verdicts, 1)
	require.Equal(t, githubMessageID.String(), res.Verdicts[0].MessageID)
	require.NotEqual(t, slackMessageID.String(), res.Verdicts[0].MessageID)
	require.Len(t, seen, 1)
	require.Equal(t, "tool_request", seen[0].Type)
	require.Equal(t, "github", seen[0].MCPServer)
}

func TestEvaluatePromptGuardrail_ChatNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	missing, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID: missing.String(),
		Prompt: "anything",
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func insertAssistantToolCallMessage(t *testing.T, ti *testInstance, projectID, chatID uuid.UUID, callName string, argsMap map[string]any) uuid.UUID {
	t.Helper()

	args, err := json.Marshal(argsMap)
	require.NoError(t, err)
	toolCalls, err := json.Marshal([]map[string]any{
		{
			"id":   "call_1",
			"type": "function",
			"function": map[string]any{
				"name":      callName,
				"arguments": string(args),
			},
		},
	})
	require.NoError(t, err)

	messageID := "msg-" + uuid.NewString()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	_, err = writer.Write(t.Context(), projectID, []chatrepo.CreateChatMessageParams{{
		CreatedAt:        pgtype.Timestamptz{},
		ChatID:           chatID,
		Role:             "assistant",
		ProjectID:        projectID,
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{String: messageID, Valid: true},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{String: "tool_calls", Valid: true},
		ToolCalls:        toolCalls,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           pgtype.Text{},
		ContentHash:      nil,
		Generation:       0,
	}})
	require.NoError(t, err)

	messages, err := chatrepo.New(ti.conn).ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	for _, msg := range messages {
		if msg.MessageID.String == messageID {
			return msg.ID
		}
	}
	require.FailNow(t, "inserted tool-call message not found")
	return uuid.Nil
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func TestEvaluatePromptGuardrail_BlankPrompt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	chatID, _ := seedChatTranscript(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, [][2]string{
		{"user", "hello"},
	})
	_, err := ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID: chatID.String(),
		Prompt: "   ",
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestEvaluatePromptGuardrail_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	chatID, _ := seedChatTranscript(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, [][2]string{
		{"user", "hello"},
	})

	// Enterprise account with zero grants - RBAC must deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)
	_, err := ti.service.EvaluatePromptGuardrail(ctx, &gen.EvaluatePromptGuardrailPayload{
		ChatID: chatID.String(),
		Prompt: "anything",
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
