package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"io"
	"reflect"
)

type TelemetryService interface {
	SearchLogs(ctx context.Context, payload *telemetry.SearchLogsPayload) (*telemetry.SearchLogsResult, error)
}

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
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false

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
		Variables: nil,
		Annotations: &types.ToolAnnotations{
			Title:           nil,
			ReadOnlyHint:    &readOnly,
			DestructiveHint: &destructive,
			IdempotentHint:  &idempotent,
			OpenWorldHint:   &openWorld,
		},
		Managed:   true,
		OwnerKind: nil,
		OwnerID:   nil,
	}
}

func (s *SearchLogs) Call(ctx context.Context, payload io.Reader, wr io.Writer) error {
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

	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &input); err != nil { //nolint:musttag // telemetry filter types are Goa generated
			return fmt.Errorf("decode request body: %w", err)
		}
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
		return fmt.Errorf("search logs: %w", err)
	}

	if err := json.NewEncoder(wr).Encode(result); err != nil { //nolint:musttag // result is a Goa generated type
		return fmt.Errorf("encode response: %w", err)
	}

	return nil
}
