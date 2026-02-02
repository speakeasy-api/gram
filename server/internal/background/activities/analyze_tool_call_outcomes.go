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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type AnalyzeToolCallOutcomes struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient *openrouter.ChatClient
}

func NewAnalyzeToolCallOutcomes(logger *slog.Logger, db *pgxpool.Pool, chatClient *openrouter.ChatClient) *AnalyzeToolCallOutcomes {
	return &AnalyzeToolCallOutcomes{
		logger:     logger,
		repo:       repo.New(db),
		chatClient: chatClient,
	}
}

type AnalyzeToolCallOutcomesArgs struct {
	ChatID    uuid.UUID
	ProjectID uuid.UUID
	OrgID     string
}

type toolCallOutcomeAnalysis struct {
	Outcome string `json:"outcome"`
	Notes   string `json:"notes"`
}

func (a *AnalyzeToolCallOutcomes) Do(ctx context.Context, args AnalyzeToolCallOutcomesArgs) error {
	// Get all messages for this chat ordered by creation time
	allMessages, err := a.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    args.ChatID,
		ProjectID: args.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("failed to list chat messages: %w", err)
	}

	// Get tool call result messages (role='tool')
	toolMessages, err := a.repo.GetToolCallMessages(ctx, args.ChatID)
	if err != nil {
		return fmt.Errorf("failed to get tool call messages: %w", err)
	}

	// If no tool calls, nothing to analyze
	if len(toolMessages) == 0 {
		return nil
	}

	// Build a map of messages by created_at for quick lookup
	messagesByTime := make(map[time.Time]repo.ChatMessage)
	for _, msg := range allMessages {
		messagesByTime[msg.CreatedAt.Time] = msg
	}

	// Analyze each tool call
	for _, toolMsg := range toolMessages {
		if err := a.analyzeToolCall(ctx, args.OrgID, toolMsg, allMessages); err != nil {
			a.logger.WarnContext(ctx, "failed to analyze tool call",
				attr.SlogError(err),
				slog.String("chat_message_id", toolMsg.ID.String()),
			)
			// Continue with other tool calls even if one fails
			continue
		}
	}

	return nil
}

func (a *AnalyzeToolCallOutcomes) analyzeToolCall(ctx context.Context, orgID string, toolMsg repo.ChatMessage, allMessages []repo.ChatMessage) error {
	// Find the index of this tool message
	toolMsgIndex := -1
	for i, msg := range allMessages {
		if msg.ID == toolMsg.ID {
			toolMsgIndex = i
			break
		}
	}

	if toolMsgIndex == -1 {
		return fmt.Errorf("tool message not found in all messages")
	}

	// Find the assistant message that made the tool call (should be right before)
	var assistantMsg *repo.ChatMessage
	for i := toolMsgIndex - 1; i >= 0; i-- {
		if allMessages[i].Role == "assistant" && allMessages[i].ToolCalls != nil {
			// Check if this assistant message contains the tool call ID
			var toolCalls []map[string]interface{}
			if err := json.Unmarshal(allMessages[i].ToolCalls, &toolCalls); err == nil {
				for _, tc := range toolCalls {
					if tcID, ok := tc["id"].(string); ok && tcID == toolMsg.ToolCallID.String {
						assistantMsg = &allMessages[i]
						break
					}
				}
			}
			if assistantMsg != nil {
				break
			}
		}
	}

	// Get the next 2-3 messages for context
	contextMessages := []repo.ChatMessage{}
	for i := toolMsgIndex + 1; i < len(allMessages) && i < toolMsgIndex+4; i++ {
		contextMessages = append(contextMessages, allMessages[i])
	}

	// Format the context for the LLM
	context := a.formatToolCallContext(assistantMsg, toolMsg, contextMessages)

	// Call LLM to analyze
	outcome, err := a.analyzeWithLLM(ctx, orgID, context)
	if err != nil {
		return fmt.Errorf("failed to analyze with LLM: %w", err)
	}

	// Update the tool message with the outcome
	err = a.repo.UpdateToolCallOutcome(ctx, repo.UpdateToolCallOutcomeParams{
		ID:               toolMsg.ID,
		ToolOutcome:      conv.ToPGText(outcome.Outcome),
		ToolOutcomeNotes: conv.ToPGText(outcome.Notes),
	})
	if err != nil {
		return fmt.Errorf("failed to update tool call outcome: %w", err)
	}

	return nil
}

func (a *AnalyzeToolCallOutcomes) formatToolCallContext(assistantMsg *repo.ChatMessage, toolMsg repo.ChatMessage, contextMessages []repo.ChatMessage) string {
	var sb strings.Builder

	if assistantMsg != nil {
		sb.WriteString("Tool Call Request:\n")
		// Parse tool_calls to extract the specific call
		var toolCalls []map[string]interface{}
		if err := json.Unmarshal(assistantMsg.ToolCalls, &toolCalls); err == nil {
			for _, tc := range toolCalls {
				if tcID, ok := tc["id"].(string); ok && tcID == toolMsg.ToolCallID.String {
					if function, ok := tc["function"].(map[string]interface{}); ok {
						name := function["name"]
						args := function["arguments"]
						sb.WriteString(fmt.Sprintf("Tool: %v\nArguments: %v\n\n", name, args))
					}
				}
			}
		}
	}

	sb.WriteString("Tool Response:\n")
	sb.WriteString(toolMsg.Content)
	sb.WriteString("\n\n")

	if len(contextMessages) > 0 {
		sb.WriteString("Subsequent Messages:\n")
		for _, msg := range contextMessages {
			sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Content))
		}
	}

	return sb.String()
}

func (a *AnalyzeToolCallOutcomes) analyzeWithLLM(ctx context.Context, orgID, toolContext string) (*toolCallOutcomeAnalysis, error) {
	systemPrompt := `You are analyzing whether a tool call in a conversation was successful.

A tool call is considered:
- SUCCESS: It executed without errors AND returned the desired information
- FAILURE: It executed but didn't return useful data (e.g., empty results due to bad filters, wrong parameters)
- PARTIAL: It returned some useful data but required follow-up or corrections

Even if a tool technically "succeeded" (no errors), if it returned empty/wrong data and required a corrective call, mark it as FAILURE.

Return your analysis as JSON with this exact structure:
{
  "outcome": "success|failure|partial",
  "notes": "brief explanation of why"
}`

	userPrompt := fmt.Sprintf("%s\n\nAnalyze this tool call and return JSON.", toolContext)

	analysisCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
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

	// Try to parse JSON from the response
	// Sometimes LLMs wrap JSON in code blocks, so try to extract it
	jsonStart := strings.Index(responseText, "{")
	jsonEnd := strings.LastIndex(responseText, "}")
	if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
		responseText = responseText[jsonStart : jsonEnd+1]
	}

	var analysis toolCallOutcomeAnalysis
	if err := json.Unmarshal([]byte(responseText), &analysis); err != nil {
		a.logger.WarnContext(ctx, "failed to parse LLM response as JSON",
			attr.SlogError(err),
			slog.String("llm_response", responseText),
		)
		// Return a default outcome if parsing fails
		return &toolCallOutcomeAnalysis{
			Outcome: "partial",
			Notes:   "Unable to analyze (LLM returned malformed JSON)",
		}, nil
	}

	// Validate the outcome field
	if analysis.Outcome != "success" && analysis.Outcome != "failure" && analysis.Outcome != "partial" {
		analysis.Outcome = "partial"
	}

	return &analysis, nil
}
