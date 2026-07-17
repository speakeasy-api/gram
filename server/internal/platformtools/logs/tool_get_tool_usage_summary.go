package logs

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type GetToolUsageSummary struct {
	telemetry TelemetryService
}

type getToolUsageSummaryInput struct {
	From               string   `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format. Defaults to 7 days ago."`
	To                 string   `json:"to,omitempty" jsonschema:"End time in ISO 8601 format. Defaults to now."`
	TargetTypes        []string `json:"target_types,omitempty" jsonschema:"Target types to include: hosted_mcp_server, tunneled_mcp_server, shadow_mcp_server, local_tool, skill. Use tunneled_mcp_server for tunneled MCP servers."`
	HostedToolsetSlugs []string `json:"hosted_toolset_slugs,omitempty" jsonschema:"Hosted MCP toolset slugs to include."`
	ShadowServerNames  []string `json:"shadow_server_names,omitempty" jsonschema:"Shadow MCP server names to include."`
	HookSources        []string `json:"hook_sources,omitempty" jsonschema:"Hook plugin sources to include. Direct MCP calls have no hook source."`
}

func NewGetToolUsageSummaryTool(telemetrySvc TelemetryService) *GetToolUsageSummary {
	return &GetToolUsageSummary{telemetry: telemetrySvc}
}

func (s *GetToolUsageSummary) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "get_tool_usage_summary",
		Name:        "platform_get_tool_usage_summary",
		Description: "Summarize target-aware tool usage for hosted MCP, tunneled MCP, shadow MCP, local tools, and skills in the current project. Use target_types=[\"tunneled_mcp_server\"] for questions like which tunneled MCPs the team uses.",
		InputSchema: core.BuildInputSchema[getToolUsageSummaryInput](
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetToolUsageSummary) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := getToolUsageSummaryInput{
		From:               "",
		To:                 "",
		TargetTypes:        nil,
		HostedToolsetSlugs: nil,
		ShadowServerNames:  nil,
		HookSources:        nil,
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	from, to := defaultTimeWindow(input.From, input.To)

	targetTypes := make([]telemetry.ToolUsageTargetType, 0, len(input.TargetTypes))
	for _, targetType := range input.TargetTypes {
		targetTypes = append(targetTypes, telemetry.ToolUsageTargetType(targetType))
	}

	result, err := s.telemetry.GetToolUsageSummary(ctx, &telemetry.GetToolUsageSummaryPayload{
		ApikeyToken:        nil,
		SessionToken:       nil,
		ProjectSlugInput:   nil,
		From:               from,
		To:                 to,
		TargetTypes:        targetTypes,
		HostedToolsetSlugs: input.HostedToolsetSlugs,
		ShadowServerNames:  input.ShadowServerNames,
		UserFilters:        nil,
		AccountType:        nil,
		HookSources:        input.HookSources,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("get tool usage summary: %w", err)
	}

	return core.EncodeResult(wr, result)
}
