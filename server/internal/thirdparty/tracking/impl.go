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

func (c *Composite) TrackToolCallUsage(ctx context.Context, event billing.ToolCallUsageEvent) {
	c.polar.TrackToolCallUsage(ctx, event)

	properties := map[string]any{
		"organization_id":      event.OrganizationID,
		"request_bytes":        event.RequestBytes,
		"output_bytes":         event.OutputBytes,
		"total_bytes":          event.RequestBytes + event.OutputBytes,
		"tool_id":              event.ToolID,
		"tool_name":            event.ToolName,
		"project_id":           event.ProjectID,
		"type":                 string(event.Type),
		"disable_notification": true,
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
