package resolution_activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type SegmentChat struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient *openrouter.ChatClient
}

func NewSegmentChat(logger *slog.Logger, db *pgxpool.Pool, chatClient *openrouter.ChatClient) *SegmentChat {
	return &SegmentChat{
		logger:     logger,
		repo:       repo.New(db),
		chatClient: chatClient,
	}
}

type SegmentChatArgs struct {
	ChatID       uuid.UUID
	ProjectID    uuid.UUID
	OrgID        string
	UserFeedback []UserFeedback // Will be used to inform segmentation
}

type ChatSegment struct {
	StartIndex int `json:"start_index"`
	EndIndex   int `json:"end_index"`
}

type SegmentChatOutput struct {
	Segments []ChatSegment
}

func (s *SegmentChat) Do(ctx context.Context, args SegmentChatArgs) (*SegmentChatOutput, error) {
	// Get all messages for this chat
	messages, err := s.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    args.ChatID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list chat messages: %w", err)
	}

	if len(messages) == 0 {
		return &SegmentChatOutput{
			Segments: []ChatSegment{},
		}, nil
	}

	// If very few messages, return single segment
	if len(messages) <= 3 {
		return &SegmentChatOutput{
			Segments: []ChatSegment{{StartIndex: 0, EndIndex: len(messages) - 1}},
		}, nil
	}

	feedbackIndices := make([]int, 0, len(args.UserFeedback))
	for _, fb := range args.UserFeedback {
		feedbackIndices = append(feedbackIndices, fb.MessageIndex)
	}

	// Format messages for segmentation
	conversationText := s.formatMessages(messages)

	// Call cheap model to identify breakpoints, passing feedback indices as hints
	segments, err := s.segmentWithLLM(ctx, args.OrgID, args.ProjectID, conversationText, len(messages), feedbackIndices)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to segment chat with LLM, using single segment",
			attr.SlogError(err),
			attr.SlogChatID(args.ChatID.String()),
		)
		// Fallback to single segment
		return &SegmentChatOutput{
			Segments: []ChatSegment{{StartIndex: 0, EndIndex: len(messages) - 1}},
		}, nil
	}

	// Validate segments
	validSegments := s.validateSegments(segments, len(messages))

	return &SegmentChatOutput{
		Segments: validSegments,
	}, nil
}

func (s *SegmentChat) formatMessages(messages []repo.ChatMessage) string {
	return formatChatMessages(messages)
}

func (s *SegmentChat) segmentWithLLM(ctx context.Context, orgID string, projectID uuid.UUID, conversationText string, numMessages int, feedbackIndices []int) ([]ChatSegment, error) {
	systemPrompt := `Analyze this conversation and identify natural breakpoints where distinct user goals/tasks begin and end.

Each segment should represent one cohesive user goal or query. Look for:
- Topic changes
- New user questions unrelated to previous discussion
- Natural completion points followed by new topics
- Significant time gaps between messages (e.g., hours or days) which often indicate new sessions or context switches

Pay special attention to time gaps shown in the messages. Large time gaps (e.g. >30 minutes) are strong indicators of natural breakpoints, even if the topic appears related.

Prefer more segments over fewer. A single segment might be just two messages: one question and one answer.

Return segments as an array of start/end message indices.`

	// Build feedback context if we have feedback message indices
	feedbackContext := ""
	if len(feedbackIndices) > 0 {
		feedbackContext = fmt.Sprintf("\n\nIMPORTANT: The user has provided explicit feedback at message indices %v. These messages very likely represent the end of distinct segments, so you should strongly consider creating segment boundaries at or near these indices.", feedbackIndices)
	}

	userPrompt := fmt.Sprintf(`%s

Identify the segments in this conversation. Return a JSON array:
[
  {
    "start_index": 0,
    "end_index": 5
  },
  {
    "start_index": 6,
    "end_index": 12
  }
]

There are %d messages total (indices 0-%d).%s`, conversationText, numMessages, numMessages-1, feedbackContext)

	analysisCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Define JSON schema for structured output
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"start_index": map[string]any{
					"type":        "integer",
					"description": "Starting message index (0-based)",
					"minimum":     0,
				},
				"end_index": map[string]any{
					"type":        "integer",
					"description": "Ending message index (0-based)",
					"minimum":     0,
				},
			},
			"required":             []string{"start_index", "end_index"},
			"additionalProperties": false,
		},
		"description": "Array of conversation segments",
	}

	jsonSchemaConfig := or.JSONSchemaConfig{
		Name:        "chat_segments",
		Schema:      schema,
		Description: nil,
		Strict:      nil,
	}

	// Use Haiku (cheaper model) for segmentation
	msg, err := s.chatClient.GetObjectCompletion(
		analysisCtx,
		orgID,
		projectID.String(),
		"anthropic/claude-haiku-4.5",
		systemPrompt,
		userPrompt,
		jsonSchemaConfig,
		billing.ModelUsageSourceGram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM completion: %w", err)
	}

	responseText := strings.TrimSpace(openrouter.GetText(*msg))

	var segments []ChatSegment
	if err := json.Unmarshal([]byte(responseText), &segments); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return segments, nil
}

func (s *SegmentChat) validateSegments(segments []ChatSegment, numMessages int) []ChatSegment {
	if len(segments) == 0 {
		// Return single segment covering everything
		return []ChatSegment{{StartIndex: 0, EndIndex: numMessages - 1}}
	}

	var valid []ChatSegment
	for _, seg := range segments {
		// Ensure indices are within bounds
		if seg.StartIndex < 0 {
			seg.StartIndex = 0
		}
		if seg.EndIndex >= numMessages {
			seg.EndIndex = numMessages - 1
		}
		if seg.StartIndex > seg.EndIndex {
			continue // Skip invalid segments
		}

		valid = append(valid, seg)
	}

	// If no valid segments, return single segment
	if len(valid) == 0 {
		return []ChatSegment{{StartIndex: 0, EndIndex: numMessages - 1}}
	}

	return valid
}
