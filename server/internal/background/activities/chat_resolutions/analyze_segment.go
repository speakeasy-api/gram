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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type AnalyzeSegment struct {
	logger           *slog.Logger
	repo             *repo.Queries
	chatClient       *openrouter.ChatClient
	db               *pgxpool.Pool
	telemetryService *telemetry.Service
}

func NewAnalyzeSegment(logger *slog.Logger, db *pgxpool.Pool, chatClient *openrouter.ChatClient, telemetryService *telemetry.Service) *AnalyzeSegment {
	return &AnalyzeSegment{
		logger:           logger,
		repo:             repo.New(db),
		chatClient:       chatClient,
		db:               db,
		telemetryService: telemetryService,
	}
}

type AnalyzeSegmentArgs struct {
	ChatID       uuid.UUID
	ProjectID    uuid.UUID
	OrgID        string
	APIKeyID     string
	StartIndex   int
	EndIndex     int
	UserFeedback []UserFeedback
}

type toolCallAnalysis struct {
	MessageIndex int    `json:"message_index"`
	Outcome      string `json:"outcome"`
	Notes        string `json:"notes"`
}

type segmentAnalysisResult struct {
	UserGoal        string             `json:"user_goal"`
	Resolution      string             `json:"resolution"`
	ResolutionNotes string             `json:"resolution_notes"`
	Score           int                `json:"score"`
	ToolCalls       []toolCallAnalysis `json:"tool_calls"`
}

func (a *AnalyzeSegment) Do(ctx context.Context, args AnalyzeSegmentArgs) error {
	allMessages, err := a.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    args.ChatID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("failed to list chat messages: %w", err)
	}

	// Extract the segment
	if args.StartIndex < 0 || args.EndIndex >= len(allMessages) || args.StartIndex > args.EndIndex {
		return fmt.Errorf("invalid segment indices: start=%d, end=%d, total=%d", args.StartIndex, args.EndIndex, len(allMessages))
	}

	// Check for existing user feedback
	var applicableUserFeedback []UserFeedback
	for _, feedback := range args.UserFeedback {
		if feedback.MessageIndex >= args.StartIndex && feedback.MessageIndex <= args.EndIndex {
			applicableUserFeedback = append(applicableUserFeedback, feedback)
		}
	}

	segmentMessages := allMessages[args.StartIndex : args.EndIndex+1]
	segmentText := a.formatSegment(segmentMessages)

	result, err := a.analyzeWithLLM(ctx, args.OrgID, args.ProjectID, segmentText, applicableUserFeedback)
	if err != nil {
		return fmt.Errorf("failed to analyze segment with LLM: %w", err)
	}

	// Use transaction for atomic updates
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

	// Update tool call outcomes
	for _, tc := range result.ToolCalls {
		// Convert relative index to absolute
		absoluteIndex := args.StartIndex + tc.MessageIndex
		if absoluteIndex >= 0 && absoluteIndex < len(allMessages) {
			msgID := allMessages[absoluteIndex].ID
			if err := txRepo.UpdateToolCallOutcome(ctx, repo.UpdateToolCallOutcomeParams{
				ID:               msgID,
				ToolOutcome:      conv.ToPGText(tc.Outcome),
				ToolOutcomeNotes: conv.ToPGText(tc.Notes),
			}); err != nil {
				a.logger.WarnContext(ctx, "failed to update tool call outcome",
					attr.SlogError(err),
					attr.SlogMessageID(msgID.String()),
				)
				// Continue with other updates
			}
		}
	}

	// Insert resolution
	score := min(max(result.Score, 0), 100)

	resolutionID, err := txRepo.InsertChatResolution(ctx, repo.InsertChatResolutionParams{
		ProjectID:       args.ProjectID,
		ChatID:          args.ChatID,
		UserGoal:        result.UserGoal,
		Resolution:      result.Resolution,
		ResolutionNotes: result.ResolutionNotes,
		Score:           int32(score), // #nosec G115 - score is clamped to 0-100
	})
	if err != nil {
		return fmt.Errorf("failed to insert chat resolution: %w", err)
	}

	// Link all messages in this segment to the resolution
	for i := args.StartIndex; i <= args.EndIndex && i < len(allMessages); i++ {
		if err := txRepo.InsertChatResolutionMessage(ctx, repo.InsertChatResolutionMessageParams{
			ChatResolutionID: resolutionID,
			MessageID:        allMessages[i].ID,
		}); err != nil {
			return fmt.Errorf("failed to insert resolution message association: %w", err)
		}
	}

	// Attach the resolution to the user feedback
	for _, fb := range applicableUserFeedback {
		err := txRepo.AddUserFeedbackChatResolution(ctx, repo.AddUserFeedbackChatResolutionParams{
			ID:               fb.ID,
			ChatResolutionID: uuid.NullUUID{UUID: resolutionID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("failed to add user feedback to chat resolution: %w", err)
		}
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	a.logger.InfoContext(ctx, "successfully analyzed segment",
		attr.SlogChatID(args.ChatID.String()),
	)

	if a.telemetryService == nil {
		return nil
	}

	attrs := map[attr.Key]any{
		attr.GenAIEvaluationNameKey:        "chat_resolution",
		attr.GenAIEvaluationScoreLabelKey:  result.Resolution,
		attr.GenAIEvaluationScoreValueKey:  score,
		attr.GenAIEvaluationExplanationKey: result.ResolutionNotes,
		attr.GenAIConversationIDKey:        args.ChatID.String(),
		attr.ProjectIDKey:                  args.ProjectID.String(),
		attr.OrganizationIDKey:             args.OrgID,
		attr.APIKeyIDKey:                   args.APIKeyID,
	}

	chatInfo, err := a.repo.GetChat(ctx, args.ChatID)
	if err == nil && chatInfo.CreatedAt.Valid {
		resolutionTimeSecs := time.Since(chatInfo.CreatedAt.Time).Seconds()

		attrs[attr.GenAIConversationDuration] = resolutionTimeSecs
	}

	a.telemetryService.CreateLog(telemetry.LogParams{
		Timestamp: time.Now(),
		ToolInfo: telemetry.ToolInfo{
			ID:             "",
			URN:            "",
			Name:           "chat_resolution",
			ProjectID:      args.ProjectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: args.OrgID,
		},
		Attributes: attrs,
	})

	return nil
}

func (a *AnalyzeSegment) formatSegment(messages []repo.ChatMessage) string {
	return formatChatMessages(messages)
}

func (a *AnalyzeSegment) analyzeWithLLM(
	ctx context.Context,
	orgID string,
	projectID uuid.UUID,
	segmentText string,
	userFeedback []UserFeedback,
) (*segmentAnalysisResult, error) {
	userFeedbackText := a.formatUserFeedback(userFeedback)
	systemPrompt := `Analyze this conversation segment comprehensively.

1. Identify the user's goal/intent in this segment
2. Determine if the goal was resolved and how (success/failure/partial/abandoned)
3. For any tool calls in the segment, analyze if they were successful:
   - SUCCESS: Executed without errors AND returned desired information
   - FAILURE: Executed but didn't return useful data (empty results, bad filters, wrong parameters)
   - PARTIAL: Returned some useful data but required follow-up
4. Calculate a quality score (0-100) based on:
   - Whether the user goal was achieved
   - Tool call efficiency (many failed tool calls = lower score)

Return structured JSON.`

	userPromptText := segmentText
	if userFeedbackText != "" {
		userPromptText = fmt.Sprintf(`%s

USER FEEDBACK: The user gave feedback on this chat. Take this feedback into account when analyzing the resolution status and quality score.
%s`, segmentText, userFeedbackText)
	}

	userPrompt := fmt.Sprintf(`%s

Analyze this conversation segment.
If there are no tool calls, return an empty array.`, userPromptText)

	analysisCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Define JSON schema for structured output
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_goal": map[string]any{
				"type":        "string",
				"description": "What the user was trying to accomplish in this segment",
			},
			"resolution": map[string]any{
				"type":        "string",
				"enum":        []string{"success", "failure", "partial", "abandoned"},
				"description": "How the user's goal was resolved",
			},
			"resolution_notes": map[string]any{
				"type":        "string",
				"description": "Free-form explanation including tool call efficiency. Include a breakdown of the score.",
			},
			"score": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"maximum":     100,
				"description": "Quality score 0-100. If the conversation could have been resolved in fewer steps/messages/tool calls, the score should be lower.",
			},
			"tool_calls": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"message_index": map[string]any{
							"type":        "integer",
							"description": "Index of the tool call message (relative to segment)",
						},
						"outcome": map[string]any{
							"type":        "string",
							"enum":        []string{"success", "failure", "partial"},
							"description": "Outcome of this tool call",
						},
						"notes": map[string]any{
							"type":        "string",
							"description": "Brief explanation of the outcome",
						},
					},
					"required":             []string{"message_index", "outcome", "notes"},
					"additionalProperties": false,
				},
				"description": "Analysis of each tool call in the segment",
			},
		},
		"required":             []string{"user_goal", "resolution", "resolution_notes", "score", "tool_calls"},
		"additionalProperties": false,
	}

	jsonSchemaConfig := or.JSONSchemaConfig{
		Name:        "segment_analysis",
		Schema:      schema,
		Description: nil,
		Strict:      nil,
	}

	msg, err := a.chatClient.GetObjectCompletion(
		analysisCtx,
		orgID,
		projectID.String(),
		"", // Use default model
		systemPrompt,
		userPrompt,
		jsonSchemaConfig,
		billing.ModelUsageSourceGram,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM completion: %w", err)
	}

	responseText := strings.TrimSpace(openrouter.GetText(*msg))

	var result segmentAnalysisResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Validate resolution field
	switch result.Resolution {
	case "success", "failure", "partial", "abandoned":
		// Valid
	default:
		result.Resolution = "partial"
	}

	// Validate tool call outcomes
	for i := range result.ToolCalls {
		tc := &result.ToolCalls[i]
		if tc.Outcome != "success" && tc.Outcome != "failure" && tc.Outcome != "partial" {
			tc.Outcome = "partial"
		}
	}

	return &result, nil
}

func (a *AnalyzeSegment) formatUserFeedback(userFeedback []UserFeedback) string {
	var feedbackText strings.Builder
	for _, fb := range userFeedback {
		feedbackText.WriteString(fmt.Sprintf(`User feedback at message index %d: %s\n`, fb.MessageIndex, fb.Resolution))
		if fb.ResolutionNotes != "" {
			feedbackText.WriteString(fmt.Sprintf(`-- User feedback notes: %s\n`, fb.ResolutionNotes))
		}
	}
	return feedbackText.String()
}
