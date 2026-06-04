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

type SearchToolCalls struct {
	telemetry TelemetryService
}

type searchToolCallsInput struct {
	Filter *telemetry.SearchToolCallsFilter `json:"filter,omitempty" jsonschema:"Filter criteria to narrow the tool-call search."`
	Cursor *string                          `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Sort   string                           `json:"sort,omitempty" jsonschema:"Sort order for matching tool calls."`
	Limit  int                              `json:"limit,omitempty" jsonschema:"Number of results to return."`
}

func NewSearchToolCallsTool(telemetrySvc TelemetryService) *SearchToolCalls {
	return &SearchToolCalls{telemetry: telemetrySvc}
}

func (s *SearchToolCalls) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "search_tool_calls",
		Name:        "platform_search_tool_calls",
		Description: "Search individual tool-call executions (MCP and agent tool invocations) for the current project.",
		InputSchema: core.BuildInputSchema[searchToolCallsInput](
			core.WithTypeSchema(reflect.TypeFor[telemetry.SearchToolCallsFilter](), core.PermissiveObjectSchema()),
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

func (s *SearchToolCalls) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := searchToolCallsInput{Filter: nil, Cursor: nil, Sort: "desc", Limit: 50}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Sort == "" {
		input.Sort = "desc"
	}

	result, err := s.telemetry.SearchToolCalls(ctx, &telemetry.SearchToolCallsPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Filter:           input.Filter,
		Cursor:           input.Cursor,
		Sort:             input.Sort,
		Limit:            input.Limit,
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("search tool calls: %w", err)
	}

	return encodeToolResult(wr, result)
}
