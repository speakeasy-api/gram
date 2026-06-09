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

type SearchChats struct {
	telemetry TelemetryService
}

type searchChatsInput struct {
	Filter *telemetry.SearchChatsFilter `json:"filter,omitempty" jsonschema:"Filter criteria to narrow the chat search."`
	Cursor *string                      `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Sort   string                       `json:"sort,omitempty" jsonschema:"Sort order for matching chats."`
	Limit  int                          `json:"limit,omitempty" jsonschema:"Number of results to return."`
}

func NewSearchChatsTool(telemetrySvc TelemetryService) *SearchChats {
	return &SearchChats{telemetry: telemetrySvc}
}

func (s *SearchChats) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "search_chats",
		Name:        "platform_search_chats",
		Description: "Search agent/assistant chat conversations for the current project.",
		InputSchema: core.BuildInputSchema[searchChatsInput](
			core.WithTypeSchema(reflect.TypeFor[telemetry.SearchChatsFilter](), core.PermissiveObjectSchema()),
			core.WithPropertyEnum("sort", "asc", "desc"),
			core.WithPropertyNumberRange("limit", 1, 1000),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *SearchChats) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := searchChatsInput{Filter: nil, Cursor: nil, Sort: "desc", Limit: 50}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Sort == "" {
		input.Sort = "desc"
	}

	result, err := s.telemetry.SearchChats(ctx, &telemetry.SearchChatsPayload{
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
		return fmt.Errorf("search chats: %w", err)
	}

	return core.EncodeResult(wr, result)
}
