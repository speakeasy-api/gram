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

type GetProjectMetricsSummary struct {
	telemetry TelemetryService
}

type getProjectMetricsSummaryInput struct {
	From string `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format. Defaults to 7 days ago."`
	To   string `json:"to,omitempty" jsonschema:"End time in ISO 8601 format. Defaults to now."`
}

func NewGetProjectMetricsSummaryTool(telemetrySvc TelemetryService) *GetProjectMetricsSummary {
	return &GetProjectMetricsSummary{telemetry: telemetrySvc}
}

func (s *GetProjectMetricsSummary) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "get_project_metrics_summary",
		Name:        "platform_get_project_metrics_summary",
		Description: "Get an aggregate activity metrics summary for the current project over a time window.",
		InputSchema: core.BuildInputSchema[getProjectMetricsSummaryInput](
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

func (s *GetProjectMetricsSummary) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := getProjectMetricsSummaryInput{From: "", To: ""}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	from, to := defaultTimeWindow(input.From, input.To)

	result, err := s.telemetry.GetProjectMetricsSummary(ctx, &telemetry.GetProjectMetricsSummaryPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             from,
		To:               to,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("get project metrics summary: %w", err)
	}

	return core.EncodeResult(wr, result)
}
