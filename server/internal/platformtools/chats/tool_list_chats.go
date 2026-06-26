package chats

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListChats struct {
	chat ChatService
}

type listChatsInput struct {
	Search         *string `json:"search,omitempty" jsonschema:"Substring matched against chat ID, external user ID, and title."`
	ExternalUserID *string `json:"external_user_id,omitempty" jsonschema:"Restrict results to chats produced by this external user ID."`
	AssistantID    *string `json:"assistant_id,omitempty" jsonschema:"Restrict results to chats produced by this assistant ID."`
	HasRisk        *string `json:"has_risk,omitempty" jsonschema:"Restrict to chats with ('true') or without ('false') risk findings."`
	Pinned         *string `json:"pinned,omitempty" jsonschema:"Restrict to pinned ('true') or unpinned ('false') chats."`
	From           *string `json:"from,omitempty" jsonschema:"Filter chats created at or after this ISO 8601 timestamp."`
	To             *string `json:"to,omitempty" jsonschema:"Filter chats created strictly before this ISO 8601 timestamp."`
	Limit          int     `json:"limit,omitempty" jsonschema:"Page size (1-100)."`
	Offset         int     `json:"offset,omitempty" jsonschema:"Pagination offset."`
	SortBy         string  `json:"sort_by,omitempty" jsonschema:"Field to sort by."`
	SortOrder      string  `json:"sort_order,omitempty" jsonschema:"Sort order."`
}

func NewListChatsTool(chatSvc ChatService) *ListChats {
	return &ListChats{chat: chatSvc}
}

func (s *ListChats) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "chats",
		HandlerName: "list_chats",
		Name:        "platform_list_chats",
		Description: "List chats for the current project, filterable by user, assistant, risk presence, and time window.",
		InputSchema: core.BuildInputSchema[listChatsInput](
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
			core.WithPropertyFormat("assistant_id", "uuid"),
			core.WithPropertyEnum("has_risk", "", "true", "false"),
			core.WithPropertyEnum("pinned", "", "true", "false"),
			core.WithPropertyEnum("sort_by", "created_at", "num_messages"),
			core.WithPropertyEnum("sort_order", "asc", "desc"),
			core.WithPropertyNumberRange("limit", 1, 100),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListChats) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.chat == nil {
		return fmt.Errorf("chat service not configured")
	}

	input := listChatsInput{
		Search:         nil,
		ExternalUserID: nil,
		AssistantID:    nil,
		HasRisk:        nil,
		Pinned:         nil,
		From:           nil,
		To:             nil,
		Limit:          50,
		Offset:         0,
		SortBy:         "created_at",
		SortOrder:      "desc",
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.SortBy == "" {
		input.SortBy = "created_at"
	}
	if input.SortOrder == "" {
		input.SortOrder = "desc"
	}

	result, err := s.chat.ListChats(ctx, &chat.ListChatsPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		Search:            input.Search,
		ExternalUserID:    input.ExternalUserID,
		AgentType:         nil,
		AssistantID:       input.AssistantID,
		HasRisk:           input.HasRisk,
		Pinned:            input.Pinned,
		MinRiskScore:      nil,
		From:              input.From,
		To:                input.To,
		Limit:             input.Limit,
		Offset:            input.Offset,
		SortBy:            input.SortBy,
		SortOrder:         input.SortOrder,
	})
	if err != nil {
		return fmt.Errorf("list chats: %w", err)
	}

	return core.EncodeResult(wr, result)
}
