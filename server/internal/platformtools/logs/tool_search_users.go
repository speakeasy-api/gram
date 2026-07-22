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

type SearchUsers struct {
	telemetry TelemetryService
}

type searchUsersInput struct {
	Filter   *telemetry.SearchUsersFilter `json:"filter,omitempty" jsonschema:"Filter criteria to narrow the user search."`
	UserType string                       `json:"user_type,omitempty" jsonschema:"Type of user identifier to group by."`
	GroupBy  string                       `json:"group_by,omitempty" jsonschema:"Grouping dimension for the results."`
	Cursor   *string                      `json:"cursor,omitempty" jsonschema:"Cursor for pagination (user identifier from the last item)."`
	Sort     string                       `json:"sort,omitempty" jsonschema:"Sort order for matching users."`
	Limit    int                          `json:"limit,omitempty" jsonschema:"Number of results to return."`
}

func NewSearchUsersTool(telemetrySvc TelemetryService) *SearchUsers {
	return &SearchUsers{telemetry: telemetrySvc}
}

func (s *SearchUsers) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "logs",
		HandlerName: "search_users",
		Name:        "platform_search_users",
		Description: "Search end users (agents/employees) observed in the current project's telemetry.",
		InputSchema: core.BuildInputSchema[searchUsersInput](
			core.WithTypeSchema(reflect.TypeFor[telemetry.SearchUsersFilter](), core.PermissiveObjectSchema()),
			core.WithPropertyEnum("user_type", "internal", "external"),
			core.WithPropertyEnum("group_by", "employee", "role"),
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

func (s *SearchUsers) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.telemetry == nil {
		return fmt.Errorf("telemetry service not configured")
	}

	input := searchUsersInput{Filter: nil, UserType: "internal", GroupBy: "employee", Cursor: nil, Sort: "desc", Limit: 50}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Sort == "" {
		input.Sort = "desc"
	}
	// user_type is required by the telemetry API; default to internal since the
	// Goa transport default we'd normally inherit is bypassed on direct calls.
	if input.UserType == "" {
		input.UserType = "internal"
	}
	if input.GroupBy == "" {
		input.GroupBy = "employee"
	}

	result, err := s.telemetry.SearchUsers(ctx, &telemetry.SearchUsersPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Filter:           input.Filter,
		UserType:         input.UserType,
		GroupBy:          input.GroupBy,
		Cursor:           input.Cursor,
		Sort:             input.Sort,
		Limit:            input.Limit,
		Metrics:          "full", // platform tool surfaces the complete per-user metrics
	})
	if err != nil {
		if errors.Is(err, telemetryerrs.ErrLogsDisabled) {
			return writeLogsDisabledResponse(wr)
		}
		return fmt.Errorf("search users: %w", err)
	}

	return core.EncodeResult(wr, result)
}
