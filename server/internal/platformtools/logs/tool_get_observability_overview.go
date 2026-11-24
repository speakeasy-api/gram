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

type GetObservabilityOverview struct {
	telemetry TelemetryService
}

type getObservabilityOverviewInput struct {
	From              string  `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format. Defaults to 7 days ago."`
	To                string  `json:"to,omitempty" jsonschema:"End time in ISO 8601 format. Defaults to now."`
	UserID            *string `json:"user_id,omitempty" jsonschema:"Optional internal user ID filter."`
	ExternalUserID    *string `json:"external_user_id,omitempty" jsonschema:"Optional external user ID filter."`
	APIKeyID          *string `json:"api_key_id,omitempty" jsonschema:"Optional API key ID filter."`
	ToolsetSlug       *string `json:"toolset_slug,omitempty" jsonschema:"Optional toolset/MCP server slug filter."`
	RemoteMcpServerID *string `json:"remote_mcp_server_id,omitempty" jsonschema:"Optional remote MCP server ID filter."`
	McpServerID       *string `json:"mcp_server_id,omitempty" jsonschema:"Optional MCP server ID filter (fronting server; spans both remote-backed and toolset-backed activity)."`
	MetaMcpServerID   *string `json:"meta_mcp_server_id,omitempty" jsonschema:"Optional gateway (meta MCP server) ID filter; scopes to traffic dispatched through that gateway."`
	EventSource       *string `json:"event_source,omitempty" jsonschema:"Optional event source filter (e.g. 'hook')."`
	HookSource        *string `json:"hook_source,omitempty" jsonschema:"Optional hook source filter (e.g. 'cursor', 'claude-code')."`
	IncludeTimeSeries bool    `json:"include_time_series,omitempty" jsonschema:"Include the bucketed time series (large: one row per bucket). Defaults to false; the summary, previous-period comparison and top tools answer most questions without it."`
}

func NewGetObservabilityOverviewTool(telemetrySvc TelemetryService) *GetObservabilityOverview {
	return &GetObservabilityOverview{telemetry: telemetrySvc}
}

func (s *GetObservabilityOverview) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "get_observability_overview",
		Name:        "platform_get_observability_overview",
		Description: "Get a compact observability overview for the current project over a time window (default: the last 7 days): chat, tool-call, failure, token and cost totals, the same totals for the previous period of equal length for trends, and the top tools by call count and by failure rate. Start here for how-is-the-project-doing, what-is-failing and spend questions. The bucketed time series is omitted unless include_time_series is true.",
		InputSchema: core.BuildInputSchema[getObservabilityOverviewInput](
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

func (s *GetObservabilityOverview) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	// Unlike the telemetry API, the tool leaves the time series out unless asked:
	// it is most of the result's size, and an assistant re-reads every tool
	// result on each later step of its turn.
	input := getObservabilityOverviewInput{
		From:              "",
		To:                "",
		UserID:            nil,
		ExternalUserID:    nil,
		APIKeyID:          nil,
		ToolsetSlug:       nil,
		RemoteMcpServerID: nil,
		McpServerID:       nil,
		MetaMcpServerID:   nil,
		EventSource:       nil,
		HookSource:        nil,
		IncludeTimeSeries: false,
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	from, to := defaultTimeWindow(input.From, input.To)

	result, err := s.telemetry.GetObservabilityOverview(ctx, &telemetry.GetObservabilityOverviewPayload{
		ApikeyToken:       nil,
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		From:              from,
		To:                to,
		UserID:            input.UserID,
		ExternalUserID:    input.ExternalUserID,
		APIKeyID:          input.APIKeyID,
		ToolsetSlug:       input.ToolsetSlug,
		RemoteMcpServerID: input.RemoteMcpServerID,
		MetaMcpServerID:   input.MetaMcpServerID,
		McpServerID:       input.McpServerID,
		EventSource:       input.EventSource,
		HookSource:        input.HookSource,
		AccountType:       nil,
		ExternalOrgID:     nil,
		IncludeTimeSeries: input.IncludeTimeSeries,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("get observability overview: %w", err)
	}

	return core.EncodeResult(wr, result)
}
