package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

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

func (d *formDecoder) Decode(v any) error {
	body, err := io.ReadAll(d.r.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	values, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("parse query: %w", err)
	}

	// Convert form values to JSON string and then unmarshal
	// This works because the form keys match the JSON field names
	jsonData := make(map[string]any)
	for key, vals := range values {
		if len(vals) > 0 {
			// Try to unmarshal as JSON if the value looks like JSON
			var parsed any
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
		return fmt.Errorf("marshal json: %w", err)
	}

	if err := json.Unmarshal(jsonBytes, v); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}

// Logs handles authenticated OTEL logs data from Claude Code
func (s *Service) Logs(ctx context.Context, payload *gen.LogsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	claudeMetadata := extractSessionMetadata(payload)
	if claudeMetadata.SessionID == "" {
		s.logger.WarnContext(ctx, "Logs payload contained no session ID")
		return nil
	}

	completeMetadata := SessionMetadata{
		SessionID:   claudeMetadata.SessionID,
		ServiceName: claudeMetadata.ServiceName,
		UserEmail:   claudeMetadata.UserEmail,
		ClaudeOrgID: claudeMetadata.ClaudeOrgID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	if err := s.cache.Set(ctx, sessionCacheKey(completeMetadata.SessionID), completeMetadata, 24*time.Hour); err != nil {
		s.logger.ErrorContext(ctx, "Failed to store session metadata", attr.SlogError(err))
	}

	s.flushPendingHooks(ctx, completeMetadata.SessionID, &completeMetadata)

	s.logger.InfoContext(ctx, "Stored session metadata",
		attr.SlogEvent("session_validated"),
	)

	return nil
}

// Metrics handles authenticated OTEL metrics data from Claude Code
func (s *Service) Metrics(ctx context.Context, payload *gen.MetricsPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	s.logger.InfoContext(ctx, "Received Claude token metrics",
		attr.SlogEvent("claude_metrics"),
		attr.SlogValueAny(map[string]any{
			"organization_id": authCtx.ActiveOrganizationID,
			"project_id":      authCtx.ProjectID.String(),
		}),
	)

	// Write metrics to ClickHouse
	s.writeMetricsToClickHouse(ctx, payload, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

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
func (s *Service) Claude(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK Claude: %s", payload.HookEventName),
		attr.SlogEvent("claude_hook"),
		attr.SlogValueAny(map[string]any{
			"hookEventName": payload.HookEventName,
			"toolName":      payload.ToolName,
		}),
	)

	s.recordHook(ctx, payload)

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
	case "UserPromptSubmit":
		return s.handleUserPromptSubmit(ctx, payload)
	case "Stop":
		return s.handleStop(ctx, payload)
	case "SessionEnd":
		return s.handleSessionEnd(ctx, payload)
	case "Notification":
		return s.handleNotification(ctx, payload)
	default:
		s.logger.ErrorContext(ctx, fmt.Sprintf("Unknown hook event: %s", payload.HookEventName))
		return makeHookResult(payload.HookEventName), nil
	}
}

func (s *Service) handleSessionStart(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	// Always allow sessions to start
	continueVal := true
	result := makeHookResult(payload.HookEventName)
	result.Continue = &continueVal
	return result, nil
}

func (s *Service) recordHook(ctx context.Context, payload *gen.ClaudeHookPayload) {
	if payload.SessionID == nil || *payload.SessionID == "" {
		s.logger.WarnContext(ctx, "Tool event called without session ID")
		return
	}

	sessionID := *payload.SessionID
	metadata, err := s.getSessionMetadata(ctx, sessionID)
	if err == nil {
		s.persistHook(ctx, payload, &metadata)
	} else {
		// Session not validated yet - buffer in Redis
		if err := s.bufferHook(ctx, sessionID, payload); err != nil {
			s.logger.ErrorContext(ctx, "Failed to buffer hook", attr.SlogError(err))
		}
	}
}

func (s *Service) persistHook(ctx context.Context, payload *gen.ClaudeHookPayload, metadata *SessionMetadata) {
	if isConversationEvent(payload.HookEventName) {
		if err := s.persistConversationEvent(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist conversation event", attr.SlogError(err))
		}
	} else {
		if err := s.persistToolCallEvent(ctx, payload, metadata); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist tool call event", attr.SlogError(err))
		}
	}
}

func (s *Service) getSessionMetadata(ctx context.Context, sessionID string) (SessionMetadata, error) {
	var metadata SessionMetadata
	err := s.cache.Get(ctx, sessionCacheKey(sessionID), &metadata)
	if err != nil {
		return SessionMetadata{}, fmt.Errorf("get session metadata: %w", err)
	}
	return metadata, nil
}

func (s *Service) handlePreToolUse(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	if s.riskScanner != nil && payload.SessionID != nil {
		scanResult := s.scanClaudeForEnforcement(ctx, payload)
		if scanResult != nil && scanResult.Action == "block" {
			deny := "deny"
			reason := fmt.Sprintf("Blocked by policy %q: %s detected", scanResult.PolicyName, scanResult.Description)
			result := makeHookResult(payload.HookEventName)
			if output, ok := result.HookSpecificOutput.(*HookSpecificOutput); ok {
				output.PermissionDecision = &deny
				output.PermissionDecisionReason = &reason
			}
			return result, nil
		}
		if scanResult != nil && scanResult.Action == "redact" && scanResult.RedactedText != "" {
			allow := "allow"
			reason := fmt.Sprintf("Content redacted by policy %q: %s detected", scanResult.PolicyName, scanResult.Description)
			result := makeHookResult(payload.HookEventName)
			if output, ok := result.HookSpecificOutput.(*HookSpecificOutput); ok {
				output.PermissionDecision = &allow
				output.PermissionDecisionReason = &reason
				output.UpdatedInput = payload.ToolInput // TODO: replace with redacted version once tool input parsing is implemented
			}
			return result, nil
		}
	}

	allow := "allow"
	result := makeHookResult(payload.HookEventName)
	if output, ok := result.HookSpecificOutput.(*HookSpecificOutput); ok {
		output.PermissionDecision = &allow
	}
	return result, nil
}

func (s *Service) handlePostToolUse(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
}

func (s *Service) handlePostToolUseFailure(ctx context.Context, payload *gen.ClaudeHookPayload) (*gen.ClaudeHookResult, error) {
	return makeHookResult(payload.HookEventName), nil
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
	data := OTELLogData{
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

// extractAttributeString extracts a string attribute value by key
func extractAttributeString(attributes []*gen.OTELAttribute, key string) string {
	if attributes == nil {
		return ""
	}

	for _, attr := range attributes {
		if attr.Key == key && attr.Value != nil && attr.Value.StringValue != nil {
			return *attr.Value.StringValue
		}
	}

	return ""
}
