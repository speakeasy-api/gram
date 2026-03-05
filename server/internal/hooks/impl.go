package hooks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	sessionCache     cache.TypedCacheObject[SessionMetadata]
	hookBufferCache  cache.TypedCacheObject[ClaudePayloadCache]
}

// SessionMetadata contains validated session information from the Logs endpoint
type SessionMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	ClaudeOrgID string
	GramOrgID   string
	ProjectID   string
}

// HookSpecificOutput is the structure for hook-specific output in responses
type HookSpecificOutput struct {
	HookEventName            *string `json:"hookEventName,omitempty"`
	AdditionalContext        *string `json:"additionalContext,omitempty"`
	PermissionDecision       *string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason *string `json:"permissionDecisionReason,omitempty"`
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, tracerProvider trace.TracerProvider, telemetryService *telemetry.Service, sessions *sessions.Manager, cacheAdapter cache.Cache) *Service {
	sessionCache := cache.NewTypedObjectCache[SessionMetadata](logger.With(attr.SlogCacheNamespace("session")), cacheAdapter, cache.SuffixNone)
	hookBufferCache := cache.NewTypedObjectCache[ClaudePayloadCache](logger.With(attr.SlogCacheNamespace("hook_buffer")), cacheAdapter, cache.SuffixNone)

	return &Service{
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hooks"),
		logger:           logger.With(attr.SlogComponent("hooks")),
		db:               db,
		telemetryService: telemetryService,
		auth:             auth.New(logger, db, sessions),
		sessionCache:     sessionCache,
		hookBufferCache:  hookBufferCache,
	}
}

// APIKeyAuth implements the API key authentication for the hooks service
func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// claudeRequestDecoder is a custom decoder that handles both JSON and form-urlencoded content types
func claudeRequestDecoder(r *http.Request) goahttp.Decoder {
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return &formDecoder{r: r}
	}

	return goahttp.RequestDecoder(r)
}

// formDecoder implements goahttp.Decoder for form-urlencoded data
type formDecoder struct {
	r *http.Request
}

func (d *formDecoder) Decode(v interface{}) error {
	body, err := io.ReadAll(d.r.Body)
	if err != nil {
		return err
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return err
	}

	// Convert form values to JSON string and then unmarshal
	// This works because the form keys match the JSON field names
	jsonData := make(map[string]interface{})
	for key, vals := range values {
		if len(vals) > 0 {
			// Try to unmarshal as JSON if the value looks like JSON
			var parsed interface{}
			if err := json.Unmarshal([]byte(vals[0]), &parsed); err == nil {
				jsonData[key] = parsed
			} else {
				jsonData[key] = vals[0]
			}
		}
	}

	// Marshal back to JSON and unmarshal into the target struct
	jsonBytes, err := json.Marshal(jsonData)
	if err != nil {
		return err
	}

	return json.Unmarshal(jsonBytes, v)
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, claudeRequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// Logs handles authenticated OTEL logs data from Claude Code
func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	claudeMetadata := extractSessionMetadata(payload)
	completeMetadata := SessionMetadata{
		SessionID:   claudeMetadata.SessionID,
		ServiceName: claudeMetadata.ServiceName,
		UserEmail:   claudeMetadata.UserEmail,
		ClaudeOrgID: claudeMetadata.ClaudeOrgID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	if err := s.sessionCache.Store(ctx, completeMetadata); err != nil {
		s.logger.ErrorContext(ctx, "Failed to store session metadata", attr.SlogError(err))
	}

	s.flushPendingHooks(ctx, completeMetadata.SessionID, &completeMetadata)

	s.logger.InfoContext(ctx, "Stored session metadata",
		attr.SlogEvent("session_validated"),
		"session_id", completeMetadata.SessionID,
		"user_email", completeMetadata.UserEmail,
		"claude_org_id", completeMetadata.ClaudeOrgID,
		"gram_org_id", completeMetadata.GramOrgID,
		"project_id", completeMetadata.ProjectID,
	)

	return nil
}

type claudeLogMetadata struct {
	SessionID   string
	ServiceName string
	UserEmail   string
	ClaudeOrgID string
}

func extractSessionMetadata(payload *gen.LogsPayload) claudeLogMetadata {
	metadata := claudeLogMetadata{
		SessionID:   "",
		ServiceName: "",
		UserEmail:   "",
		ClaudeOrgID: "",
	}

	// Iterate through all resource logs
	for _, resourceLog := range payload.ResourceLogs {
		if resourceLog == nil {
			continue
		}

		// Extract service name from resource attributes
		metadata.ServiceName = extractResourceAttribute(resourceLog.Resource, "service.name")

		// Iterate through all scope logs
		for _, scopeLog := range resourceLog.ScopeLogs {
			if scopeLog == nil {
				continue
			}

			// Iterate through all log records
			for _, logRecord := range scopeLog.LogRecords {
				if logRecord == nil {
					continue
				}

				// Extract session data
				data := extractLogData(logRecord)

				if data.SessionID == "" {
					continue
				}

				// Store session metadata in Redis
				metadata.SessionID = data.SessionID
				metadata.UserEmail = data.UserEmail
				metadata.ClaudeOrgID = data.ClaudeOrgID
			}
		}
	}

	return metadata
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

	s.recordToolEvent(ctx, payload)

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
	// For now, always allow sessions to start
	continueVal := true
	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		Continue: &continueVal,
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

// recordToolEvent records a tool event, either directly to ClickHouse if session is validated, or buffers it
func (s *Service) recordToolEvent(ctx context.Context, payload *gen.ClaudePayload) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		s.logger.WarnContext(ctx, "Tool event called without session ID", "hook_event", payload.HookEventName)
		return
	}

	sessionID := *payload.SessionID
	metadata, err := s.sessionCache.Get(ctx, sessionCacheKey(sessionID))

	if err == nil {
		s.writeHookToClickHouseWithMetadata(ctx, payload, &metadata)
	} else {
		// Session not validated yet - buffer in Redis
		if err := s.bufferHook(ctx, sessionID, payload); err != nil {
			s.logger.ErrorContext(ctx, "Failed to buffer hook", attr.SlogError(err))
		}
	}
}

func (s *Service) handlePreToolUse(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
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
	return &gen.ClaudeHookResult{ //nolint:exhaustruct // optional fields
		HookSpecificOutput: &HookSpecificOutput{ //nolint:exhaustruct // optional fields
			HookEventName: &payload.HookEventName,
		},
	}, nil
}

func (s *Service) handlePostToolUseFailure(ctx context.Context, payload *gen.ClaudePayload) (*gen.ClaudeHookResult, error) {
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

// OTELLogData contains extracted data from an OTEL log record
type OTELLogData struct {
	SessionID   string
	UserEmail   string
	ClaudeOrgID string
}

// extractResourceAttribute extracts a specific attribute from OTEL resource
func extractResourceAttribute(resource *gen.OTELResource, key string) string {
	if resource == nil || resource.Attributes == nil {
		return ""
	}
	for _, attr := range resource.Attributes {
		if attr.Key == key && attr.Value != nil && attr.Value.StringValue != nil {
			return *attr.Value.StringValue
		}
	}
	return ""
}

// extractLogData extracts session data from an OTEL log record
func extractLogData(logRecord *gen.OTELLogRecord) OTELLogData {
	data := OTELLogData{ //nolint:exhaustruct // fields are populated below
		SessionID:   "",
		UserEmail:   "",
		ClaudeOrgID: "",
	}

	if logRecord.Attributes == nil {
		return data
	}

	for _, attr := range logRecord.Attributes {
		if attr.Value == nil {
			continue
		}

		var value string
		if attr.Value.StringValue != nil {
			value = *attr.Value.StringValue
		}

		switch attr.Key {
		case "session.id":
			data.SessionID = value
		case "user.email":
			data.UserEmail = value
		case "organization.id":
			data.ClaudeOrgID = value
		}
	}

	return data
}
