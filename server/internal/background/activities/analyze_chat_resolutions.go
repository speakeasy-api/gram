package activities

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type AnalyzeChatResolutions struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient *openrouter.ChatClient
	db         *pgxpool.Pool
}

func NewAnalyzeChatResolutions(logger *slog.Logger, db *pgxpool.Pool, chatClient *openrouter.ChatClient) *AnalyzeChatResolutions {
	return &AnalyzeChatResolutions{
		logger:     logger,
		repo:       repo.New(db),
		chatClient: chatClient,
		db:         db,
	}
}

type AnalyzeChatResolutionsArgs struct {
	ChatID    uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
}

type resolutionAnalysis struct {
	UserGoal        string `json:"user_goal"`
	Resolution      string `json:"resolution"`
	ResolutionNotes string `json:"resolution_notes"`
	Score           int    `json:"score"`
	MessageIndices  []int  `json:"message_indices"`
}

func (a *AnalyzeChatResolutions) Do(ctx context.Context, args AnalyzeChatResolutionsArgs) error {
	// Get all messages for this chat
	messages, err := a.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    args.ChatID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("failed to list chat messages: %w", err)
	}

	// If no messages, nothing to analyze
	if len(messages) == 0 {
		return nil
	}

	// Format messages for analysis (including tool outcomes)
	conversationText := a.formatMessagesWithOutcomes(messages)

	// Call LLM to analyze resolutions
	resolutions, err := a.analyzeWithLLM(ctx, args.OrgID, conversationText)
	if err != nil {
		return fmt.Errorf("failed to analyze with LLM: %w", err)
	}

	// If no resolutions identified, we're done
	if len(resolutions) == 0 {
		a.logger.InfoContext(ctx, "no resolutions identified for chat",
			attr.SlogChatID(args.ChatID.String()),
		)
		return nil
	}

	// Use a transaction to ensure atomicity
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			a.logger.WarnContext(ctx, "failed to rollback transaction", attr.SlogError(err))
		}
	}()

	txRepo := a.repo.WithTx(tx)

	// Delete existing resolutions for this chat (idempotency)
	if err := txRepo.DeleteChatResolutions(ctx, args.ChatID); err != nil {
		return fmt.Errorf("failed to delete existing resolutions: %w", err)
	}

	// Insert each resolution and its message associations
	for _, resolution := range resolutions {
		// Clamp score to valid range before conversion
		score := resolution.Score
		if score < 0 {
			score = 0
		}
		if score > 100 {
			score = 100
		}

		// Insert the resolution record
		resolutionID, err := txRepo.InsertChatResolution(ctx, repo.InsertChatResolutionParams{
			ProjectID:       args.ProjectID,
			ChatID:          args.ChatID,
			UserGoal:        resolution.UserGoal,
			Resolution:      resolution.Resolution,
			ResolutionNotes: resolution.ResolutionNotes,
			Score:           int32(score), // #nosec G115 - score is clamped to 0-100
		})
		if err != nil {
			return fmt.Errorf("failed to insert chat resolution: %w", err)
		}

		// Insert message associations
		for _, msgIndex := range resolution.MessageIndices {
			if msgIndex >= 0 && msgIndex < len(messages) {
				if err := txRepo.InsertChatResolutionMessage(ctx, repo.InsertChatResolutionMessageParams{
					ChatResolutionID: resolutionID,
					MessageID:        messages[msgIndex].ID,
				}); err != nil {
					return fmt.Errorf("failed to insert resolution message association: %w", err)
				}
			}
		}
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	a.logger.InfoContext(ctx, "successfully analyzed chat resolutions",
		attr.SlogChatID(args.ChatID.String()),
		slog.Int("resolutions_count", len(resolutions)),
	)

	return nil
}

func (a *AnalyzeChatResolutions) formatMessagesWithOutcomes(messages []repo.ChatMessage) string {
	var sb strings.Builder

	sb.WriteString("Conversation:\n\n")

	for i, msg := range messages {
		// Format: [index] role: content
		sb.WriteString(fmt.Sprintf("[%d] %s: %s", i, msg.Role, msg.Content))

		// If this is a tool message with an outcome, include it
		if msg.Role == "tool" && msg.ToolOutcome.Valid {
			sb.WriteString(fmt.Sprintf(" (outcome: %s)", msg.ToolOutcome.String))
		}

		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (a *AnalyzeChatResolutions) analyzeWithLLM(ctx context.Context, orgID, conversationText string) ([]resolutionAnalysis, error) {
	systemPrompt := `Analyze this chat conversation and identify all distinct user goals or queries that were addressed.
A single conversation may contain multiple unrelated goals.

For each distinct goal, determine:
- What the user was trying to accomplish
- Whether it was resolved (success/failure/partial/abandoned)
- Quality score 0-100 (factor in tool call efficiency - many failed tool calls = lower score)
- Which messages are relevant to this goal

Tool call outcomes are included - use them to inform your scoring:
- Many failed tool calls even if ultimately succeeded = moderate score (60-75)
- Efficient tool usage with success = high score (85-100)

Return a JSON array with this exact structure:
[
  {
    "user_goal": "description of what user wanted",
    "resolution": "success|failure|partial|abandoned",
    "resolution_notes": "free-form explanation including tool efficiency",
    "score": 85,
    "message_indices": [0, 1, 2, 3]
  }
]

Important:
- message_indices should be the 0-based indices from the conversation
- If only one goal in the entire chat, return an array with one item
- Identify ALL separate goals, even if multiple in one chat`

	userPrompt := fmt.Sprintf("%s\n\nAnalyze this conversation and return a JSON array of resolutions.", conversationText)

	analysisCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	msg, err := a.chatClient.GetCompletion(analysisCtx, orgID,
		systemPrompt,
		userPrompt,
		nil,
		billing.ModelUsageSourceChat,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM completion: %w", err)
	}

	responseText := strings.TrimSpace(openrouter.GetText(*msg))

	// Try to extract JSON array from the response
	// Sometimes LLMs wrap JSON in code blocks or add explanatory text
	jsonStart := strings.Index(responseText, "[")
	jsonEnd := strings.LastIndex(responseText, "]")
	if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
		responseText = responseText[jsonStart : jsonEnd+1]
	}

	var resolutions []resolutionAnalysis
	if err := json.Unmarshal([]byte(responseText), &resolutions); err != nil {
		a.logger.WarnContext(ctx, "failed to parse LLM response as JSON",
			attr.SlogError(err),
			slog.String("llm_response", responseText),
		)
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Validate and normalize resolution values
	for i := range resolutions {
		res := &resolutions[i]

		// Validate resolution field
		switch res.Resolution {
		case "success", "failure", "partial", "abandoned":
			// Valid values
		default:
			// Default to partial if invalid
			res.Resolution = "partial"
		}

		// Clamp score to 0-100
		if res.Score < 0 {
			res.Score = 0
		}
		if res.Score > 100 {
			res.Score = 100
		}
	}

	return resolutions, nil
}
