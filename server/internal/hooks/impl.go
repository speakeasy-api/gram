package hooks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/telemetry"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	srv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
)

type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	db               *pgxpool.Pool
	telemetryService *telemetry.Service
	auth             *auth.Auth
}

// HookSpecificOutput is the structure for hook-specific output in responses
type HookSpecificOutput struct {
	HookEventName            *string `json:"hookEventName,omitempty"`
	AdditionalContext        *string `json:"additionalContext,omitempty"`
	PermissionDecision       *string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason *string `json:"permissionDecisionReason,omitempty"`
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, tracerProvider trace.TracerProvider, telemetryService *telemetry.Service, sessions *sessions.Manager) *Service {
	return &Service{
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hooks"),
		logger:           logger.With(attr.SlogComponent("hooks")),
		db:               db,
		telemetryService: telemetryService,
		auth:             auth.New(logger, db, sessions),
	}
}

// APIKeyAuth implements the API key authentication for the hooks service
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// Claude is the unified endpoint for all Claude Code hook events
func (s *Service) Claude(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK Claude: %s", payload.HookEventName),
		attr.SlogEvent("claude_hook"),
		attr.SlogValueAny(map[string]any{
			"hookEventName": payload.HookEventName,
			"toolName":      payload.ToolName,
		}),
	)

	// Route to appropriate handler based on hook type
	switch payload.HookEventName {
	case "SessionStart":
		return s.handleSessionStart(ctx, payload)
	case "PreToolUse":
		return s.handlePreToolUse(ctx, payload)
	case "PostToolUse":
		return s.handlePostToolUse(ctx, payload)
	case "PostToolUseFailure":
		return s.handlePostToolUseFailure(ctx, payload)
	default:
		s.logger.ErrorContext(ctx, fmt.Sprintf("Unknown hook event: %s", payload.HookEventName))
		return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
			HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
				HookEventName: &payload.HookEventName,
			},
		}, nil
	}
}

func (s *Service) handleSessionStart(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	s.logger.InfoContext(ctx, "🚀 Session Start")

	// For now, always allow sessions to start
	continueVal := true
	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		Continue: &continueVal,
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

func (s *Service) handlePreToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	authCtx, ok := s.extractAuthContext(ctx, "PreToolUse")
	if !ok {
		// Allow tool to proceed even without auth
		allow := "allow"
		return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
			HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
				HookEventName:      &payload.HookEventName,
				PermissionDecision: &allow,
			},
		}, nil
	}

	attrs := s.buildTelemetryAttributes(authCtx, payload)
	s.writeToClickHouse(authCtx.ProjectID, authCtx.ActiveOrganizationID, s.getToolName(payload), attrs)

	// For now, always allow tools
	allow := "allow"
	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName:      &payload.HookEventName,
			PermissionDecision: &allow,
		},
	}, nil
}

func (s *Service) handlePostToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	authCtx, ok := s.extractAuthContext(ctx, "PostToolUse")
	if !ok {
		return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
			HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
				HookEventName: &payload.HookEventName,
			},
		}, nil
	}

	attrs := s.buildTelemetryAttributes(authCtx, payload)
	s.writeToClickHouse(authCtx.ProjectID, authCtx.ActiveOrganizationID, s.getToolName(payload), attrs)

	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

func (s *Service) handlePostToolUseFailure(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
	authCtx, ok := s.extractAuthContext(ctx, "PostToolUse Failure")
	if !ok {
		return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
			HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
				HookEventName: &payload.HookEventName,
			},
		}, nil
	}

	attrs := s.buildTelemetryAttributes(authCtx, payload)
	if payload.ToolError != nil {
		attrs[attr.HookErrorKey] = payload.ToolError
	}
	s.writeToClickHouse(authCtx.ProjectID, authCtx.ActiveOrganizationID, s.getToolName(payload), attrs)

	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

// generateTraceID generates a W3C-compliant trace ID (32 hex characters)
func generateTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashToolCallIDToTraceID converts a tool call ID (e.g., toolu_01SsRreQbJuFTsZS9ZszkzNR)
// into a W3C-compliant 32-character hex trace ID using SHA256 hashing
func hashToolCallIDToTraceID(toolCallID string) string {
	hash := sha256.Sum256([]byte(toolCallID))
	// Take first 16 bytes (128 bits) of the hash to create a 32-hex-char trace ID
	return hex.EncodeToString(hash[:16])
}

// generateSpanID generates a W3C-compliant span ID (16 hex characters)
func generateSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// extractAuthContext extracts and validates the auth context from the request
func (s *Service) extractAuthContext(ctx context.Context, handlerName string) (*contextvalues.AuthContext, bool) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.ErrorContext(ctx, fmt.Sprintf("%s called without valid auth context", handlerName))
		return nil, false
	}
	return authCtx, true
}

// getToolName safely extracts the tool name from the payload
func (s *Service) getToolName(payload *gen.ClaudePayload) string {
	if payload.ToolName != nil {
		return *payload.ToolName
	}
	return ""
}

// buildTelemetryAttributes creates the full set of attributes for a hook event with common fields
func (s *Service) buildTelemetryAttributes(authCtx *contextvalues.AuthContext, payload *gen.ClaudePayload) map[attr.Key]any {
	toolName := s.getToolName(payload)

	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.ToolNameKey:       toolName,
		attr.HookEventKey:      payload.HookEventName,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        generateTraceID(),
		attr.LogBodyKey:        fmt.Sprintf("Tool: %s, Hook: %s", toolName, payload.HookEventName),
		attr.UserIDKey:         authCtx.UserID,
		attr.ExternalUserIDKey: authCtx.ExternalUserID,
		attr.APIKeyIDKey:       authCtx.APIKeyID,
		attr.ProjectIDKey:      authCtx.ProjectID.String(),
		attr.OrganizationIDKey: authCtx.ActiveOrganizationID,
		attr.HookSourceKey:     "claude", // TODO: support other hook sources
	}

	if authCtx.Email != nil {
		attrs[attr.UserEmailKey] = *authCtx.Email
	}

	// Parse MCP tool names (format: mcp__<server>__<tool>)
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.ToolCallSourceKey] = parts[1]
			attrs[attr.ToolNameKey] = parts[2]
		}
	}

	// Hash toolUseID to create a valid W3C trace ID if available, otherwise use generated one
	if payload.ToolUseID != nil && *payload.ToolUseID != "" {
		attrs[attr.TraceIDKey] = hashToolCallIDToTraceID(*payload.ToolUseID)
	}
	if payload.SessionID != nil {
		attrs[attr.GenAIConversationIDKey] = *payload.SessionID
	}
	if payload.ToolUseID != nil {
		attrs[attr.GenAIToolCallIDKey] = *payload.ToolUseID
	}
	if payload.ToolInput != nil {
		attrs[attr.GenAIToolCallArgumentsKey] = payload.ToolInput
	}
	if payload.ToolResponse != nil {
		attrs[attr.GenAIToolCallResultKey] = payload.ToolResponse
	}

	return attrs
}

// writeToClickHouse writes the hook event to ClickHouse telemetry
func (s *Service) writeToClickHouse(projectID *uuid.UUID, organizationID string, toolName string, attrs map[attr.Key]any) {
	// Make sure we don't discard any tool name information
	if actualToolName, ok := attrs[attr.ToolNameKey]; ok {
		tn, ok := actualToolName.(string)
		if ok {
			toolName = tn
		}
	}

	// Build ToolInfo with project/org context from auth
	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: organizationID,
		ProjectID:      projectID.String(),
		ID:             "",
		URN:            "", // These tools have no real URN
		DeploymentID:   "",
		FunctionID:     nil,
	}

	s.telemetryService.CreateLog(telemetry.LogParams{
		Timestamp:  time.Now(),
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})
}
