package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

func (s *Service) PreToolUse(ctx context.Context, payload *gen.PreToolUsePayload) (*gen.HookResult, error) {
	// Extract auth context for project and organization IDs
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.WarnContext(ctx, "PreToolUse called without valid auth context")
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK PreToolUse: %s", payload.ToolName),
		attr.SlogEvent("pre_tool_use"),
		attr.SlogToolName(payload.ToolName),
		attr.SlogValueAny(payload.ToolInput),
	)

	// Write to ClickHouse
	attrs := s.buildBaseAttributes(payload.ToolName, payload.ToolInput)
	attrs[attr.HookEventKey] = "pre_tool_use"
	attrs[attr.LogBodyKey] = fmt.Sprintf("Pre-tool use hook: %s", payload.ToolName)

	s.writeToClickHouse(ctx, payload.ToolName, attrs)

	return &gen.HookResult{OK: true}, nil
}

func (s *Service) PostToolUse(ctx context.Context, payload *gen.PostToolUsePayload) (*gen.HookResult, error) {
	// Extract auth context for project and organization IDs
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.WarnContext(ctx, "PostToolUse called without valid auth context")
	}

	s.logger.InfoContext(ctx, fmt.Sprintf("🪝 HOOK PostToolUse: %s", payload.ToolName),
		attr.SlogEvent("post_tool_use"),
		attr.SlogToolName(payload.ToolName),
		attr.SlogValueAny(payload.ToolInput),
	)

	// Write to ClickHouse
	attrs := s.buildBaseAttributes(payload.ToolName, payload.ToolInput)
	attrs[attr.HookEventKey] = "post_tool_use"
	attrs[attr.LogBodyKey] = fmt.Sprintf("Post-tool use hook: %s", payload.ToolName)
	if payload.ToolResponse != nil {
		attrs[attr.HookToolResponseKey] = payload.ToolResponse
	}

	s.writeToClickHouse(ctx, payload.ToolName, attrs)

	return &gen.HookResult{OK: true}, nil
}

func (s *Service) PostToolUseFailure(ctx context.Context, payload *gen.PostToolUseFailurePayload) (*gen.HookResult, error) {
	// Extract auth context for project and organization IDs
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		s.logger.WarnContext(ctx, "PostToolUseFailure called without valid auth context")
	}

	s.logger.WarnContext(ctx, fmt.Sprintf("🪝 HOOK PostToolUseFailure: %s", payload.ToolName),
		attr.SlogEvent("post_tool_use_failure"),
		attr.SlogToolName(payload.ToolName),
		attr.SlogValueAny(payload.ToolInput),
	)

	// Write to ClickHouse
	attrs := s.buildBaseAttributes(payload.ToolName, payload.ToolInput)
	attrs[attr.HookEventKey] = "post_tool_use_failure"
	attrs[attr.LogBodyKey] = fmt.Sprintf("Post-tool use failure hook: %s", payload.ToolName)
	if payload.ToolError != nil {
		attrs[attr.HookToolErrorKey] = payload.ToolError
	}

	s.writeToClickHouse(ctx, payload.ToolName, attrs)

	return &gen.HookResult{OK: true}, nil
}

// buildBaseAttributes creates the base set of attributes for a hook event
func (s *Service) buildBaseAttributes(toolName string, toolInput any) map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.ToolNameKey: toolName,
	}

	if toolInput != nil {
		attrs[attr.HookToolInputKey] = toolInput
	}

	// Parse MCP tool names (format: mcp__<server>__<tool>)
	if strings.HasPrefix(toolName, "mcp__") {
		parts := strings.SplitN(toolName, "__", 3)
		if len(parts) == 3 {
			attrs[attr.McpServerNameKey] = parts[1]
			attrs[attr.McpToolNameKey] = parts[2]
		}
	}

	return attrs
}

// writeToClickHouse writes the hook event to ClickHouse telemetry
func (s *Service) writeToClickHouse(ctx context.Context, toolName string, attrs map[attr.Key]any) {
	// Extract auth context to get project and organization IDs
	authCtx, ok := contextvalues.GetAuthContext(ctx)

	var projectID string
	var organizationID string

	if ok && authCtx != nil {
		if authCtx.ProjectID != nil {
			projectID = authCtx.ProjectID.String()
		}
		organizationID = authCtx.ActiveOrganizationID
	}

	// Build ToolInfo with project/org context from auth
	toolInfo := telemetry.ToolInfo{
		Name:           toolName,
		OrganizationID: organizationID,
		ProjectID:      projectID,
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
