package litellm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	srv "github.com/speakeasy-api/gram/server/gen/http/litellm/server"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	genericBlockedReason = "Request blocked by policy."
	callCacheTimeout     = time.Second
	agentTurnPrefix      = "agent-turn:v1:"
)

type HookIngester interface {
	IngestAuthenticatedDetailed(context.Context, *contextvalues.AuthContext, *hooksgen.IngestPayload, hooks.AuthenticatedIngestOptions) (*hooks.AuthenticatedIngestResult, error)
}

type authorizer interface {
	Authorize(context.Context, string, *security.APIKeyScheme) (context.Context, error)
}

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	auth      authorizer
	hooks     HookIngester
	calls     *callcache.Cache
	traces    *TraceProcessor
	metrics   *MetricProcessor
	health    *HealthProcessor
	db        *pgxpool.Pool
	telemetry telemetryrepo.CHTX
	instances *InstanceResolver
	authz     *authz.Engine
	audit     *audit.Logger
	keyPrefix string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, telemetryDB telemetryrepo.CHTX, sessionsManager *sessions.Manager, authzEngine *authz.Engine, hookIngester HookIngester, calls *callcache.Cache, traces *TraceProcessor, metrics *MetricProcessor, health *HealthProcessor, instances *InstanceResolver, auditLogger *audit.Logger, environment string) *Service {
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/litellm"),
		logger:    logger.With(attr.SlogComponent("litellm")),
		auth:      auth.New(logger, db, sessionsManager, authzEngine),
		hooks:     hookIngester,
		calls:     calls,
		traces:    traces,
		metrics:   metrics,
		health:    health,
		db:        db,
		telemetry: telemetryDB,
		instances: instances,
		authz:     authzEngine,
		audit:     auditLogger,
		keyPrefix: auth.APIKeyPrefix(environment),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	server := srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	server.Traces = service.traceHTTPHandler()
	srv.Mount(mux, server)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	if strings.TrimSpace(key) == "" && scheme.Name != constants.SessionSecurityScheme {
		var err error
		if scheme.Name == constants.ProjectSlugSecuritySchema {
			err = oops.E(oops.CodeUnauthorized, nil, "project header is required")
		} else {
			err = oops.E(oops.CodeUnauthorized, nil, "API key is required")
		}
		s.health.Record(ctx, healthSignalNone, "", err)
		return ctx, err
	}
	ctx, err := s.auth.Authorize(ctx, key, scheme)
	if err != nil {
		err = fmt.Errorf("authorize LiteLLM request: %w", err)
		s.health.Record(ctx, healthSignalNone, "", err)
		return ctx, err
	}
	return ctx, nil
}

func (s *Service) Ingest(ctx context.Context, payload *gen.IngestPayload) (result *gen.LitellmIngestResult, retErr error) {
	version := ""
	if payload != nil {
		version = conv.PtrValOr(payload.LitellmVersion, "")
	}
	defer func() {
		s.health.Record(ctx, healthSignalGuardrail, version, retErr)
	}()

	if payload == nil || payload.RequestData == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "request_data is required")
	}
	callID := strings.TrimSpace(conv.PtrValOr(payload.LitellmCallID, ""))
	if callID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "litellm_call_id is required")
	}
	switch payload.InputType {
	case "request":
		return s.ingestRequest(ctx, payload, callID)
	case "response":
		return s.ingestResponse(ctx, payload, callID)
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "input_type must be request or response")
	}
}

func (s *Service) ingestRequest(ctx context.Context, payload *gen.IngestPayload, callID string) (*gen.LitellmIngestResult, error) {
	prompt := latestUserPrompt(payload.StructuredMessages)
	if prompt == "" {
		prompt = lastText(payload.Texts)
	}
	if prompt == "" {
		return noneResult(), nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}
	authCopy := strippedAuthContext(authCtx)

	traceID := strings.TrimSpace(conv.PtrValOr(payload.LitellmTraceID, ""))
	attribution := agentAttributionFromHeaders(payload.RequestHeaders)
	sessionID := attribution.SessionID
	if sessionID == "" {
		sessionID = conv.Default(traceID, callID)
	}
	model := strings.TrimSpace(conv.PtrValOr(payload.Model, ""))
	version := strings.TrimSpace(conv.PtrValOr(payload.LitellmVersion, ""))
	email := conv.NormalizeEmail(conv.PtrValOr(payload.RequestData.UserAPIKeyUserEmail, ""))
	idempotencyKey := "litellm:" + callID + ":request"
	turnID := callID
	if attribution.TurnID != "" {
		turnID = agentTurnPrefix + attribution.TurnProvider + ":" + attribution.TurnID
	}

	hookPayload := &hooksgen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    "hook.ingest.v1",
		IdempotencyKey:   &idempotencyKey,
		Source: &hooksgen.HookIngestSource{
			Adapter:        "litellm",
			AdapterVersion: conv.PtrEmpty(version),
			RawEventName:   nil,
			Hostname:       nil,
			UserEmail:      conv.PtrEmpty(email),
		},
		Session: &hooksgen.HookIngestSession{
			ID:     &sessionID,
			TurnID: &turnID,
			Cwd:    nil,
			Model:  conv.PtrEmpty(model),
		},
		Event: &hooksgen.HookIngestEvent{
			Type:       "prompt.submitted",
			OccurredAt: nil,
		},
		Data: &hooksgen.HookIngestData{
			Prompt:                &hooksgen.HookPromptData{Text: &prompt},
			ToolCall:              nil,
			Mcp:                   nil,
			McpInventory:          nil,
			McpInventoryCollected: nil,
			Usage:                 nil,
			Message:               nil,
			Skill:                 nil,
			Notification:          nil,
			McpAttribution:        nil,
			PromptAttachments:     nil,
		},
		Raw: nil,
	}
	outcome, err := s.hooks.IngestAuthenticatedDetailed(ctx, &authCopy, hookPayload, hooks.AuthenticatedIngestOptions{
		AllowWarnAcknowledgement:     false,
		AllowSessionIdentityFallback: false,
		SourceAttributes:             sourceAttributes(payload),
		OutputToolCalls:              nil,
		OriginatingClient:            attribution.OriginatingClient,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest LiteLLM hook: %w", err)
	}
	if outcome.Result.Decision == "deny" {
		reason := strings.TrimSpace(conv.PtrValOr(outcome.Result.Message, ""))
		if reason == "" {
			reason = genericBlockedReason
		}
		return &gen.LitellmIngestResult{
			Action:              "BLOCKED",
			BlockedReason:       &reason,
			Texts:               nil,
			Images:              nil,
			Tools:               nil,
			StreamHoldbackChars: nil,
		}, nil
	}
	cacheCtx, cancel := context.WithTimeout(ctx, callCacheTimeout)
	err = s.calls.Store(cacheCtx, callcache.Record{
		ProjectID:         *authCtx.ProjectID,
		CallID:            callID,
		TraceID:           traceID,
		SessionID:         sessionID,
		UserID:            outcome.Actor.UserID,
		Email:             outcome.Actor.Email,
		OriginatingClient: attribution.OriginatingClient,
	})
	cancel()
	if err != nil {
		s.logger.WarnContext(ctx, "failed to cache LiteLLM call",
			attr.SlogError(err),
			attr.SlogProjectID(authCtx.ProjectID.String()),
			attr.SlogGenAIConversationID(sessionID),
			attr.SlogLiteLLMCallID(callID),
			attr.SlogLiteLLMTraceID(traceID),
		)
	}
	return noneResult(), nil
}

func (s *Service) ingestResponse(ctx context.Context, payload *gen.IngestPayload, callID string) (*gen.LitellmIngestResult, error) {
	text := joinedTexts(payload.Texts)
	if text == "" && len(payload.ToolCalls) == 0 {
		return noneResult(), nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}
	authCopy := strippedAuthContext(authCtx)
	traceID := strings.TrimSpace(conv.PtrValOr(payload.LitellmTraceID, ""))
	sessionID := conv.Default(traceID, callID)
	originatingClient := ""
	email := conv.NormalizeEmail(conv.PtrValOr(payload.RequestData.UserAPIKeyUserEmail, ""))

	cacheCtx, cancel := context.WithTimeout(ctx, callCacheTimeout)
	cached, err := s.calls.Get(cacheCtx, *authCtx.ProjectID, callID)
	cancel()
	if err == nil {
		sessionID = cached.SessionID
		authCopy.UserID = cached.UserID
		authCopy.Email = conv.PtrEmpty(cached.Email)
		email = cached.Email
		if cached.OriginatingClient != "" {
			originatingClient = cached.OriginatingClient
		} else {
			originatingClient = agentAttributionFromHeaders(payload.RequestHeaders).OriginatingClient
		}
	} else {
		attribution := agentAttributionFromHeaders(payload.RequestHeaders)
		sessionID = conv.Default(attribution.SessionID, sessionID)
		originatingClient = attribution.OriginatingClient
		if !callcache.IsMiss(err) {
			s.logger.WarnContext(ctx, "failed to read cached LiteLLM call",
				attr.SlogError(err),
				attr.SlogProjectID(authCtx.ProjectID.String()),
				attr.SlogLiteLLMCallID(callID),
			)
		}
	}

	model := strings.TrimSpace(conv.PtrValOr(payload.Model, ""))
	version := strings.TrimSpace(conv.PtrValOr(payload.LitellmVersion, ""))
	idempotencyKey := "litellm:" + callID + ":response"
	role := "assistant"
	hookPayload := &hooksgen.IngestPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Replayed:         nil,
		SchemaVersion:    "hook.ingest.v1",
		IdempotencyKey:   &idempotencyKey,
		Source: &hooksgen.HookIngestSource{
			Adapter:        "litellm",
			AdapterVersion: conv.PtrEmpty(version),
			RawEventName:   nil,
			Hostname:       nil,
			UserEmail:      conv.PtrEmpty(email),
		},
		Session: &hooksgen.HookIngestSession{
			ID:     &sessionID,
			TurnID: &callID,
			Cwd:    nil,
			Model:  conv.PtrEmpty(model),
		},
		Event: &hooksgen.HookIngestEvent{
			Type:       "assistant.responded",
			OccurredAt: nil,
		},
		Data: &hooksgen.HookIngestData{
			Prompt:                nil,
			ToolCall:              nil,
			Mcp:                   nil,
			McpInventory:          nil,
			McpInventoryCollected: nil,
			Usage:                 nil,
			Message:               &hooksgen.HookMessageData{Text: &text, Role: &role, DurationMs: nil},
			Skill:                 nil,
			Notification:          nil,
			McpAttribution:        nil,
			PromptAttachments:     nil,
		},
		Raw: nil,
	}
	_, err = s.hooks.IngestAuthenticatedDetailed(ctx, &authCopy, hookPayload, hooks.AuthenticatedIngestOptions{
		AllowWarnAcknowledgement:     false,
		AllowSessionIdentityFallback: false,
		SourceAttributes:             sourceAttributes(payload),
		OutputToolCalls:              payload.ToolCalls,
		OriginatingClient:            originatingClient,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest LiteLLM hook: %w", err)
	}
	return noneResult(), nil
}

func strippedAuthContext(authCtx *contextvalues.AuthContext) contextvalues.AuthContext {
	authCopy := *authCtx
	authCopy.UserID = ""
	authCopy.Email = nil
	authCopy.ExternalUserID = ""
	authCopy.OrgWidePluginHooksKey = false
	return authCopy
}

func noneResult() *gen.LitellmIngestResult {
	return &gen.LitellmIngestResult{
		Action:              "NONE",
		BlockedReason:       nil,
		Texts:               nil,
		Images:              nil,
		Tools:               nil,
		StreamHoldbackChars: nil,
	}
}

func latestUserPrompt(messages []*gen.LiteLLMStructuredMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message != nil && strings.EqualFold(strings.TrimSpace(message.Role), "user") {
			return messageText(message.Content)
		}
	}
	return ""
}

func messageText(content any) string {
	if text, ok := content.(string); ok {
		return strings.TrimSpace(text)
	}
	var blocks []any
	switch value := content.(type) {
	case []any:
		blocks = value
	case map[string]any:
		blocks = []any{value}
	default:
		return ""
	}
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		value, ok := block.(map[string]any)
		if !ok || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(value["type"])), "text") {
			continue
		}
		text, ok := value["text"].(string)
		if text = strings.TrimSpace(text); ok && text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func lastText(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	return strings.TrimSpace(texts[len(texts)-1])
}

func joinedTexts(texts []string) string {
	joined := make([]string, 0, len(texts))
	for _, text := range texts {
		if text = strings.TrimSpace(text); text != "" {
			joined = append(joined, text)
		}
	}
	return strings.Join(joined, "\n")
}

type codexTurnMetadata struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
}

type agentAttribution struct {
	SessionID         string
	OriginatingClient string
	TurnProvider      string
	TurnID            string
}

func agentAttributionFromHeaders(headers map[string]string) agentAttribution {
	normalized := make(map[string]string, len(headers))
	for key, value := range headers {
		value = strings.TrimSpace(value)
		if value != "" && !strings.EqualFold(value, "[present]") {
			normalized[strings.ToLower(strings.TrimSpace(key))] = value
		}
	}

	metadata := codexTurnMetadata{SessionID: "", TurnID: ""}
	if json.Unmarshal([]byte(normalized["x-codex-turn-metadata"]), &metadata) != nil {
		metadata = codexTurnMetadata{SessionID: "", TurnID: ""}
	}
	metadata.SessionID = strings.TrimSpace(metadata.SessionID)
	metadata.TurnID = strings.TrimSpace(metadata.TurnID)

	sessionID := ""
	for _, header := range []string{"x-gram-agent-session-id", "x-gram-session-id", "x-claude-code-session-id", "session-id", "thread-id", "x-session-id", "x-opencode-session"} {
		if normalized[header] != "" {
			sessionID = normalized[header]
			break
		}
	}
	sessionID = conv.Default(sessionID, metadata.SessionID)

	provider := strings.ToLower(normalized["x-gram-agent-provider"])
	supportedProvider := provider == "codex" || provider == "opencode"
	originatingClient := ""
	switch {
	case normalized["x-gram-agent-session-id"] != "" && supportedProvider:
		originatingClient = provider
	case normalized["x-claude-code-session-id"] != "":
		originatingClient = "claude-code"
	case normalized["session-id"] != "" || normalized["thread-id"] != "" || metadata.SessionID != "":
		originatingClient = "codex"
	case normalized["x-session-id"] != "" || normalized["x-opencode-session"] != "":
		originatingClient = "opencode"
	}

	turnProvider := ""
	turnID := ""
	switch {
	case supportedProvider && normalized["x-gram-agent-turn-id"] != "":
		turnProvider, turnID = provider, normalized["x-gram-agent-turn-id"]
	case metadata.TurnID != "":
		turnProvider, turnID = "codex", metadata.TurnID
	case normalized["x-opencode-request"] != "":
		turnProvider, turnID = "opencode", normalized["x-opencode-request"]
	}

	return agentAttribution{
		SessionID:         sessionID,
		OriginatingClient: originatingClient,
		TurnProvider:      turnProvider,
		TurnID:            turnID,
	}
}

func sourceAttributes(payload *gen.IngestPayload) map[attr.Key]any {
	attributes := make(map[attr.Key]any)
	add := func(key attr.Key, value string) {
		if value = strings.TrimSpace(value); value != "" {
			attributes[key] = value
		}
	}
	add(attr.LiteLLMCallIDKey, conv.PtrValOr(payload.LitellmCallID, ""))
	add(attr.LiteLLMTraceIDKey, conv.PtrValOr(payload.LitellmTraceID, ""))
	add(attr.LiteLLMUserIDKey, conv.PtrValOr(payload.RequestData.UserAPIKeyUserID, ""))
	add(attr.LiteLLMTeamIDKey, conv.PtrValOr(payload.RequestData.UserAPIKeyTeamID, ""))
	add(attr.LiteLLMTeamAliasKey, conv.PtrValOr(payload.RequestData.UserAPIKeyTeamAlias, ""))
	add(attr.LiteLLMEndUserIDKey, conv.PtrValOr(payload.RequestData.UserAPIKeyEndUserID, ""))
	add(attr.LiteLLMOrganizationIDKey, conv.PtrValOr(payload.RequestData.UserAPIKeyOrgID, ""))
	return attributes
}
