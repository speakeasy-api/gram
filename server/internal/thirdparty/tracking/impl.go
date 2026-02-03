package tracking

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type Composite struct {
	polar   *polar.Client
	posthog *posthog.Posthog
	logger  *slog.Logger
}

var _ billing.Tracker = (*Composite)(nil)

func New(polar *polar.Client, posthog *posthog.Posthog, logger *slog.Logger) *Composite {
	return &Composite{
		polar:   polar,
		posthog: posthog,
		logger:  logger.With(attr.SlogComponent("usage-tracker")),
	}
}

func (c *Composite) TrackModelUsage(ctx context.Context, event billing.ModelUsageEvent) {
	c.polar.TrackModelUsage(ctx, event)

	properties := map[string]any{
		"organization_id":         event.OrganizationID,
		"organization_slug":       event.OrganizationSlug,
		"model":                   event.Model,
		"source":                  string(event.Source),
		"input_tokens":            event.InputTokens,
		"output_tokens":           event.OutputTokens,
		"total_tokens":            event.TotalTokens,
		"project_id":              event.ProjectID,
		"chat_id":                 event.ChatID,
		"native_tokens_cached":    event.NativeTokensCached,
		"native_tokens_reasoning": event.NativeTokensReasoning,
		"cache_discount":          event.CacheDiscount,
		"upstream_inference_cost": event.UpstreamInferenceCost,
		"disable_notification":    true,
	}
	if event.Cost != nil {
		properties["cost"] = *event.Cost
	}

	if err := c.posthog.CaptureEvent(ctx, "model_usage", event.OrganizationID, properties); err != nil {
		c.logger.ErrorContext(ctx, "failed to capture model usage event", attr.SlogError(err))
	}
}

func (c *Composite) TrackToolCallUsage(ctx context.Context, event billing.ToolCallUsageEvent) {
	c.polar.TrackToolCallUsage(ctx, event)

	properties := map[string]any{
		"organization_id":      event.OrganizationID,
		"request_bytes":        event.RequestBytes,
		"output_bytes":         event.OutputBytes,
		"total_bytes":          event.RequestBytes + event.OutputBytes,
		"tool_urn":             event.ToolURN,
		"tool_name":            event.ToolName,
		"project_id":           event.ProjectID,
		"type":                 string(event.Type),
		"disable_notification": true,
		"status_code":          event.ResponseStatusCode,
		"success":              toolCallSuccessHeuristic(event.ResponseStatusCode),
	}

	if event.ResourceURI != "" {
		properties["resource_uri"] = event.ResourceURI
	}
	if event.ProjectSlug != nil {
		properties["project_slug"] = *event.ProjectSlug
	}
	if event.OrganizationSlug != nil {
		properties["organization_slug"] = *event.OrganizationSlug
	}
	if event.ToolsetSlug != nil {
		properties["toolset_slug"] = *event.ToolsetSlug
	}
	if event.ChatID != nil {
		properties["chat_session_id"] = *event.ChatID
	}
	if event.MCPSessionID != nil {
		properties["mcp_session_id"] = *event.MCPSessionID
	}
	if event.MCPURL != nil {
		properties["mcp_url"] = *event.MCPURL
	}
	if event.ToolsetID != nil {
		properties["toolset_id"] = *event.ToolsetID
	}
	if event.FunctionCPUUsage != nil {
		properties["function_cpu_usage"] = *event.FunctionCPUUsage
	}
	if event.FunctionMemUsage != nil {
		properties["function_mem_usage"] = *event.FunctionMemUsage
	}
	if event.FunctionExecutionTime != nil {
		properties["function_execution_time"] = *event.FunctionExecutionTime
	}

	if err := c.posthog.CaptureEvent(ctx, "tool_call", event.OrganizationID, properties); err != nil {
		c.logger.ErrorContext(ctx, "failed to capture tool call usage event", attr.SlogError(err))
	}
}

func (c *Composite) TrackPromptCallUsage(ctx context.Context, event billing.PromptCallUsageEvent) {
	c.polar.TrackPromptCallUsage(ctx, event)

	properties := map[string]any{
		"organization_id":      event.OrganizationID,
		"request_bytes":        event.RequestBytes,
		"output_bytes":         event.OutputBytes,
		"total_bytes":          event.RequestBytes + event.OutputBytes,
		"prompt_name":          event.PromptName,
		"project_id":           event.ProjectID,
		"disable_notification": true,
	}

	if event.PromptID != nil {
		properties["prompt_id"] = *event.PromptID
	}
	if event.ProjectSlug != nil {
		properties["project_slug"] = *event.ProjectSlug
	}
	if event.OrganizationSlug != nil {
		properties["organization_slug"] = *event.OrganizationSlug
	}
	if event.ToolsetSlug != nil {
		properties["toolset_slug"] = *event.ToolsetSlug
	}
	if event.ChatID != nil {
		properties["chat_id"] = *event.ChatID
	}
	if event.MCPURL != nil {
		properties["mcp_url"] = *event.MCPURL
	}
	if event.MCPSessionID != nil {
		properties["mcp_session_id"] = *event.MCPSessionID
	}
	if event.ChatID != nil {
		properties["chat_session_id"] = *event.ChatID
	}
	if event.ToolsetID != nil {
		properties["toolset_id"] = *event.ToolsetID
	}

	if err := c.posthog.CaptureEvent(ctx, "prompt_call", event.OrganizationID, properties); err != nil {
		c.logger.ErrorContext(ctx, "failed to capture prompt call usage event", attr.SlogError(err))
	}
}

func (c *Composite) TrackPlatformUsage(ctx context.Context, events []billing.PlatformUsageEvent) {
	c.polar.TrackPlatformUsage(ctx, events)
}

// for product metrics we create our own heuristic to estimate tool call success
// this is for cases of unauthenticated requests, timeouts, our server errors that we are fairly certain are indicative of a failed tool call
// it's important to note that a non 200 alone does not indicate that the actual tool did not suceed in its job
func toolCallSuccessHeuristic(statusCode int) bool {
	return statusCode == 0 || statusCode == 401 || statusCode != 403 || statusCode >= 500
}
