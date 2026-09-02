package chat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	authchatsessions "github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/hostedinference"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const hostedInferenceDenialNote = "Hosted inference is paused for this account."

type fixedHostedInferenceEvaluator struct {
	result  killswitches.EvaluationResult
	results []killswitches.EvaluationResult
	calls   int
}

func (e *fixedHostedInferenceEvaluator) Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult {
	result := e.result
	if e.calls < len(e.results) {
		result = e.results[e.calls]
	}
	e.calls++
	return result
}

type checkpointCompletionClient struct {
	checkpoint       hostedinference.AttemptCheckpoint
	completionCalls  int
	streamCalls      int
	providerAttempts int
}

func (c *checkpointCompletionClient) check(ctx context.Context, organizationID string) error {
	if err := c.checkpoint.Check(ctx, organizationID); err != nil {
		return fmt.Errorf("check hosted inference: %w", err)
	}
	return nil
}

func (c *checkpointCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.completionCalls++
	if err := c.check(ctx, request.OrgID); err != nil {
		return nil, err
	}
	c.providerAttempts++
	return assistantTextResponse("unused"), nil
}

func (c *checkpointCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	c.streamCalls++
	if err := c.check(ctx, request.OrgID); err != nil {
		return nil, err
	}
	c.providerAttempts++
	return io.NopCloser(strings.NewReader("")), nil
}

func (c *checkpointCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	if err := c.check(ctx, request.OrgID); err != nil {
		return nil, err
	}
	return assistantTextResponse("unused"), nil
}

func (c *checkpointCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, _ string, _ []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	if err := c.check(ctx, orgID); err != nil {
		return nil, err
	}
	return [][]float32{{1}}, nil
}

func (c *checkpointCompletionClient) ResolveKey(context.Context, string, string, billing.ModelUsageSource, openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

func newChatServiceWithEvaluation(t *testing.T, result killswitches.EvaluationResult) (*chatTestInstance, *fixedHostedInferenceEvaluator, *checkpointCompletionClient) {
	t.Helper()

	gate := &checkpointCompletionClient{}
	client := chat.NewAgenticChatClient(testenv.NewLogger(t), nil, nil, nil, gate, nil)
	ti := newTestChatServiceWithCompletion(t, client)
	registry, err := mcptoolexecution.NewRegistry(ti.conn)
	require.NoError(t, err)
	evaluator := &fixedHostedInferenceEvaluator{result: result}
	gate.checkpoint, err = hostedinference.NewCheckpoint(registry, evaluator, time.Second)
	require.NoError(t, err)
	return ti, evaluator, gate
}

func newDeniedChatService(t *testing.T) (*chatTestInstance, *fixedHostedInferenceEvaluator) {
	t.Helper()
	result, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", hostedInferenceDenialNote)
	require.NoError(t, err)
	ti, evaluator, _ := newChatServiceWithEvaluation(t, result)
	return ti, evaluator
}

func requireHostedInferenceDenial(t *testing.T, err error) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeAIAccessDenied, shareable.Code)
	require.Equal(t, hostedInferenceDenialNote, shareable.Error())
}

func completionRequest(t *testing.T, ti *chatTestInstance, sessionToken, chatToken, projectSlug, query string, stream bool) *http.Request {
	t.Helper()
	ctx := initSessionCtx(t, ti)
	if projectSlug == "" {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NotNil(t, authCtx.ProjectSlug)
		projectSlug = *authCtx.ProjectSlug
	}

	body := []byte(`{"model":"openai/gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"stream":` + fmt.Sprint(stream) + `}`)
	req := httptest.NewRequest(http.MethodPost, "/chat/completions"+query, bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set(constants.ProjectHeader, projectSlug)
	if sessionToken != "" {
		req.Header.Set(constants.SessionHeader, sessionToken)
	}
	if chatToken != "" {
		req.Header.Set(constants.ChatSessionsTokenHeader, chatToken)
	}
	return req
}

func serveCompletion(t *testing.T, ti *chatTestInstance, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	oops.ErrHandle(testenv.NewLogger(t), ti.service.HandleCompletion).ServeHTTP(recorder, req)
	return recorder
}

func requireCompletionError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code oops.Code, message string) {
	t.Helper()
	require.Equal(t, status, recorder.Code, recorder.Body.String())
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.NotEqual(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	var payload struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, string(code), payload.Name)
	require.Equal(t, message, payload.Message)
}

func mintRouteChatToken(t *testing.T, ti *chatTestInstance, governed bool) (string, string) {
	t.Helper()
	ctx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.SessionID)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	selectors, err := authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		Scope:          string(authz.ScopeProjectRead),
		Selectors:      selectors,
	})
	require.NoError(t, err)
	claims := authchatsessions.ChatSessionClaims{
		OrgID: authCtx.ActiveOrganizationID, ProjectID: authCtx.ProjectID.String(),
		OrganizationSlug: authCtx.OrganizationSlug, ProjectSlug: *authCtx.ProjectSlug,
		UserID: authCtx.UserID, SessionID: authCtx.SessionID, AccountType: authCtx.AccountType,
	}
	if governed {
		claims.GramSessionActingUser = &authchatsessions.GramSessionActingUserClaim{
			OrgID: claims.OrgID, UserID: claims.UserID, SessionID: *claims.SessionID,
		}
	}
	token, _, err := ti.chatSessions.GenerateToken(ctx, claims, "https://example.com", 3600)
	require.NoError(t, err)
	_, err = ti.chatSessions.Authorize(ctx, token)
	require.NoError(t, err)
	return token, *authCtx.ProjectSlug
}

func mintRouteAPIKey(t *testing.T, ti *chatTestInstance) (string, string) {
	t.Helper()
	ctx := testenv.InitAuthContext(t, t.Context(), ti.conn, ti.sessions)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	require.NotNil(t, authCtx.ProjectSlug)

	key := "gram_local_" + uuid.NewString()
	hash, err := auth.GetAPIKeyHash(key)
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		CreatedByUserID: authCtx.UserID, Name: "route-test-key", KeyPrefix: key[:16], KeyHash: hash, Scopes: []string{"chat"},
	})
	require.NoError(t, err)
	return key, *authCtx.ProjectSlug
}

func TestHandleCompletionGovernedSessionDenialWithOptionalContextWindow(t *testing.T) {
	t.Parallel()

	for _, includeContextWindow := range []bool{false, true} {
		t.Run(fmt.Sprintf("include context window %t", includeContextWindow), func(t *testing.T) {
			t.Parallel()
			result, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", hostedInferenceDenialNote)
			require.NoError(t, err)
			ti, evaluator, gate := newChatServiceWithEvaluation(t, result)
			ctx := initSessionCtx(t, ti)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			query := ""
			if includeContextWindow {
				query = "?includeContextWindow=1"
			}
			req := completionRequest(t, ti, *authCtx.SessionID, "", "", query, true)
			recorder := serveCompletion(t, ti, req)

			requireCompletionError(t, recorder, http.StatusForbidden, oops.CodeAIAccessDenied, hostedInferenceDenialNote)
			require.Equal(t, 1, evaluator.calls)
			require.Zero(t, gate.providerAttempts)
			require.Equal(t, 1, gate.streamCalls, "the completion client's common checkpoint owns the denial")
		})
	}
}

func TestHandleCompletionEmptyStreamFallbackRechecksHostedInference(t *testing.T) {
	t.Parallel()

	allowed, err := killswitches.NewNoMatchResult(killswitches.NoMatchReasonNoPrescription)
	require.NoError(t, err)
	denied, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", hostedInferenceDenialNote)
	require.NoError(t, err)
	ti, evaluator, gate := newChatServiceWithEvaluation(t, denied)
	evaluator.results = []killswitches.EvaluationResult{allowed, denied}

	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	req := completionRequest(t, ti, *authCtx.SessionID, "", "", "", false)
	req.Header.Set("Gram-Chat-ID", uuid.NewString())
	recorder := serveCompletion(t, ti, req)

	requireCompletionError(t, recorder, http.StatusForbidden, oops.CodeAIAccessDenied, hostedInferenceDenialNote)
	require.Equal(t, 2, evaluator.calls, "the empty stream is allowed before the fallback is denied")
	require.Equal(t, 1, gate.streamCalls)
	require.Equal(t, 1, gate.completionCalls)
	require.Equal(t, 1, gate.providerAttempts, "the denied plain fallback must not reach its provider delegate")
}

func TestHandleCompletionSessionBackedChatJWTAndLegacyJWTClassification(t *testing.T) {
	t.Parallel()

	result, err := killswitches.NewMatchResult("0198a1b2-c3d4-7000-8000-0123456789ab", hostedInferenceDenialNote)
	require.NoError(t, err)

	t.Run("matching ordinary-session claim is governed", func(t *testing.T) {
		t.Parallel()
		ti, evaluator, gate := newChatServiceWithEvaluation(t, result)
		token, projectSlug := mintRouteChatToken(t, ti, true)
		req := completionRequest(t, ti, "", token, projectSlug, "", true)
		recorder := serveCompletion(t, ti, req)
		requireCompletionError(t, recorder, http.StatusForbidden, oops.CodeAIAccessDenied, hostedInferenceDenialNote)
		require.Equal(t, 1, evaluator.calls)
		require.Zero(t, gate.providerAttempts)
	})

	t.Run("old unstamped token remains unsupported", func(t *testing.T) {
		t.Parallel()
		ti, evaluator, gate := newChatServiceWithEvaluation(t, result)
		token, projectSlug := mintRouteChatToken(t, ti, false)
		req := completionRequest(t, ti, "", token, projectSlug, "", false)
		recorder := serveCompletion(t, ti, req)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Zero(t, evaluator.calls)
		require.Equal(t, 1, gate.providerAttempts)
	})

	t.Run("API key creator remains unsupported", func(t *testing.T) {
		t.Parallel()
		ti, evaluator, gate := newChatServiceWithEvaluation(t, result)
		key, projectSlug := mintRouteAPIKey(t, ti)
		req := completionRequest(t, ti, "", "", projectSlug, "", false)
		req.Header.Set(constants.APIKeyHeader, key)
		recorder := serveCompletion(t, ti, req)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Zero(t, evaluator.calls)
		require.Equal(t, 1, gate.providerAttempts)
	})
}

func TestHandleCompletionEvaluatorUnavailableIsGeneric503(t *testing.T) {
	t.Parallel()

	result, err := killswitches.NewInfrastructureFailureResult(errors.New("private evaluator detail"))
	require.NoError(t, err)
	ti, evaluator, gate := newChatServiceWithEvaluation(t, result)
	ctx := initSessionCtx(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	req := completionRequest(t, ti, *authCtx.SessionID, "", "", "", true)
	recorder := serveCompletion(t, ti, req)
	requireCompletionError(t, recorder, http.StatusServiceUnavailable, oops.CodeUnavailable, oops.CodeUnavailable.UserMessage())
	require.NotContains(t, recorder.Body.String(), "private evaluator detail")
	require.Equal(t, 1, evaluator.calls)
	require.Zero(t, gate.providerAttempts)
}

func TestServiceSummarizePreservesGovernedClassificationThroughAgenticClient(t *testing.T) {
	t.Parallel()

	ti, evaluator := newDeniedChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "external-user", "Denied summary")
	seedMessageContent(t, ctx, ti, chatID, "summarize this")

	_, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	requireHostedInferenceDenial(t, err)
	require.Equal(t, 1, evaluator.calls)
}

func TestServiceSummarizeToolCallPreservesGovernedClassificationThroughAgenticClient(t *testing.T) {
	t.Parallel()

	ti, evaluator := newDeniedChatService(t)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
	chatID := seedChat(t, ctx, ti, "", "external-user", "Denied tool summary")
	toolCallID := "call_test"
	queries := repo.New(ti.conn)
	messageID, err := queries.CreateChatMessageReturningID(ctx, repo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Role: "assistant", Content: "",
		ToolCalls: []byte(`[{"id":"call_test","type":"function","function":{"name":"lookup","arguments":{}}}]`),
	})
	require.NoError(t, err)
	_, err = queries.CreateChatMessageReturningID(ctx, repo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Role: "tool", Content: "result",
		ToolCallID: pgtype.Text{String: toolCallID, Valid: true},
	})
	require.NoError(t, err)

	_, err = ti.service.SummarizeToolCall(ctx, &gen.SummarizeToolCallPayload{
		ID: chatID.String(), MessageID: messageID.String(), ToolCallID: toolCallID,
	})
	requireHostedInferenceDenial(t, err)
	require.Equal(t, 1, evaluator.calls)
}
