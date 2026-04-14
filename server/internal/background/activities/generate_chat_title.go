package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type GenerateChatTitle struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient openrouter.CompletionClient
}

func NewGenerateChatTitle(logger *slog.Logger, db *pgxpool.Pool, chatClient openrouter.CompletionClient) *GenerateChatTitle {
	return &GenerateChatTitle{
		logger:     logger,
		repo:       repo.New(db),
		chatClient: chatClient,
	}
}

type GenerateChatTitleArgs struct {
	ChatID    string
	OrgID     string
	ProjectID string
}

const (
	defaultChatTitle       = "New Chat"
	DefaultClaudeChatTitle = "Claude Code Session"
	DefaultCursorChatTitle = "Cursor Session"
)

func isDefaultChatTitle(title string) bool {
	return title == defaultChatTitle || title == DefaultClaudeChatTitle || title == DefaultCursorChatTitle
}

func (g *GenerateChatTitle) Do(ctx context.Context, args GenerateChatTitleArgs) error {
	chatID, err := uuid.Parse(args.ChatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	chat, err := g.repo.GetChat(ctx, chatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // chat was deleted, nothing to do
	}
	if err != nil {
		return fmt.Errorf("get chat: %w", err)
	}

	// Already has a meaningful title — nothing to do.
	if chat.Title.Valid && !isDefaultChatTitle(chat.Title.String) {
		return nil
	}

	// Build context from the first few user/assistant messages.
	messages, err := g.repo.ListChatMessages(ctx, repo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: chat.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("list chat messages: %w", err)
	}

	contextStr := buildTitleContext(messages)
	if len(contextStr) < 20 {
		return nil // Not enough context yet — will retry on next completion.
	}

	title := g.generateTitle(ctx, args.OrgID, chat.ProjectID.String(), contextStr)
	if title == defaultChatTitle {
		return nil
	}

	err = g.repo.UpdateChatTitle(ctx, repo.UpdateChatTitleParams{
		ID:    chatID,
		Title: conv.ToPGText(title),
	})
	if err != nil {
		return fmt.Errorf("update chat title: %w", err)
	}

	return nil
}

// buildTitleContext concatenates the last few user/assistant messages into a
// single string suitable for LLM title generation.
func buildTitleContext(messages []repo.ChatMessage) string {
	var b strings.Builder
	count := 0
	for i := len(messages) - 1; i >= 0; i-- { // Start from the last message and work backwards to make sure we capture the most recent messages
		msg := messages[i]
		if (msg.Role != "user" && msg.Role != "assistant") || strings.TrimSpace(msg.Content) == "" {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", msg.Role, strings.TrimSpace(msg.Content))
		count++
		if count >= 6 {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func (g *GenerateChatTitle) generateTitle(ctx context.Context, orgID, projectID string, conversationContext string) string {
	if g.chatClient == nil {
		return defaultChatTitle
	}

	titleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	systemPrompt := "Generate a concise title (3-6 words) for this conversation based on the messages below. " +
		"Return ONLY the title text, no quotes or explanation. " +
		"IMPORTANT: The title must directly relate to the content of the messages. " +
		"Do NOT expand, interpret, or replace abbreviations or acronyms — use the user's exact terminology. " +
		"If the conversation is a greeting, vague, or lacks a clear topic, return exactly: New Chat"

	response, err := g.chatClient.GetCompletion(titleCtx, openrouter.CompletionRequest{
		OrgID:     orgID,
		ProjectID: projectID,
		ChatID:    uuid.Nil,
		Messages: []or.Message{
			openrouter.CreateMessageSystem(systemPrompt),
			openrouter.CreateMessageUser(conversationContext),
		},
		Tools:          nil,
		Temperature:    nil,
		Model:          "",
		Stream:         false,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		APIKeyID:       "",
		JSONSchema:     nil,
	})
	if err != nil {
		g.logger.WarnContext(ctx, "failed to generate chat title via OpenRouter", attr.SlogError(err))
		return defaultChatTitle
	}

	title := strings.TrimSpace(openrouter.GetText(*response.Message))
	if title == "" {
		return defaultChatTitle
	}

	return title
}
