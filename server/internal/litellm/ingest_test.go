package litellm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type capturedHookCall struct {
	auth    *contextvalues.AuthContext
	payload *hooksgen.IngestPayload
	options hooks.AuthenticatedIngestOptions
}

type captureIngester struct {
	result *hooks.AuthenticatedIngestResult
	err    error
	calls  []capturedHookCall
}

func (c *captureIngester) IngestAuthenticatedDetailed(_ context.Context, authCtx *contextvalues.AuthContext, payload *hooksgen.IngestPayload, options hooks.AuthenticatedIngestOptions) (*hooks.AuthenticatedIngestResult, error) {
	c.calls = append(c.calls, capturedHookCall{auth: authCtx, payload: payload, options: options})
	return c.result, c.err
}

type memoryCache struct {
	cache.Cache
	entries map[string][]byte
	ttls    []time.Duration
}

func newMemoryCache() *memoryCache {
	return &memoryCache{Cache: cache.NoopCache, entries: map[string][]byte{}, ttls: nil}
}

func (c *memoryCache) Get(_ context.Context, key string, value any) error {
	raw, ok := c.entries[key]
	if !ok {
		return redisCache.ErrCacheMiss
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("unmarshal cache value: %w", err)
	}
	return nil
}

func (c *memoryCache) Set(_ context.Context, key string, value any, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache value: %w", err)
	}
	c.entries[key] = raw
	c.ttls = append(c.ttls, ttl)
	return nil
}

func allowResult(actor hooks.ResolvedActor) *hooks.AuthenticatedIngestResult {
	return &hooks.AuthenticatedIngestResult{
		Result: &hooksgen.IngestHookResult{Decision: "allow", Reason: nil, Message: nil, Effects: nil},
		Actor:  actor,
	}
}

type fixedAuthorizer struct {
	authCtx *contextvalues.AuthContext
}

func (f fixedAuthorizer) Authorize(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	return contextvalues.SetAuthContext(ctx, f.authCtx), nil
}

func testPayload() *gen.IngestPayload {
	return &gen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		InputType:        "request",
		RequestData: &gen.LiteLLMRequestData{
			UserAPIKeyHash:      nil,
			UserAPIKeyAlias:     nil,
			UserAPIKeyUserID:    nil,
			UserAPIKeyUserEmail: nil,
			UserAPIKeyTeamID:    nil,
			UserAPIKeyTeamAlias: nil,
			UserAPIKeyEndUserID: nil,
			UserAPIKeyOrgID:     nil,
		},
		Texts:                            nil,
		Images:                           nil,
		Tools:                            nil,
		ToolCalls:                        nil,
		StructuredMessages:               nil,
		RequestHeaders:                   nil,
		LitellmCallID:                    new("call-1"),
		LitellmTraceID:                   nil,
		LitellmVersion:                   nil,
		Model:                            nil,
		AdditionalProviderSpecificParams: nil,
	}
}

func testAuthContext() *contextvalues.AuthContext {
	projectID := uuid.New()
	return &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_test",
		UserID:                "integration-key-owner",
		ExternalUserID:        "untrusted-existing-external-user",
		APIKeyID:              "key_test",
		APIKeyName:            "integration-key",
		OrgWidePluginHooksKey: true,
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "org-test",
		Email:                 new("owner@example.test"),
		AccountType:           "user",
		HasActiveSubscription: true,
		Whitelisted:           false,
		ProjectSlug:           new("project-test"),
		APIKeyScopes:          []string{"hooks"},
		IsAdmin:               false,
	}
}

func unitService(t *testing.T, ingester HookIngester, authCtx *contextvalues.AuthContext) *Service {
	t.Helper()
	tracerProvider := testenv.NewTracerProvider(t)
	return &Service{
		tracer:    tracerProvider.Tracer("test"),
		logger:    testenv.NewLogger(t),
		auth:      fixedAuthorizer{authCtx: authCtx},
		hooks:     ingester,
		calls:     callcache.New(newMemoryCache()),
		traces:    newTraceProcessor(testenv.NewLogger(t), testenv.NewMeterProvider(t), telemetry.NewStub(testenv.NewLogger(t)).LogBulk, traceProcessorWorkers, traceProcessorQueueSize),
		metrics:   nil,
		health:    newDisabledHealthProcessor(t),
		db:        nil,
		telemetry: nil,
		instances: NewInstanceResolver(testenv.NewLogger(t), nil),
		authz:     nil,
		audit:     nil,
		keyPrefix: "",
	}
}

func TestIngestTranslatesLatestStructuredUserMessage(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "resolved-user", Email: "member@example.test"}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)
	payload := testPayload()
	payload.StructuredMessages = []*gen.LiteLLMStructuredMessage{
		{Role: "user", Content: "older prompt"},
		{Role: "assistant", Content: "assistant response"},
		{Role: "USER", Content: []any{
			map[string]any{"type": "text", "text": "first block"},
			map[string]any{"type": "image_url", "image_url": "ignored"},
			map[string]any{"type": "text", "text": "second block"},
		}},
	}
	payload.Texts = []string{"fallback must not win"}
	payload.Images = []string{"image"}
	payload.Tools = []any{map[string]any{"name": "definition-only"}}
	payload.ToolCalls = []any{map[string]any{"name": "historical-only"}}
	payload.RequestHeaders = map[string]string{"X-Gram-Session-ID": "session-from-header"}
	payload.LitellmTraceID = new("trace-1")
	payload.LitellmVersion = new("1.94.0")
	payload.Model = new("model-1")
	payload.RequestData.UserAPIKeyUserEmail = new("  Member@Example.Test ")
	payload.RequestData.UserAPIKeyUserID = new("virtual-key-user")
	payload.RequestData.UserAPIKeyTeamID = new("team-id")
	payload.RequestData.UserAPIKeyTeamAlias = new("team-alias")
	payload.RequestData.UserAPIKeyEndUserID = new("caller-controlled-end-user")
	payload.RequestData.UserAPIKeyOrgID = new("external-org")

	result, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Len(t, ingester.calls, 1)
	call := ingester.calls[0]
	require.Equal(t, "first block\nsecond block", *call.payload.Data.Prompt.Text)
	require.Nil(t, call.payload.Data.ToolCall)
	require.Nil(t, call.payload.Raw)
	require.Equal(t, "litellm", call.payload.Source.Adapter)
	require.Equal(t, "1.94.0", *call.payload.Source.AdapterVersion)
	require.Equal(t, "member@example.test", *call.payload.Source.UserEmail)
	require.Equal(t, "session-from-header", *call.payload.Session.ID)
	require.Equal(t, "call-1", *call.payload.Session.TurnID)
	require.Equal(t, "model-1", *call.payload.Session.Model)
	require.Equal(t, "litellm:call-1:request", *call.payload.IdempotencyKey)
	require.Empty(t, call.auth.UserID)
	require.Nil(t, call.auth.Email)
	require.Empty(t, call.auth.ExternalUserID)
	require.False(t, call.auth.OrgWidePluginHooksKey)
	require.Equal(t, authCtx.ActiveOrganizationID, call.auth.ActiveOrganizationID)
	require.Equal(t, authCtx.APIKeyID, call.auth.APIKeyID)
	require.False(t, call.options.AllowWarnAcknowledgement)
	require.False(t, call.options.AllowSessionIdentityFallback)
	require.Nil(t, call.options.OutputToolCalls)
	require.Equal(t, "call-1", call.options.SourceAttributes[attr.LiteLLMCallIDKey])
	require.Equal(t, "trace-1", call.options.SourceAttributes[attr.LiteLLMTraceIDKey])
	require.Equal(t, "virtual-key-user", call.options.SourceAttributes[attr.LiteLLMUserIDKey])
	require.Equal(t, "team-id", call.options.SourceAttributes[attr.LiteLLMTeamIDKey])
	require.Equal(t, "team-alias", call.options.SourceAttributes[attr.LiteLLMTeamAliasKey])
	require.Equal(t, "caller-controlled-end-user", call.options.SourceAttributes[attr.LiteLLMEndUserIDKey])
	require.Equal(t, "external-org", call.options.SourceAttributes[attr.LiteLLMOrganizationIDKey])
	require.Equal(t, "integration-key-owner", authCtx.UserID)
	cached, err := service.calls.Get(t.Context(), *authCtx.ProjectID, "call-1")
	require.NoError(t, err)
	require.Equal(t, callcache.Record{
		ProjectID: *authCtx.ProjectID,
		CallID:    "call-1",
		TraceID:   "trace-1",
		SessionID: "session-from-header",
		UserID:    "resolved-user",
		Email:     "member@example.test",
	}, cached)
}

func TestIngestUsesTextsAndSessionFallbacks(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "", Email: ""}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)

	headerPayload := testPayload()
	headerPayload.StructuredMessages = []*gen.LiteLLMStructuredMessage{
		{Role: "user", Content: "older structured prompt"},
		{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": "no text"}}},
	}
	headerPayload.Texts = []string{"older", "  latest text  "}
	headerPayload.RequestHeaders = map[string]string{"x-GrAm-SeSsIoN-Id": "header-session"}
	headerPayload.LitellmTraceID = new("trace-session")
	_, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), headerPayload)
	require.NoError(t, err)

	tracePayload := testPayload()
	tracePayload.LitellmCallID = new("call-2")
	tracePayload.Texts = []string{"trace prompt"}
	tracePayload.RequestHeaders = map[string]string{"x-gram-session-id": "[present]"}
	tracePayload.LitellmTraceID = new("trace-session")
	_, err = service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), tracePayload)
	require.NoError(t, err)

	callPayload := testPayload()
	callPayload.LitellmCallID = new("call-3")
	callPayload.Texts = []string{"older", "call prompt"}
	_, err = service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), callPayload)
	require.NoError(t, err)

	require.Len(t, ingester.calls, 3)
	require.Equal(t, "latest text", *ingester.calls[0].payload.Data.Prompt.Text)
	require.Equal(t, "header-session", *ingester.calls[0].payload.Session.ID)
	require.Equal(t, "trace-session", *ingester.calls[1].payload.Session.ID)
	require.Equal(t, "call-3", *ingester.calls[2].payload.Session.ID)
}

func TestSessionHeaderUsesNativeClientHeadersInPrecedenceOrder(t *testing.T) {
	t.Parallel()
	headers := map[string]string{
		"X-Session-ID":             "opencode-session",
		"Thread-ID":                "codex-thread",
		"Session-ID":               "codex-session",
		"X-Claude-Code-Session-ID": "claude-session",
		"X-Gram-Session-ID":        "gram-session",
	}

	require.Equal(t, "gram-session", sessionHeader(headers))
	delete(headers, "X-Gram-Session-ID")
	require.Equal(t, "claude-session", sessionHeader(headers))
	delete(headers, "X-Claude-Code-Session-ID")
	require.Equal(t, "codex-session", sessionHeader(headers))
	delete(headers, "Session-ID")
	require.Equal(t, "codex-thread", sessionHeader(headers))
	delete(headers, "Thread-ID")
	require.Equal(t, "opencode-session", sessionHeader(headers))

	headers["X-Gram-Session-ID"] = "[present]"
	require.Equal(t, "opencode-session", sessionHeader(headers))
}

func TestIngestResponseUsesCachedActorAndSession(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "cached-user", Email: "cached@example.test"}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)
	require.NoError(t, service.calls.Store(t.Context(), callcache.Record{
		ProjectID: *authCtx.ProjectID,
		CallID:    "call-1",
		TraceID:   "request-trace",
		SessionID: "request-session",
		UserID:    "cached-user",
		Email:     "cached@example.test",
	}))

	payload := testPayload()
	payload.InputType = "response"
	payload.Texts = []string{" first segment ", "", "  second segment  "}
	payload.StructuredMessages = []*gen.LiteLLMStructuredMessage{{Role: "user", Content: "request history must be ignored"}}
	payload.ToolCalls = []any{map[string]any{"id": "tool-1", "type": "function"}}
	payload.RequestHeaders = map[string]string{"x-gram-session-id": "conflicting-response-session"}
	payload.LitellmTraceID = new("conflicting-response-trace")
	payload.RequestData.UserAPIKeyUserEmail = new("conflicting@example.test")

	result, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Len(t, ingester.calls, 1)
	call := ingester.calls[0]
	require.Equal(t, "assistant.responded", call.payload.Event.Type)
	require.Equal(t, "first segment\nsecond segment", *call.payload.Data.Message.Text)
	require.Equal(t, "assistant", *call.payload.Data.Message.Role)
	require.Equal(t, "request-session", *call.payload.Session.ID)
	require.Equal(t, "call-1", *call.payload.Session.TurnID)
	require.Equal(t, "litellm:call-1:response", *call.payload.IdempotencyKey)
	require.Equal(t, "cached-user", call.auth.UserID)
	require.Equal(t, "cached@example.test", *call.auth.Email)
	require.False(t, call.auth.OrgWidePluginHooksKey)
	require.Equal(t, "cached@example.test", *call.payload.Source.UserEmail)
	require.Equal(t, payload.ToolCalls, call.options.OutputToolCalls)
}

func TestIngestResponseCacheMissUsesTrustedEmailWithoutKeyOwner(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "", Email: "member@example.test"}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)
	payload := testPayload()
	payload.InputType = "response"
	payload.Texts = []string{"response"}
	payload.LitellmCallID = new("response-miss")
	payload.LitellmTraceID = new("response-trace")
	payload.RequestData.UserAPIKeyUserEmail = new(" Member@Example.Test ")

	result, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Len(t, ingester.calls, 1)
	call := ingester.calls[0]
	require.Empty(t, call.auth.UserID)
	require.Nil(t, call.auth.Email)
	require.Empty(t, call.auth.ExternalUserID)
	require.False(t, call.auth.OrgWidePluginHooksKey)
	require.Equal(t, "member@example.test", *call.payload.Source.UserEmail)
	require.Equal(t, "response-trace", *call.payload.Session.ID)
}

func TestIngestValidatesAndSkipsEmptyPrompt(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "", Email: ""}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)

	invalidPayload := testPayload()
	invalidPayload.InputType = "invalid"
	result, err := service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), invalidPayload)
	require.Error(t, err)
	require.Nil(t, result)

	missingCall := testPayload()
	missingCall.LitellmCallID = nil
	result, err = service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), missingCall)
	require.Error(t, err)
	require.Nil(t, result)

	empty := testPayload()
	empty.Texts = []string{"must not scan", "  "}
	result, err = service.Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), empty)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("NONE"), result.Action)
	require.Empty(t, ingester.calls)
}

func TestIngestMapsDenyAndErrors(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	message := "Configured policy message"
	payload := testPayload()
	payload.Texts = []string{"prompt"}

	deny := &captureIngester{result: &hooks.AuthenticatedIngestResult{
		Result: &hooksgen.IngestHookResult{Decision: "deny", Reason: nil, Message: &message, Effects: nil},
		Actor:  hooks.ResolvedActor{UserID: "", Email: ""},
	}, err: nil, calls: nil}
	result, err := unitService(t, deny, authCtx).Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMGuardrailAction("BLOCKED"), result.Action)
	require.Equal(t, message, *result.BlockedReason)

	deny.result.Result.Message = nil
	result, err = unitService(t, deny, authCtx).Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.NoError(t, err)
	require.Equal(t, genericBlockedReason, *result.BlockedReason)

	wantErr := errors.New("ingest failed")
	failing := &captureIngester{result: nil, err: wantErr, calls: nil}
	result, err = unitService(t, failing, authCtx).Ingest(contextvalues.SetAuthContext(t.Context(), authCtx), payload)
	require.ErrorIs(t, err, wantErr)
	require.Nil(t, result)
}

func TestHTTPRouteRequiresKeyAndExplicitProject(t *testing.T) {
	t.Parallel()
	authCtx := testAuthContext()
	ingester := &captureIngester{result: allowResult(hooks.ResolvedActor{UserID: "", Email: ""}), err: nil, calls: nil}
	service := unitService(t, ingester, authCtx)
	mux := goahttp.NewMuxer()
	Attach(mux, service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	body, err := json.Marshal(map[string]any{
		"input_type": "request",
		"request_data": map[string]any{
			"user_api_key_user_email":  "Wire.Member@Example.Test",
			"user_api_key_user_id":     "wire-user-id",
			"user_api_key_team_id":     "wire-team-id",
			"user_api_key_team_alias":  "wire-team-alias",
			"user_api_key_end_user_id": "wire-end-user-id",
		},
		"structured_messages": []map[string]any{
			{"role": "assistant"},
			{"role": "user", "content": "http prompt"},
		},
		"litellm_call_id": "http-call",
	})
	require.NoError(t, err)

	request := func(key, project string) *http.Response {
		req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/rpc/litellm.ingest/beta/litellm_basic_guardrail_api", bytes.NewReader(body))
		require.NoError(t, requestErr)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Gram-Key", key)
		}
		if project != "" {
			req.Header.Set("Gram-Project", project)
		}
		response, requestErr := http.DefaultClient.Do(req)
		require.NoError(t, requestErr)
		return response
	}

	missingKey := request("", "project-test")
	_, err = io.ReadAll(missingKey.Body)
	require.NoError(t, err)
	require.NoError(t, missingKey.Body.Close())
	require.Equal(t, http.StatusUnauthorized, missingKey.StatusCode)

	missingProject := request("test-key", "")
	_, err = io.ReadAll(missingProject.Body)
	require.NoError(t, err)
	require.NoError(t, missingProject.Body.Close())
	require.Equal(t, http.StatusUnauthorized, missingProject.StatusCode)

	valid := request("test-key", "project-test")
	responseBody, err := io.ReadAll(valid.Body)
	require.NoError(t, err)
	require.NoError(t, valid.Body.Close())
	require.Equal(t, http.StatusOK, valid.StatusCode)
	require.Contains(t, string(responseBody), `"action":"NONE"`)
	require.Len(t, ingester.calls, 1)
	call := ingester.calls[0]
	require.Equal(t, "http prompt", *call.payload.Data.Prompt.Text)
	require.Equal(t, "wire.member@example.test", *call.payload.Source.UserEmail)
	require.Equal(t, "wire-user-id", call.options.SourceAttributes[attr.LiteLLMUserIDKey])
	require.Equal(t, "wire-team-id", call.options.SourceAttributes[attr.LiteLLMTeamIDKey])
	require.Equal(t, "wire-team-alias", call.options.SourceAttributes[attr.LiteLLMTeamAliasKey])
	require.Equal(t, "wire-end-user-id", call.options.SourceAttributes[attr.LiteLLMEndUserIDKey])
}
