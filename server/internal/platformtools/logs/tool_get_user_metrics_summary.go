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

type GetUserMetricsSummary struct {
	telemetry TelemetryService
}

type getUserMetricsSummaryInput struct {
	From           string  `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format. Defaults to 7 days ago."`
	To             string  `json:"to,omitempty" jsonschema:"End time in ISO 8601 format. Defaults to now."`
	UserID         *string `json:"user_id,omitempty" jsonschema:"Internal user ID to get metrics for (mutually exclusive with external_user_id)."`
	ExternalUserID *string `json:"external_user_id,omitempty" jsonschema:"External user ID to get metrics for (mutually exclusive with user_id)."`
	EventSource    *string `json:"event_source,omitempty" jsonschema:"Optional event source filter (e.g. 'hook')."`
	HookSource     *string `json:"hook_source,omitempty" jsonschema:"Optional hook source filter (e.g. 'cursor', 'claude-code')."`
}

func NewGetUserMetricsSummaryTool(telemetrySvc TelemetryService) *GetUserMetricsSummary {
	return &GetUserMetricsSummary{telemetry: telemetrySvc}
}

func (s *GetUserMetricsSummary) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "get_user_metrics_summary",
		Name:        "platform_get_user_metrics_summary",
		Description: "Get an activity metrics summary for a single end user over a time window.",
		InputSchema: core.BuildInputSchema[getUserMetricsSummaryInput](
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetUserMetricsSummary) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := getUserMetricsSummaryInput{
		From:           "",
		To:             "",
		UserID:         nil,
		ExternalUserID: nil,
		EventSource:    nil,
		HookSource:     nil,
	}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	from, to := defaultTimeWindow(input.From, input.To)

	result, err := s.telemetry.GetUserMetricsSummary(ctx, &telemetry.GetUserMetricsSummaryPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             from,
		To:               to,
		UserID:           input.UserID,
		ExternalUserID:   input.ExternalUserID,
		EventSource:      input.EventSource,
		HookSource:       input.HookSource,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("get user metrics summary: %w", err)
	}

	return encodeToolResult(wr, result)
}
