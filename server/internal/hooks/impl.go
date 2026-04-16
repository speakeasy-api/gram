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
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	srv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
)

type Service struct {
	tracer             trace.Tracer
	logger             *slog.Logger
	db                 *pgxpool.Pool
	telemetryService   *telemetry.Service
	auth               *auth.Auth
	cache              cache.Cache
	temporalEnv        *tenv.Environment
	repo               *repo.Queries
	productFeatures    ProductFeaturesClient
	chatTitleGenerator ChatTitleGenerator
	sessionTotals      map[string]*SessionTotals
	sessionTotalsMu    sync.RWMutex
}

// SessionTotals tracks cumulative metrics for a session
type SessionTotals struct {
	TotalCost         float64
	TotalInputTokens  float64
	TotalOutputTokens float64
	TotalCacheRead    float64
	TotalCacheCreate  float64
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

// ProductFeaturesClient checks whether product features are enabled for an org.
type ProductFeaturesClient interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

// ChatTitleGenerator schedules async chat title generation.
type ChatTitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	tracerProvider trace.TracerProvider,
	telemetryService *telemetry.Service,
	sessionsMgr *sessions.Manager,
	cacheAdapter cache.Cache,
	completionsClient openrouter.CompletionClient,
	temporalEnv *tenv.Environment,
	accessLoader auth.AccessLoader,
	pfClient ProductFeaturesClient,
	chatTitleGenerator ChatTitleGenerator,
) *Service {
	return &Service{
		tracer:             tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hooks"),
		logger:             logger.With(attr.SlogComponent("hooks")),
		db:                 db,
		telemetryService:   telemetryService,
		auth:               auth.New(logger, db, sessionsMgr, accessLoader),
		cache:              cacheAdapter,
		temporalEnv:        temporalEnv,
		repo:               repo.New(db),
		productFeatures:    pfClient,
		chatTitleGenerator: chatTitleGenerator,
		sessionTotals:      make(map[string]*SessionTotals),
	}
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, claudeRequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
	AttachServerNames(mux, service)
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

	// Extract and print token usage metrics
	tokenMetrics := extractTokenMetrics(payload)
	j, _ := json.MarshalIndent(tokenMetrics, "", "  ")

	s.logger.InfoContext(ctx, "Received Claude token metrics",
		attr.SlogEvent("claude_metrics"),
		attr.SlogValueAny(map[string]any{
			"organization_id": authCtx.ActiveOrganizationID,
			"project_id":      authCtx.ProjectID.String(),
		}),
	)

	// Update running totals
	s.updateSessionTotals(tokenMetrics)

	// Write metrics to ClickHouse
	s.writeMetricsToClickHouse(ctx, payload, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	// Get the current totals for this session
	s.sessionTotalsMu.RLock()
	totals := s.sessionTotals[tokenMetrics.SessionID]
	s.sessionTotalsMu.RUnlock()

	println("--------------------------------")
	println("EXTRACTED METRICS:")
	println(string(j))
	println("")
	println("SESSION TOTALS:")
	if totals != nil {
		totalsJSON, _ := json.MarshalIndent(map[string]any{
			"session_id":          tokenMetrics.SessionID,
			"total_cost_usd":      totals.TotalCost,
			"total_input_tokens":  totals.TotalInputTokens,
			"total_output_tokens": totals.TotalOutputTokens,
			"total_cache_read":    totals.TotalCacheRead,
			"total_cache_create":  totals.TotalCacheCreate,
		}, "", "  ")
		println(string(totalsJSON))
	}
	println("--------------------------------")

	return nil
}

// updateSessionTotals accumulates metrics for a session
func (s *Service) updateSessionTotals(metrics TokenMetrics) {
	if metrics.SessionID == "" {
		return
	}

	s.sessionTotalsMu.Lock()
	defer s.sessionTotalsMu.Unlock()

	if s.sessionTotals[metrics.SessionID] == nil {
		s.sessionTotals[metrics.SessionID] = &SessionTotals{}
	}

	totals := s.sessionTotals[metrics.SessionID]

	for _, dp := range metrics.DataPoints {
		switch dp.MetricName {
		case "claude_code.cost.usage":
			totals.TotalCost += dp.Value
		case "claude_code.token.usage":
			switch dp.Type {
			case "input":
				totals.TotalInputTokens += dp.Value
			case "output":
				totals.TotalOutputTokens += dp.Value
			case "cacheRead":
				totals.TotalCacheRead += dp.Value
			case "cacheCreation":
				totals.TotalCacheCreate += dp.Value
			}
		}
	}
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
	// For now, always allow tools
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

// Cursor is the endpoint for Cursor hook events
func (s *Service) Cursor(ctx context.Context, payload *gen.CursorPayload) (*gen.CursorHookResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "unauthorized")
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK Cursor: %s", payload.HookEventName),
		attr.SlogEvent("cursor_hook"),
		attr.SlogValueAny(map[string]any{
			"hookEventName": payload.HookEventName,
			"toolName":      payload.ToolName,
		}),
	)

	s.writeCursorHookToClickHouse(ctx, payload, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())

	result := &gen.CursorHookResult{
		Permission:        nil,
		UserMessage:       nil,
		AdditionalContext: nil,
	}

	switch payload.HookEventName {
	case "preToolUse":
		result.Permission = new("allow")
	default:
		// nothing to do
	}

	return result, nil
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

// OTELLogData contains extracted data from an OTEL log record
type OTELLogData struct {
	SessionID   string
	UserEmail   string
	ClaudeOrgID string
}

// TokenMetrics contains extracted token usage information
type TokenMetrics struct {
	SessionID   string
	Model       string
	UserEmail   string
	ClaudeOrgID string
	DataPoints  []TokenDataPoint
}

// TokenDataPoint represents a single metric data point
type TokenDataPoint struct {
	MetricName string
	Type       string
	Value      float64
	Timestamp  string
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

// extractTokenMetrics extracts token usage information from OTEL metrics payload
func extractTokenMetrics(payload *gen.MetricsPayload) TokenMetrics {
	metrics := TokenMetrics{
		DataPoints: make([]TokenDataPoint, 0),
	}

	if payload.ResourceMetrics == nil {
		return metrics
	}

	// Iterate through resource metrics
	for _, resourceMetric := range payload.ResourceMetrics {
		if resourceMetric == nil || resourceMetric.ScopeMetrics == nil {
			continue
		}

		// Iterate through scope metrics
		for _, scopeMetric := range resourceMetric.ScopeMetrics {
			if scopeMetric == nil || scopeMetric.Metrics == nil {
				continue
			}

			// Iterate through individual metrics
			for _, metric := range scopeMetric.Metrics {
				if metric == nil || metric.Name == nil || metric.Sum == nil {
					continue
				}

				metricName := *metric.Name

				// Process each data point
				for _, dataPoint := range metric.Sum.DataPoints {
					if dataPoint == nil {
						continue
					}

					// Extract common attributes
					sessionID := extractAttributeString(dataPoint.Attributes, "session.id")
					model := extractAttributeString(dataPoint.Attributes, "model")
					userEmail := extractAttributeString(dataPoint.Attributes, "user.email")
					claudeOrgID := extractAttributeString(dataPoint.Attributes, "organization.id")
					metricType := extractAttributeString(dataPoint.Attributes, "type")

					// Store common metadata (from first data point)
					if sessionID != "" && metrics.SessionID == "" {
						metrics.SessionID = sessionID
					}
					if model != "" && metrics.Model == "" {
						metrics.Model = model
					}
					if userEmail != "" && metrics.UserEmail == "" {
						metrics.UserEmail = userEmail
					}
					if claudeOrgID != "" && metrics.ClaudeOrgID == "" {
						metrics.ClaudeOrgID = claudeOrgID
					}

					// Get the value
					value := float64(0)
					if dataPoint.AsDouble != nil {
						value = *dataPoint.AsDouble
					} else if dataPoint.AsInt != nil {
						value = float64(*dataPoint.AsInt)
					}

					// Get timestamp
					timestamp := ""
					if dataPoint.TimeUnixNano != nil {
						timestamp = *dataPoint.TimeUnixNano
					}

					// Create data point entry
					metrics.DataPoints = append(metrics.DataPoints, TokenDataPoint{
						MetricName: metricName,
						Type:       metricType,
						Value:      value,
						Timestamp:  timestamp,
					})
				}
			}
		}
	}

	return metrics
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
