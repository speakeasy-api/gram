package chats

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type LoadChat struct {
	chat ChatService
}

// defaultLoadChatPageSize is the page size used when the caller does not request
// one. It matches the loadChat maximum so a single call returns as much as the
// endpoint allows; walk older history with before_seq.
const defaultLoadChatPageSize = 200

type loadChatInput struct {
	ID         string `json:"id" jsonschema:"The chat ID to load."`
	Generation *int   `json:"generation,omitempty" jsonschema:"Generation to load. Omit to receive the latest; walk from max_generation down to 0 to page through history."`
	Limit      *int   `json:"limit,omitempty" jsonschema:"Maximum messages to return for this page (default 200, max 200)."`
	BeforeSeq  *int64 `json:"before_seq,omitempty" jsonschema:"Keyset cursor: return messages with seq less than this (older messages). Use the seq of the oldest message you currently hold to page backward."`
	AfterSeq   *int64 `json:"after_seq,omitempty" jsonschema:"Keyset cursor: return messages with seq greater than this (newer messages). Use the seq of the newest message you currently hold to page forward."`
	RiskOnly   *bool  `json:"risk_only,omitempty" jsonschema:"When true, return only messages with active risk findings plus surrounding context."`
}

func NewLoadChatTool(chatSvc ChatService) *LoadChat {
	return &LoadChat{chat: chatSvc}
}

func (s *LoadChat) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "chats",
		HandlerName: "load_chat",
		Name:        "platform_load_chat",
		Description: "Load a chat's messages by ID. Returns the newest page of the latest generation by default; pass `before_seq`/`after_seq` to page within a generation, `generation` to walk history, or `risk_only` to fetch only risk findings with context.",
		InputSchema: core.BuildInputSchema[loadChatInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *LoadChat) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.chat == nil {
		return fmt.Errorf("chat service not configured")
	}

	input := loadChatInput{ID: "", Generation: nil, Limit: nil, BeforeSeq: nil, AfterSeq: nil, RiskOnly: nil}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}
	if input.ID == "" {
		return fmt.Errorf("id is required")
	}
	if input.Generation != nil && *input.Generation < 0 {
		return fmt.Errorf("generation must be >= 0")
	}

	limit := defaultLoadChatPageSize
	if input.Limit != nil {
		limit = *input.Limit
	}
	riskOnly := false
	if input.RiskOnly != nil {
		riskOnly = *input.RiskOnly
	}

	result, err := s.chat.LoadChat(ctx, &chat.LoadChatPayload{
		SessionToken:      nil,
		ProjectSlugInput:  nil,
		ChatSessionsToken: nil,
		ID:                input.ID,
		Generation:        input.Generation,
		Limit:             limit,
		BeforeSeq:         input.BeforeSeq,
		AfterSeq:          input.AfterSeq,
		RiskOnly:          riskOnly,
	})
	if err != nil {
		return fmt.Errorf("load chat: %w", err)
	}

	return core.EncodeResult(wr, result)
}
