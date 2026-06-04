package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/telemetry/telemetryerrs"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type SearchLogs struct {
	telemetry TelemetryService
}

type searchLogsInput struct {
	From    *string                     `json:"from,omitempty" jsonschema:"Start time in ISO 8601 format."`
	To      *string                     `json:"to,omitempty" jsonschema:"End time in ISO 8601 format."`
	Filters []*telemetry.LogFilter      `json:"filters,omitempty" jsonschema:"Attribute filters to narrow the log search."`
	Filter  *telemetry.SearchLogsFilter `json:"filter,omitempty" jsonschema:"Deprecated compatibility filter payload accepted by telemetry.searchLogs."`
	Cursor  *string                     `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Sort    string                      `json:"sort,omitempty" jsonschema:"Sort order for matching logs."`
	Limit   int                         `json:"limit,omitempty" jsonschema:"Number of results to return."`
}

func NewSearchLogsTool(telemetrySvc TelemetryService) *SearchLogs {
	return &SearchLogs{telemetry: telemetrySvc}
}

func (s *SearchLogs) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "search_logs",
		Name:        "platform_search_logs",
		Description: "Search and inspect telemetry logs for the current project.",
		InputSchema: core.BuildInputSchema[searchLogsInput](
			core.WithTypeSchema(reflect.TypeFor[telemetry.LogFilter](), core.PermissiveObjectSchema()),
			core.WithTypeSchema(reflect.TypeFor[telemetry.SearchLogsFilter](), core.PermissiveObjectSchema()),
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
			core.WithPropertyEnum("sort", "asc", "desc"),
			core.WithPropertyNumberRange("limit", 1, 1000),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *SearchLogs) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := searchLogsInput{
		From:    nil,
		To:      nil,
		Filters: nil,
		Filter:  nil,
		Cursor:  nil,
		Sort:    "desc",
		Limit:   50,
	}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Sort == "" {
		input.Sort = "desc"
	}

	result, err := s.telemetry.SearchLogs(ctx, &telemetry.SearchLogsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		From:             input.From,
		To:               input.To,
		Filters:          input.Filters,
		Filter:           input.Filter,
		Cursor:           input.Cursor,
		Limit:            input.Limit,
		Sort:             input.Sort,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("search logs: %w", err)
	}

	return encodeToolResult(wr, result)
}
