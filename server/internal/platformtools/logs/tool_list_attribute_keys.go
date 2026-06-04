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

type ListAttributeKeys struct {
	telemetry TelemetryService
}

type listAttributeKeysInput struct {
	From string `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format. Defaults to 7 days ago."`
	To   string `json:"to,omitempty" jsonschema:"End time in ISO 8601 format. Defaults to now."`
}

func NewListAttributeKeysTool(telemetrySvc TelemetryService) *ListAttributeKeys {
	return &ListAttributeKeys{telemetry: telemetrySvc}
}

func (s *ListAttributeKeys) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "list_attribute_keys",
		Name:        "platform_list_attribute_keys",
		Description: "List the custom (@-prefixed) and system attribute keys present in the project's logs over a time window. Call this first to discover which attributes exist before filtering log or tool-call searches.",
		InputSchema: core.BuildInputSchema[listAttributeKeysInput](
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

func (s *ListAttributeKeys) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := listAttributeKeysInput{From: "", To: ""}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	from, to := defaultTimeWindow(input.From, input.To)

	result, err := s.telemetry.ListAttributeKeys(ctx, &telemetry.ListAttributeKeysPayload{
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
		return fmt.Errorf("list attribute keys: %w", err)
	}

	return encodeToolResult(wr, result)
}
