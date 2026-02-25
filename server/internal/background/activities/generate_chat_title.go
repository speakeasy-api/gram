package activities

import (
	"context"
	"database/sql"
	"errors"
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
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type GenerateChatTitle struct {
	logger     *slog.Logger
	repo       *repo.Queries
	chatClient *openrouter.ChatClient
}

func NewGenerateChatTitle(logger *slog.Logger, db *pgxpool.Pool, chatClient *openrouter.ChatClient) *GenerateChatTitle {
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

func (g *GenerateChatTitle) Do(ctx context.Context, args GenerateChatTitleArgs) error {
	chatID, err := uuid.Parse(args.ChatID)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %w", err)
	}

	// Get the first user message for title generation
	firstUserMessage, err := g.repo.GetFirstUserChatMessage(ctx, chatID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("no user messages found for chat: %w", err)
	case err != nil:
		return fmt.Errorf("failed to get first user message: %w", err)
	}

	// Generate the title
	title := g.generateTitle(ctx, args.OrgID, args.ProjectID, firstUserMessage)

	// Update the chat title in the database
	err = g.repo.UpdateChatTitle(ctx, repo.UpdateChatTitleParams{
		ID:    chatID,
		Title: conv.ToPGText(title),
	})
	if err != nil {
		return fmt.Errorf("failed to update chat title: %w", err)
	}

	return nil
}

func (g *GenerateChatTitle) generateTitle(ctx context.Context, orgID, projectID string, firstMessage string) string {
	if g.chatClient == nil {
		return "New Chat"
	}

	titleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	response, err := g.chatClient.GetCompletion(titleCtx, openrouter.CompletionRequest{
		OrgID:     orgID,
		ProjectID: projectID,
		Messages: []or.Message{
			openrouter.CreateMessageUser(firstMessage),
		},
		Tools:       nil,
		Temperature: nil,
		Model:       "",
		UsageSource: billing.ModelUsageSourceGram,
	})
	if err != nil {
		g.logger.WarnContext(ctx, "failed to generate chat title via OpenRouter", attr.SlogError(err))
		return "New Chat"
	}

	title := strings.TrimSpace(openrouter.GetText(*response.Message))
	if title == "" {
		return "New Chat"
	}

	return title
}
