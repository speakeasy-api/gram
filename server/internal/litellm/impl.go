package litellm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	hooksgen "github.com/speakeasy-api/gram/server/gen/hooks"
	srv "github.com/speakeasy-api/gram/server/gen/http/litellm/server"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const genericBlockedReason = "Request blocked by policy."

type HookIngester interface {
	IngestAuthenticatedWithOptions(context.Context, *contextvalues.AuthContext, *hooksgen.IngestPayload, hooks.AuthenticatedIngestOptions) (*hooksgen.IngestHookResult, error)
}

type authorizer interface {
	Authorize(context.Context, string, *security.APIKeyScheme) (context.Context, error)
}

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	auth   authorizer
	hooks  HookIngester
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionsManager *sessions.Manager, authzEngine *authz.Engine, hookIngester HookIngester) *Service {
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/litellm"),
		logger: logger.With(attr.SlogComponent("litellm")),
		auth:   auth.New(logger, db, sessionsManager, authzEngine),
		hooks:  hookIngester,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, scheme *security.APIKeyScheme) (context.Context, error) {
	if strings.TrimSpace(key) == "" {
		if scheme.Name == constants.ProjectSlugSecuritySchema {
			return ctx, oops.E(oops.CodeUnauthorized, nil, "project header is required")
		}
		return ctx, oops.E(oops.CodeUnauthorized, nil, "API key is required")
	}
	ctx, err := s.auth.Authorize(ctx, key, scheme)
	if err != nil {
		return ctx, fmt.Errorf("authorize LiteLLM request: %w", err)
	}
	return ctx, nil
}

func (s *Service) Ingest(ctx context.Context, payload *gen.IngestPayload) (*gen.LitellmIngestResult, error) {
	if payload == nil || payload.RequestData == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "request_data is required")
	}
	if payload.InputType != "request" {
		return nil, oops.E(oops.CodeBadRequest, nil, "only request input_type is supported")
	}
	callID := strings.TrimSpace(conv.PtrValOr(payload.LitellmCallID, ""))
	if callID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "litellm_call_id is required")
	}
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
	authCopy := *authCtx
	authCopy.UserID = ""
	authCopy.Email = nil
	authCopy.ExternalUserID = ""
	authCopy.OrgWidePluginHooksKey = false

	traceID := strings.TrimSpace(conv.PtrValOr(payload.LitellmTraceID, ""))
	sessionID := sessionHeader(payload.RequestHeaders)
	if sessionID == "" {
		sessionID = conv.Default(traceID, callID)
	}
	model := strings.TrimSpace(conv.PtrValOr(payload.Model, ""))
	version := strings.TrimSpace(conv.PtrValOr(payload.LitellmVersion, ""))
	email := conv.NormalizeEmail(conv.PtrValOr(payload.RequestData.UserAPIKeyUserEmail, ""))
	idempotencyKey := "litellm:" + callID + ":request"

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
			Type:       "prompt.submitted",
			OccurredAt: nil,
		},
		Data: &hooksgen.HookIngestData{
			Prompt:            &hooksgen.HookPromptData{Text: &prompt},
			ToolCall:          nil,
			Mcp:               nil,
			McpInventory:      nil,
			Usage:             nil,
			Message:           nil,
			Skill:             nil,
			Notification:      nil,
			McpAttribution:    nil,
			PromptAttachments: nil,
		},
		Raw: nil,
	}
	result, err := s.hooks.IngestAuthenticatedWithOptions(ctx, &authCopy, hookPayload, hooks.AuthenticatedIngestOptions{
		AllowWarnAcknowledgement:     false,
		AllowSessionIdentityFallback: false,
		SourceAttributes:             sourceAttributes(payload),
	})
	if err != nil {
		return nil, fmt.Errorf("ingest LiteLLM hook: %w", err)
	}
	if result.Decision == "deny" {
		reason := strings.TrimSpace(conv.PtrValOr(result.Message, ""))
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
	return noneResult(), nil
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

func sessionHeader(headers map[string]string) string {
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), "x-gram-session-id") {
			value = strings.TrimSpace(value)
			if value != "" && !strings.EqualFold(value, "[present]") {
				return value
			}
		}
	}
	return ""
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
