package toolmetrics

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/trace"
)

// NewToolLog initializes a ToolHTTPRequest with common metadata before the HTTP round tripper executes.
func NewToolLog(ctx context.Context, tool ToolInfo, toolType ToolType) (*ToolHTTPRequest, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate tool http request id: %w", err)
	}

	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	var traceID, spanID string
	if spanCtx.HasTraceID() {
		traceID = spanCtx.TraceID().String()
	}
	if spanCtx.HasSpanID() {
		spanID = spanCtx.SpanID().String()
	}

	return &ToolHTTPRequest{
		ID:                id.String(),
		Ts:                time.Unix(id.Time().UnixTime()),
		OrganizationID:    tool.OrganizationID,
		ProjectID:         tool.ProjectID,
		DeploymentID:      tool.DeploymentID,
		ToolID:            tool.ID,
		ToolURN:           tool.Urn,
		ToolType:          toolType,
		TraceID:           traceID,
		SpanID:            spanID,
		StatusCode:        0,
		DurationMs:        0,
		UserAgent:         "",
		HTTPMethod:        "",
		HTTPRoute:         "",
		RequestHeaders:    map[string]string{},
		RequestBodyBytes:  0,
		ResponseHeaders:   map[string]string{},
		ResponseBodyBytes: 0,
	}, nil
}

// WithStatusCode sets the HTTP status code on the log entry.
func (t *ToolHTTPRequest) WithStatusCode(code int64) *ToolHTTPRequest {
	t.StatusCode = code
	return t
}

// WithHTTPMethod sets the HTTP method on the log entry.
func (t *ToolHTTPRequest) WithHTTPMethod(method string) *ToolHTTPRequest {
	t.HTTPMethod = method
	return t
}

// WithHTTPRoute sets the HTTP route on the log entry.
func (t *ToolHTTPRequest) WithHTTPRoute(route string) *ToolHTTPRequest {
	t.HTTPRoute = route
	return t
}

// EmitHTTPRequestLog logs the provided HTTP request using the tool metrics provider.
// Errors are reported through the supplied logger. Logging happens asynchronously to
// avoid blocking the caller and the request struct is copied to prevent data races.
func EmitHTTPRequestLog(
	ctx context.Context,
	logger *slog.Logger,
	provider ToolMetricsProvider,
	toolName string,
	request ToolHTTPRequest,
) {
	if provider == nil || request.ID == "" {
		return
	}

	go func() {
		logCtx := context.WithoutCancel(ctx)

		if err := provider.Log(logCtx, request); err != nil {
			logger.ErrorContext(logCtx,
				"failed to log HTTP attempt to ClickHouse",
				attr.SlogError(err),
				attr.SlogToolURN(request.ToolURN),
				attr.SlogToolName(toolName),
				attr.SlogHTTPRequestMethod(request.HTTPMethod),
			)
			return
		}

		logger.DebugContext(logCtx,
			"logged HTTP request to ClickHouse",
			attr.SlogToolURN(request.ToolURN),
			attr.SlogToolName(toolName),
			attr.SlogHTTPRequestMethod(request.HTTPMethod),
			attr.SlogHTTPResponseStatusCode(int(request.StatusCode)),
		)
	}()
}
