package litellm

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.temporal.io/sdk/testsuite"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/protobuf/encoding/protojson"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	riskanalysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	riskcelenv "github.com/speakeasy-api/gram/server/internal/risk/celenv"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type recordingScanner struct {
	result             *risk.ScanResult
	seenUserIDs        []string
	acknowledgementHit bool
	challenges         int
}

type noMCPProvenance struct{}

func (noMCPProvenance) LookupMCPProvenanceByToolCallID(_ context.Context, _ uuid.UUID, _ []string, _ time.Time) (map[string]telemetryrepo.MCPProvenance, error) {
	return map[string]telemetryrepo.MCPProvenance{}, nil
}

func requireChatMessages(t *testing.T, ctx context.Context, conn *pgxpool.Pool, params chatrepo.ListChatMessagesParams, count int) []chatrepo.ChatMessage {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		messages, err := chatrepo.New(conn).ListChatMessages(ctx, params)
		assert.NoError(collect, err)
		assert.Len(collect, messages, count)
	}, 2*time.Second, 10*time.Millisecond)
	messages, err := chatrepo.New(conn).ListChatMessages(ctx, params)
	require.NoError(t, err)
	return messages
}

func (s *recordingScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, userID string, _ string, _ message.Type, _ string) (*risk.ScanResult, error) {
	s.seenUserIDs = append(s.seenUserIDs, userID)
	return s.result, nil
}

func (s *recordingScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (s *recordingScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (s *recordingScanner) HasAcknowledgedChallenge(_ context.Context, _ uuid.UUID, _, _, _, _ string) bool {
	s.acknowledgementHit = true
	return false
}

func (s *recordingScanner) RecordPolicyChallenge(_ context.Context, _ string, _ uuid.UUID, _, _, _, _, _, _, _ string) {
	s.challenges++
}

func TestRealHooksPersistsMixedCaseMemberAndDedupesRetry(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	userID := "user_" + uuid.NewString()
	storedEmail := "Member." + uuid.NewString() + "@Example.Test"
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       storedEmail,
		DisplayName: "Test Member",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	payload := testPayload()
	callID := "mixed-case-" + uuid.NewString()
	payload.LitellmCallID = &callID
	payload.Texts = []string{"safe prompt"}
	payload.RequestData.UserAPIKeyUserEmail = new(storedEmail)
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	result, err = ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "safe prompt", messages[0].Content)
	require.Equal(t, "litellm", messages[0].Source.String)
	require.Equal(t, userID, messages[0].UserID.String)
	require.Equal(t, conv.NormalizeEmail(storedEmail), messages[0].ExternalUserID.String)
}

func TestOTLPTraceUsesGuardrailCallAttribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	userID := "user_" + uuid.NewString()
	storedEmail := "Trace." + uuid.NewString() + "@Example.Test"
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       storedEmail,
		DisplayName: "Trace Member",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	callID := "trace-call-" + uuid.NewString()
	traceID := "trace-session-" + uuid.NewString()
	payload := testPayload()
	payload.LitellmCallID = &callID
	payload.LitellmTraceID = &traceID
	payload.Texts = []string{"joined prompt"}
	payload.RequestData.UserAPIKeyUserEmail = new(storedEmail)
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(traceID),
		ProjectID: *authCtx.ProjectID,
	}, 1)

	ti.service.auth = fixedAuthorizer{authCtx: authCtx}
	start := time.Now().UTC()
	body := fmt.Appendf(nil, `{"resourceSpans":[{"scopeSpans":[{"spans":[{"traceId":"0123456789abcdef0123456789abcdef","spanId":"0123456789abcdef","name":"chat model","kind":3,"startTimeUnixNano":"%d","endTimeUnixNano":"%d","attributes":[{"key":"gen_ai.operation.name","value":{"stringValue":"chat"}},{"key":"gen_ai.request.model","value":{"stringValue":"model-group"}},{"key":"litellm.provider.model","value":{"stringValue":"openai/gpt-4o"}},{"key":"litellm.call_id","value":{"stringValue":"%s"}},{"key":"litellm.trace_id","value":{"stringValue":"otel-trace"}},{"key":"litellm.metadata.user_api_key_user_email","value":{"stringValue":"otel@example.test"}},{"key":"gen_ai.usage.input_tokens","value":{"intValue":"11"}},{"key":"gen_ai.usage.output_tokens","value":{"intValue":"7"}},{"key":"litellm.cost.total","value":{"doubleValue":0.125}}]}]}]}]}`, start.UnixNano(), start.Add(125*time.Millisecond).UnixNano(), callID)
	response := serveTraceRequest(t, mountedTraceMux(ti.service), body, "application/json", "", "fixture-key", "fixture-project")
	require.Equal(t, http.StatusAccepted, response.Code)
	require.NoError(t, ti.service.traces.Shutdown(t.Context()))

	query := telemetryrepo.New(ti.chConn)
	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
		var queryErr error
		logs, queryErr = query.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     start.Add(-time.Second).UnixNano(),
			TimeEnd:       start.Add(time.Second).UnixNano(),
			GramURNs:      []string{litellmOTLPResourceURN},
			SortOrder:     "asc",
			Cursor:        "",
			Limit:         10,
		})
		assert.NoError(collect, queryErr)
		assert.Len(collect, logs, 1)
	}, 10*time.Second, 50*time.Millisecond)

	require.Equal(t, messages[0].UserID.String, gjson.Get(logs[0].Attributes, "user.id").String())
	require.Equal(t, conv.NormalizeEmail(storedEmail), gjson.Get(logs[0].Attributes, "user.email").String())
	require.Equal(t, callID, gjson.Get(logs[0].Attributes, "gram.litellm.call_id").String())
	require.Equal(t, traceID, gjson.Get(logs[0].Attributes, "gram.litellm.trace_id").String())
	require.Equal(t, traceID, gjson.Get(logs[0].Attributes, "gen_ai.conversation.id").String())
	require.EqualValues(t, 18, gjson.Get(logs[0].Attributes, "gen_ai.usage.total_tokens").Int())
	require.Equal(t, "urn:telemetry:provider_otel:span:chat", gjson.Get(logs[0].Attributes, "gram.event.urn").String())
}

func TestOTLPMetricsPersistOnlyOperationalRows(t *testing.T) {
	t.Parallel()

	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.auth = fixedAuthorizer{authCtx: authCtx}

	now := time.Now().UTC()
	request := liteLLMMetricFixture()
	for _, resourceMetrics := range request.ResourceMetrics {
		for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
			for _, metric := range scopeMetrics.Metrics {
				for _, point := range metric.GetHistogram().DataPoints {
					point.StartTimeUnixNano = uint64(now.Add(-time.Minute).UnixNano())
					point.TimeUnixNano = uint64(now.UnixNano())
				}
			}
		}
	}
	body, err := protojson.Marshal(request)
	require.NoError(t, err)
	for range 2 {
		response := serveMetricRequest(t, ti.service.metricHTTPHandler(), body, "application/json", "", "fixture-key", "fixture-project")
		require.Equal(t, http.StatusAccepted, response.Code)
	}
	require.NoError(t, ti.service.metrics.Shutdown(t.Context()))

	query := telemetryrepo.New(ti.chConn)
	var logs []telemetryrepo.TelemetryLog
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
		var queryErr error
		logs, queryErr = query.ListTelemetryLogs(ctx, telemetryrepo.ListTelemetryLogsParams{
			GramProjectID: authCtx.ProjectID.String(),
			TimeStart:     now.Add(-time.Second).UnixNano(),
			TimeEnd:       now.Add(time.Second).UnixNano(),
			GramURNs:      []string{litellmOTLPMetricsURN},
			SortOrder:     "asc",
			Cursor:        "",
			Limit:         100,
		})
		assert.NoError(collect, queryErr)
		assert.Len(collect, logs, 2*len(litellmMetricNames))
	}, 10*time.Second, 50*time.Millisecond)

	for _, log := range logs {
		require.Equal(t, litellmMetricEventURN, gjson.Get(log.Attributes, "gram.event.urn").String())
		require.NotEmpty(t, gjson.Get(log.Attributes, "gram.metric.name").String())
		require.False(t, gjson.Get(log.Attributes, "gen_ai.usage.input_tokens").Exists())
		require.False(t, gjson.Get(log.Attributes, "gen_ai.usage.output_tokens").Exists())
		require.False(t, gjson.Get(log.Attributes, "gen_ai.usage.cost").Exists())
	}
	var aggregateRows uint64
	require.NoError(t, ti.chConn.QueryRow(ctx, "SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id = ?", authCtx.ProjectID.String()).Scan(&aggregateRows))
	require.Zero(t, aggregateRows)
	var sessionRows uint64
	require.NoError(t, ti.chConn.QueryRow(ctx, "SELECT count() FROM chat_session_summaries WHERE gram_project_id = ?", authCtx.ProjectID.String()).Scan(&sessionRows))
	require.Zero(t, sessionRows)
	require.Zero(t, ti.observer.count(*authCtx.ProjectID))
}

func TestRealHooksCapturesResponseWithCachedActorAndDedupesRetry(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	userID := "user_" + uuid.NewString()
	storedEmail := "Member." + uuid.NewString() + "@Example.Test"
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       storedEmail,
		DisplayName: "Response Member",
		PhotoUrl:    pgtype.Text{},
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)

	callID := "response-" + uuid.NewString()
	sessionID := "session-" + uuid.NewString()
	request := testPayload()
	request.LitellmCallID = &callID
	request.Texts = []string{"safe prompt"}
	request.RequestHeaders = map[string]string{"x-gram-session-id": sessionID}
	request.RequestData.UserAPIKeyUserEmail = &storedEmail
	result, err := ti.service.Ingest(ctx, request)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	toolCalls := []any{map[string]any{
		"id":   "call_1",
		"type": "function",
		"function": map[string]any{
			"name":      "lookup",
			"arguments": `{"query":"test"}`,
		},
	}}
	response := testPayload()
	response.InputType = "response"
	response.LitellmCallID = &callID
	response.Texts = []string{" first response segment ", " ", "second response segment"}
	response.ToolCalls = toolCalls
	response.StructuredMessages = []*gen.LiteLLMStructuredMessage{{Role: "user", Content: "request history"}}
	response.RequestHeaders = map[string]string{"x-gram-session-id": "conflicting-session"}
	response.RequestData.UserAPIKeyUserEmail = new("conflicting@example.test")
	result, err = ti.service.Ingest(ctx, response)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	result, err = ti.service.Ingest(ctx, response)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(sessionID),
		ProjectID: *authCtx.ProjectID,
	}, 2)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, "safe prompt", messages[0].Content)
	require.Equal(t, "assistant", messages[1].Role)
	require.Equal(t, "first response segment\nsecond response segment", messages[1].Content)
	require.Equal(t, userID, messages[0].UserID.String)
	require.Equal(t, userID, messages[1].UserID.String)
	require.Equal(t, conv.NormalizeEmail(storedEmail), messages[0].ExternalUserID.String)
	require.Equal(t, conv.NormalizeEmail(storedEmail), messages[1].ExternalUserID.String)
	expectedToolCalls, err := json.Marshal(toolCalls)
	require.NoError(t, err)
	require.JSONEq(t, string(expectedToolCalls), string(messages[1].ToolCalls))
	require.Empty(t, messages[1].ToolCallID.String)
	require.Empty(t, messages[1].ToolUrn.Kind)
	require.Empty(t, messages[1].ToolUrn.Source)
	require.Empty(t, messages[1].ToolUrn.Name)
	require.False(t, messages[1].ToolOutcome.Valid)
	for _, msg := range messages {
		require.NotEqual(t, "tool", msg.Role)
	}
	require.Eventually(t, func() bool {
		return ti.observer.count(*authCtx.ProjectID) >= 2
	}, 2*time.Second, 10*time.Millisecond)
}

func TestRealHooksCorrelatesAgentTurnsAcrossNativeHooksAndLiteLLM(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	prompt := "summarize this repository"
	ingestNativePrompt := func(adapter, sessionID, turn string) {
		t.Helper()
		var turnID *string
		if turn != "" {
			turnID = new("agent-turn:v1:" + adapter + ":" + turn)
		}
		_, err := ti.hooks.IngestAuthenticated(ctx, authCtx, &hooksgen.IngestPayload{
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			Replayed:         nil,
			SchemaVersion:    "hook.ingest.v1",
			IdempotencyKey:   new("native-" + uuid.NewString()),
			Source: &hooksgen.HookIngestSource{
				Adapter:        adapter,
				AdapterVersion: nil,
				RawEventName:   nil,
				Hostname:       nil,
				UserEmail:      nil,
			},
			Session: &hooksgen.HookIngestSession{ID: &sessionID, TurnID: turnID, Cwd: nil, Model: nil},
			Event:   &hooksgen.HookIngestEvent{Type: "prompt.submitted", OccurredAt: nil},
			Data:    &hooksgen.HookIngestData{Prompt: &hooksgen.HookPromptData{Text: &prompt}},
			Raw:     nil,
		})
		require.NoError(t, err)
	}
	ingestLiteLLMPrompt := func(sessionID string, extraHeaders map[string]string) {
		t.Helper()
		payload := testPayload()
		payload.LitellmCallID = new("call-" + uuid.NewString())
		payload.Texts = []string{prompt}
		payload.RequestHeaders = map[string]string{}
		if sessionID != "" {
			payload.RequestHeaders["x-session-id"] = sessionID
		}
		maps.Copy(payload.RequestHeaders, extraHeaders)
		result, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	}

	openCodeFirstSession := "ses_" + uuid.NewString()
	openCodeFirstMessage := "msg_" + uuid.NewString()
	openCodeHeaders := map[string]string{
		"x-gram-agent-provider": "opencode",
		"x-gram-agent-turn-id":  openCodeFirstMessage,
	}
	ingestNativePrompt("opencode", openCodeFirstSession, openCodeFirstMessage)
	ingestLiteLLMPrompt(openCodeFirstSession, openCodeHeaders)
	ingestLiteLLMPrompt(openCodeFirstSession, openCodeHeaders)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(openCodeFirstSession),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "opencode", messages[0].Source.String)

	liteLLMFirstSession := uuid.NewString()
	liteLLMFirstTurn := uuid.NewString()
	codexHeaders := map[string]string{
		"x-codex-turn-metadata": fmt.Sprintf(`{"session_id":%q,"turn_id":%q}`, liteLLMFirstSession, liteLLMFirstTurn),
	}
	ingestLiteLLMPrompt("", codexHeaders)
	ingestLiteLLMPrompt("", codexHeaders)
	ingestNativePrompt("codex", liteLLMFirstSession, liteLLMFirstTurn)

	messages = requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(liteLLMFirstSession),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "codex", messages[0].Source.String)

	repeatedSession := "ses_" + uuid.NewString()
	ingestNativePrompt("opencode", repeatedSession, "msg_"+uuid.NewString())
	ingestNativePrompt("opencode", repeatedSession, "msg_"+uuid.NewString())
	messages = requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(repeatedSession),
		ProjectID: *authCtx.ProjectID,
	}, 2)
	for _, msg := range messages {
		require.Equal(t, "user", msg.Role)
		require.Equal(t, prompt, msg.Content)
		require.Equal(t, "opencode", msg.Source.String)
	}

	claudeSession := uuid.NewString()
	ingestNativePrompt("claude", claudeSession, "prompt_"+uuid.NewString())
	require.NoError(t, ti.cache.Delete(ctx, fmt.Sprintf("session:native-prompt:v1:%s:%s", authCtx.ProjectID.String(), claudeSession)))
	ingestLiteLLMPrompt(claudeSession, nil)
	messages = requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(claudeSession),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "claude", messages[0].Source.String)
	var repairedMarker string
	require.NoError(t, ti.cache.Get(ctx, fmt.Sprintf("session:native-prompt:v1:%s:%s", authCtx.ProjectID.String(), claudeSession), &repairedMarker))
	require.Equal(t, "claude", repairedMarker)

	cursorSession := uuid.NewString()
	ingestNativePrompt("cursor", cursorSession, "generation_"+uuid.NewString())
	ingestLiteLLMPrompt(cursorSession, nil)
	messages = requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(cursorSession),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "cursor", messages[0].Source.String)

	liteLLMFirstFallbackSession := uuid.NewString()
	ingestLiteLLMPrompt(liteLLMFirstFallbackSession, nil)
	ingestNativePrompt("claude", liteLLMFirstFallbackSession, "")
	ingestNativePrompt("claude", liteLLMFirstFallbackSession, "")
	messages = requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(liteLLMFirstFallbackSession),
		ProjectID: *authCtx.ProjectID,
	}, 3)
	// Claude has no turn ID shared with LiteLLM. LiteLLM-first therefore keeps
	// both observations, and repeated identical native prompts remain distinct.
	require.Equal(t, "litellm", messages[0].Source.String)
	require.Equal(t, "claude", messages[1].Source.String)
	require.Equal(t, "claude", messages[2].Source.String)

	startedOnlySession := uuid.NewString()
	_, err := ti.hooks.IngestAuthenticated(ctx, authCtx, &hooksgen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    "hook.ingest.v1",
		IdempotencyKey:   new("native-start-" + uuid.NewString()),
		Source: &hooksgen.HookIngestSource{
			Adapter:        "claude",
			AdapterVersion: nil,
			RawEventName:   nil,
			Hostname:       nil,
			UserEmail:      nil,
		},
		Session: &hooksgen.HookIngestSession{ID: &startedOnlySession, TurnID: nil, Cwd: nil, Model: nil},
		Event:   &hooksgen.HookIngestEvent{Type: "session.started", OccurredAt: nil},
		Data:    nil,
		Raw:     nil,
	})
	require.NoError(t, err)
	ingestLiteLLMPrompt(startedOnlySession, nil)
	messages = requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(startedOnlySession),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "litellm", messages[0].Source.String)
}

func TestRealHooksConcurrentUncorrelatedPromptsPreserveNative(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	const sessions = 16
	start := make(chan struct{})
	errs := make(chan error, sessions*2)
	var wg sync.WaitGroup
	sessionIDs := make([]string, sessions)
	for i := range sessions {
		sessionID := "concurrent-uncorrelated-" + uuid.NewString()
		sessionIDs[i] = sessionID
		prompt := fmt.Sprintf("concurrent prompt %d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := ti.hooks.IngestAuthenticated(ctx, authCtx, &hooksgen.IngestPayload{
				ApikeyToken:      nil,
				ProjectSlugInput: nil,
				Replayed:         nil,
				SchemaVersion:    "hook.ingest.v1",
				IdempotencyKey:   new("native-concurrent-" + uuid.NewString()),
				Source: &hooksgen.HookIngestSource{
					Adapter:        "claude",
					AdapterVersion: nil,
					RawEventName:   nil,
					Hostname:       nil,
					UserEmail:      nil,
				},
				Session: &hooksgen.HookIngestSession{ID: &sessionID, TurnID: nil, Cwd: nil, Model: nil},
				Event:   &hooksgen.HookIngestEvent{Type: "prompt.submitted", OccurredAt: nil},
				Data:    &hooksgen.HookIngestData{Prompt: &hooksgen.HookPromptData{Text: &prompt}},
				Raw:     nil,
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			payload := testPayload()
			payload.LitellmCallID = new("litellm-concurrent-" + uuid.NewString())
			payload.Texts = []string{prompt}
			payload.RequestHeaders = map[string]string{"x-session-id": sessionID}
			_, err := ti.service.Ingest(ctx, payload)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	for _, sessionID := range sessionIDs {
		messages, err := chatrepo.New(ti.conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
			ChatID:    chat.SessionIDToChatID(sessionID),
			ProjectID: *authCtx.ProjectID,
		})
		require.NoError(t, err)
		require.NotEmpty(t, messages)
		require.LessOrEqual(t, len(messages), 2)
		nativeCount := 0
		liteLLMCount := 0
		for _, message := range messages {
			switch message.Source.String {
			case "claude":
				nativeCount++
			case "litellm":
				liteLLMCount++
			}
		}
		require.Equal(t, 1, nativeCount)
		require.LessOrEqual(t, liteLLMCount, 1)
		require.Equal(t, len(messages), nativeCount+liteLLMCount)
	}
}

func TestRealHooksLiteLLMOnlyLongSessionPersistsEveryPrompt(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	const promptCount = 128
	sessionID := "litellm-long-session-" + uuid.NewString()
	for i := range promptCount {
		payload := testPayload()
		payload.LitellmCallID = new(fmt.Sprintf("long-session-call-%d-%s", i, uuid.NewString()))
		payload.Texts = []string{fmt.Sprintf("long session prompt %d", i)}
		payload.RequestHeaders = map[string]string{"x-session-id": sessionID}
		_, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err)
	}

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(sessionID),
		ProjectID: *authCtx.ProjectID,
	}, promptCount)
	require.Equal(t, "long session prompt 0", messages[0].Content)
	require.Equal(t, "long session prompt 127", messages[len(messages)-1].Content)
}

func TestRealHooksPersistsToolCallOnlyResponse(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	callID := "tool-only-" + uuid.NewString()

	request := testPayload()
	request.LitellmCallID = &callID
	request.Texts = []string{"prompt"}
	_, err := ti.service.Ingest(ctx, request)
	require.NoError(t, err)

	response := testPayload()
	response.InputType = "response"
	response.LitellmCallID = &callID
	response.Texts = []string{"", "  "}
	response.ToolCalls = []any{map[string]any{"id": "call_only", "type": "function"}}
	_, err = ti.service.Ingest(ctx, response)
	require.NoError(t, err)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 2)
	require.Equal(t, "assistant", messages[1].Role)
	require.Empty(t, messages[1].Content)
	require.NotEmpty(t, messages[1].ToolCalls)
}

func TestRealHooksFixtureToolsNeverBecomeExecutions(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.auth = fixedAuthorizer{authCtx: authCtx}
	mux := goahttp.NewMuxer()
	Attach(mux, ti.service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	for _, raw := range readJSONLines(t, "openai-chat-tools.jsonl") {
		_, response := postContractFixture(t, server.Client(), server.URL, raw)
		require.Equal(t, map[string]any{"action": "NONE"}, response)
	}

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID("fixture-chat-session"),
		ProjectID: *authCtx.ProjectID,
	}, 2)
	require.Equal(t, []string{"user", "assistant"}, []string{messages[0].Role, messages[1].Role})
	require.Equal(t, "latest chat block one\nlatest chat block two", messages[0].Content)
	require.Equal(t, "chat answer", messages[1].Content)
	require.NotEmpty(t, messages[1].ToolCalls)
	for _, stored := range messages {
		require.NotEqual(t, "tool", stored.Role)
		require.Empty(t, stored.ToolCallID.String)
		require.Empty(t, stored.ToolUrn.Kind)
		require.Empty(t, stored.ToolUrn.Source)
		require.Empty(t, stored.ToolUrn.Name)
		require.False(t, stored.ToolOutcome.Valid)
	}
}

func TestRealHooksResponseCacheMissDoesNotUseIntegrationKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	callID := "response-miss-" + uuid.NewString()
	reportedEmail := "Missing." + uuid.NewString() + "@Example.Test"

	response := testPayload()
	response.InputType = "response"
	response.LitellmCallID = &callID
	response.Texts = []string{"uncached response"}
	response.RequestData.UserAPIKeyUserEmail = &reportedEmail
	result, err := ti.service.Ingest(ctx, response)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "assistant", messages[0].Role)
	require.False(t, messages[0].UserID.Valid)
	require.NotEqual(t, authCtx.UserID, messages[0].UserID.String)
	require.Equal(t, conv.NormalizeEmail(reportedEmail), messages[0].ExternalUserID.String)
}

func TestRealHooksPureTextResponseProducesAssistantPolicyFinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	analyzerConfig, err := riskanalysis.WithDetectionScopes(nil, []riskanalysis.DetectionScopeConfig{{
		Category:     string(categories.CategorySecrets),
		ScopeInclude: `kind == "assistant_message"`,
		ScopeExempt:  "",
	}})
	require.NoError(t, err)
	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   policyID,
		ProjectID:            *authCtx.ProjectID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		Name:                 "assistant response secrets",
		PolicyType:           nil,
		Sources:              []string{riskanalysis.SourceGitleaks},
		PresidioEntities:     nil,
		AnalyzerConfig:       analyzerConfig,
		PromptInjectionRules: nil,
		DisabledRules:        nil,
		CustomRuleIds:        nil,
		MessageTypes:         []string{message.Assistant},
		ScopeInclude:         pgtype.Text{},
		ScopeExempt:          pgtype.Text{},
		Enabled:              true,
		Action:               "flag",
		AudienceType:         "everyone",
		ShadowMcpDisposition: pgtype.Text{},
		AutoName:             false,
		UserMessage:          pgtype.Text{},
		Prompt:               pgtype.Text{},
		ModelConfig:          nil,
		Score:                pgtype.Float8{},
	})
	require.NoError(t, err)

	callID := "assistant-policy-" + uuid.NewString()
	response := testPayload()
	response.InputType = "response"
	response.LitellmCallID = &callID
	response.Texts = []string{"AccessKeyId ASIAZ2XY3WNBQR5TUVWX SecretAccessKey wJalrXUtnFEMIbKp7MDoRZfiCYqTvHgNsQ8xLcWd"}
	response.ToolCalls = nil
	result, err := ti.service.Ingest(ctx, response)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Eventually(t, func() bool {
		return ti.observer.count(*authCtx.ProjectID) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "assistant", messages[0].Role)
	require.Nil(t, messages[0].ToolCalls)

	fetch := riskanalysis.NewFetchUnanalyzed(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn)
	fetched, err := fetch.Do(ctx, riskanalysis.FetchUnanalyzedArgs{
		ProjectID:    *authCtx.ProjectID,
		IDLowerBound: uuid.Nil,
		BatchLimit:   100,
	})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{messages[0].ID}, fetched.MessageIDs)
	require.Len(t, fetched.Policies, 1)
	require.Equal(t, []string{message.Assistant}, fetched.Policies[0].MessageTypes)

	customRules, err := customruleanalyzer.NewScanner(ti.conn)
	require.NoError(t, err)
	celEngine, err := riskcelenv.New()
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskRecommendedScopes, authCtx.ActiveOrganizationID, true)
	shadowMCPClient := shadowmcp.NewClient(testenv.NewLogger(t), ti.conn, cache.NoopCache, nil)
	analyze, err := riskanalysis.NewAnalyzeBatch(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		nil,
		&riskanalysis.StubPIIScanner{},
		nil,
		shadowMCPClient,
		noMCPProvenance{},
		nil,
		flags,
		gcp.NewNoopPublisher[*riskv1.PresidioAnalysis](),
		gcp.NewNoopPublisher[*riskv1.GitleaksAnalysis](),
		gcp.NewNoopPublisher[*riskv1.PromptInjectionAnalysis](),
		gcp.NewNoopPublisher[*riskv1.PromptPolicyAnalysis](),
		gcp.NewNoopPublisher[*riskv1.CustomRulesAnalysis](),
		gcp.NewNoopPublisher[*riskv1.Finding](),
		customRules,
		celEngine,
		nil,
		nil,
	)
	require.NoError(t, err)

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.RegisterActivity(analyze.Do)
	activityResult, err := env.ExecuteActivity(analyze.Do, riskanalysis.AnalyzeBatchArgs{
		ProjectID:              *authCtx.ProjectID,
		OrganizationID:         authCtx.ActiveOrganizationID,
		RiskPolicyID:           policy.ID,
		PolicyVersion:          policy.Version,
		MessageIDs:             fetched.MessageIDs,
		ContentPartIDs:         nil,
		Sources:                policy.Sources,
		MessageTypes:           policy.MessageTypes,
		PresidioEntities:       nil,
		PresidioScoreThreshold: 0,
		CustomRuleIds:          nil,
		ApprovedEmailDomains:   nil,
		BuiltinPresetsEnabled:  false,
		DetectionScopes:        nil,
	})
	require.NoError(t, err)
	var analyzed riskanalysis.AnalyzeBatchResult
	require.NoError(t, activityResult.Get(&analyzed))
	require.Equal(t, 1, analyzed.Processed)
	require.Positive(t, analyzed.Findings)

	findings, err := riskrepo.New(ti.conn).ListRiskResultsByProjectAndPolicy(ctx, riskrepo.ListRiskResultsByProjectAndPolicyParams{
		ProjectID:              *authCtx.ProjectID,
		RiskPolicyID:           policy.ID,
		CursorMessageCreatedAt: pgtype.Timestamptz{},
		CursorID:               uuid.NullUUID{},
		PageLimit:              10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, findings)
	require.Equal(t, riskanalysis.SourceGitleaks, findings[0].Source)
	require.True(t, findings[0].Found)
	require.Equal(t, messages[0].ID, findings[0].ChatMessageID.UUID)
}

func TestRealHooksBlocksAndCapturesPolicyMessage(t *testing.T) {
	t.Parallel()
	userMessage := "This prompt is not permitted."
	scanner := &recordingScanner{
		result: &risk.ScanResult{
			Action:           "block",
			PolicyID:         uuid.NewString(),
			PolicyName:       "test block policy",
			Source:           "test",
			MessageType:      message.User,
			RuleID:           "test-rule",
			Description:      "matched test policy",
			UserMessage:      &userMessage,
			MatchedValue:     "",
			Entity:           "",
			CallFingerprint:  "fingerprint",
			DeadLetterReason: "",
		},
		seenUserIDs:        nil,
		acknowledgementHit: false,
		challenges:         0,
	}
	ctx, ti := newRealTestService(t, scanner)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	payload := testPayload()
	callID := "blocked-" + uuid.NewString()
	payload.LitellmCallID = &callID
	payload.Texts = []string{"blocked prompt"}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
	require.Equal(t, userMessage, *result.BlockedReason)
	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(callID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.Equal(t, "blocked prompt", messages[0].Content)
}

func TestRealHooksTreatsWarnAsBlockWithoutChallenge(t *testing.T) {
	t.Parallel()
	userMessage := "Warning policy blocks model traffic."
	scanner := &recordingScanner{
		result: &risk.ScanResult{
			Action:           "warn",
			PolicyID:         uuid.NewString(),
			PolicyName:       "test warn policy",
			Source:           "test",
			MessageType:      message.User,
			RuleID:           "warn-rule",
			Description:      "matched warning policy",
			UserMessage:      &userMessage,
			MatchedValue:     "sensitive match",
			Entity:           "secret",
			CallFingerprint:  "fingerprint",
			DeadLetterReason: "",
		},
		seenUserIDs:        nil,
		acknowledgementHit: false,
		challenges:         0,
	}
	ctx, ti := newRealTestService(t, scanner)
	payload := testPayload()
	payload.LitellmCallID = new("warn-" + uuid.NewString())
	payload.Texts = []string{"warning prompt"}

	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
	require.Equal(t, userMessage, *result.BlockedReason)
	require.NotContains(t, *result.BlockedReason, "ack")
	require.False(t, scanner.acknowledgementHit)
	require.Zero(t, scanner.challenges)
}

func TestRealHooksNeverUsesEndUserKeyOwnerOrCachedActor(t *testing.T) {
	t.Parallel()
	scanner := &recordingScanner{result: nil, seenUserIDs: nil, acknowledgementHit: false, challenges: 0}
	ctx, ti := newRealTestService(t, scanner)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	sessionID := "cached-identity-" + uuid.NewString()
	_, err := ti.hooks.IngestAuthenticated(ctx, authCtx, &hooksgen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    "hook.ingest.v1",
		IdempotencyKey:   new("seed-" + uuid.NewString()),
		Source: &hooksgen.HookIngestSource{
			Adapter:        "test",
			AdapterVersion: nil,
			RawEventName:   nil,
			Hostname:       nil,
			UserEmail:      nil,
		},
		Session: &hooksgen.HookIngestSession{ID: &sessionID, TurnID: nil, Cwd: nil, Model: nil},
		Event:   &hooksgen.HookIngestEvent{Type: "session.started", OccurredAt: nil},
		Data:    nil,
		Raw:     nil,
	})
	require.NoError(t, err)

	payload := testPayload()
	callID := "unattributed-" + uuid.NewString()
	payload.LitellmCallID = &callID
	payload.Texts = []string{"unattributed prompt"}
	payload.RequestHeaders = map[string]string{"x-gram-session-id": sessionID}
	payload.RequestData.UserAPIKeyUserEmail = new("missing." + uuid.NewString() + "@example.test")
	payload.RequestData.UserAPIKeyEndUserID = new(authCtx.UserID)
	payload.RequestData.UserAPIKeyUserID = new(authCtx.UserID)
	result, err := ti.service.Ingest(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)

	missingEmail := testPayload()
	missingCallID := "missing-email-" + uuid.NewString()
	missingEmail.LitellmCallID = &missingCallID
	missingEmail.Texts = []string{"missing email prompt"}
	missingEmail.RequestData.UserAPIKeyEndUserID = new(authCtx.UserID)
	result, err = ti.service.Ingest(ctx, missingEmail)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Equal(t, []string{"", ""}, scanner.seenUserIDs)

	messages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(sessionID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.False(t, messages[0].UserID.Valid)
	require.NotEqual(t, authCtx.UserID, messages[0].ExternalUserID.String)
	missingMessages := requireChatMessages(t, ctx, ti.conn, chatrepo.ListChatMessagesParams{
		ChatID:    chat.SessionIDToChatID(missingCallID),
		ProjectID: *authCtx.ProjectID,
	}, 1)
	require.False(t, missingMessages[0].UserID.Valid)
	require.False(t, missingMessages[0].ExternalUserID.Valid)
}
