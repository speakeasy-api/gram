package logs

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// compactToolRows is how many per-target tool rows the compact summary keeps.
const compactToolRows = 25

type GetToolUsageSummary struct {
	telemetry TelemetryService
}

type getToolUsageSummaryInput struct {
	From               string   `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format. Defaults to 7 days ago."`
	To                 string   `json:"to,omitempty" jsonschema:"End time in ISO 8601 format. Defaults to now."`
	TargetTypes        []string `json:"target_types,omitempty" jsonschema:"Target types to include: hosted_mcp_server, tunneled_mcp_server, meta_mcp_server, shadow_mcp_server, local_tool, skill. Use tunneled_mcp_server for tunneled MCP servers and meta_mcp_server for calls observed against a gateway endpoint."`
	HostedToolsetSlugs []string `json:"hosted_toolset_slugs,omitempty" jsonschema:"Hosted MCP toolset slugs to include. Prefer mcp_server_ids."`
	MCPServerIDs       []string `json:"mcp_server_ids,omitempty" jsonschema:"MCP server ids (UUIDs, at most 100) to include: calls served by those hosted, remote or tunneled servers. Toolset slugs go in hosted_toolset_slugs."`
	ShadowServerNames  []string `json:"shadow_server_names,omitempty" jsonschema:"Shadow MCP server names to include."`
	MetaMcpServerIDs   []string `json:"meta_mcp_server_ids,omitempty" jsonschema:"Gateway (meta MCP server) ids to include: calls dispatched through the gateway plus calls observed against it."`
	HookSources        []string `json:"hook_sources,omitempty" jsonschema:"Hook plugin sources to include. Direct MCP calls have no hook source."`
	ClientKeys         []string `json:"client_keys,omitempty" jsonschema:"MCP clients to include, lowercased as the client named itself at the initialize handshake. Use \"unattributed\" for calls whose caller reported no client name."`
	IncludeTimeSeries  bool     `json:"include_time_series,omitempty" jsonschema:"Include the per-target and per-user time series (very large: one row per bucket per target or user). Defaults to false."`
	IncludeBreakdowns  bool     `json:"include_breakdowns,omitempty" jsonschema:"Include every per-target tool row and the cross breakdowns of users by target and of tools by MCP client (large). Defaults to false: totals, targets, users, clients and the 25 per-target tool rows with the most failures are returned."`
}

func NewGetToolUsageSummaryTool(telemetrySvc TelemetryService) *GetToolUsageSummary {
	return &GetToolUsageSummary{telemetry: telemetrySvc}
}

func (s *GetToolUsageSummary) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "get_tool_usage_summary",
		Name:        "platform_get_tool_usage_summary",
		Description: "Summarize target-aware tool usage for hosted MCP, tunneled MCP, gateways, shadow MCP, local tools, and skills in the current project over a time window (default: the last 7 days). Returns a compact summary: totals, the top targets, users and MCP clients, and the 25 per-target tool rows with the most failures, with success and failure counts — use it for which-tools-or-MCP-servers-are-failing questions. Use target_types=[\"tunneled_mcp_server\"] for questions like which tunneled MCPs the team uses. Set include_time_series=true for the time series, and include_breakdowns=true for the users-by-target and client tool breakdowns and every per-target tool row instead of the top 25.",
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
		MCPServerIDs:       nil,
		ShadowServerNames:  nil,
		MetaMcpServerIDs:   nil,
		HookSources:        nil,
		ClientKeys:         nil,
		IncludeTimeSeries:  false,
		IncludeBreakdowns:  false,
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if len(input.MCPServerIDs) > 100 {
		return fmt.Errorf("mcp_server_ids: at most 100 ids")
	}
	for _, id := range input.MCPServerIDs {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("mcp_server_ids: %q is not a UUID", id)
		}
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
		McpServerIds:       input.MCPServerIDs,
		ShadowServerNames:  input.ShadowServerNames,
		MetaMcpServerIds:   input.MetaMcpServerIDs,
		UserFilters:        nil,
		AgentIds:           nil,
		AccountType:        nil,
		HookSources:        input.HookSources,
		ClientKeys:         input.ClientKeys,
		// The MCP tool exposes no status, search or attribute filter, so the
		// summary it returns covers the whole window it was asked for.
		Statuses: nil,
		Query:    nil,
		Filters:  nil,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("get tool usage summary: %w", err)
	}

	// The time series and cross breakdowns are most of the result's size, and
	// an assistant re-reads every tool result on each later step of its turn,
	// so they are left out unless asked for.
	if !input.IncludeTimeSeries {
		result.TargetTimeSeries = nil
		result.UserTimeSeries = nil
	}
	if !input.IncludeBreakdowns {
		result.UsersByTarget = nil
		result.ClientToolBreakdown = nil
		result.TargetToolBreakdown = mostFailingToolRows(result.TargetToolBreakdown, compactToolRows)
	}

	return core.EncodeResult(wr, result)
}

// mostFailingToolRows keeps the n rows with the most failures, then the most
// calls, so a compact summary still names the tools that are failing.
func mostFailingToolRows(rows []*telemetry.ToolUsageTargetToolBreakdownRow, n int) []*telemetry.ToolUsageTargetToolBreakdownRow {
	if len(rows) <= n {
		return rows
	}
	sorted := slices.Clone(rows)
	slices.SortStableFunc(sorted, func(a, b *telemetry.ToolUsageTargetToolBreakdownRow) int {
		if c := cmp.Compare(b.FailureCount, a.FailureCount); c != 0 {
			return c
		}
		return cmp.Compare(b.EventCount, a.EventCount)
	})
	return sorted[:n]
}
