package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// ChatMessageCaptureStrategy captures completion messages to the database.
// It implements the MessageCaptureStrategy interface.
type ChatMessageCaptureStrategy struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	assetStorage assets.BlobStore
}

var _ openrouter.MessageCaptureStrategy = (*ChatMessageCaptureStrategy)(nil)

// NewChatMessageCaptureStrategy creates a new ChatMessageCaptureStrategy.
func NewChatMessageCaptureStrategy(
	logger *slog.Logger,
	db *pgxpool.Pool,
	assetStorage assets.BlobStore,
) *ChatMessageCaptureStrategy {
	return &ChatMessageCaptureStrategy{
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		assetStorage: assetStorage,
	}
}

func (s *ChatMessageCaptureStrategy) StartOrResumeChat(ctx context.Context, request openrouter.CompletionRequest) error {
	chatID := request.ChatID
	if chatID == uuid.Nil {
		return nil // No chat ID, so no need to start or resume a chat
	}

	projectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse project ID").Log(ctx, s.logger)
	}
	orgID := request.OrgID
	userID := request.UserID
	externalUserID := request.ExternalUserID

	// Create chat with placeholder title - title generation happens via the generateTitle RPC
	_, err = s.repo.UpsertChat(ctx, repo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
		UserID:         conv.ToPGText(userID),
		ExternalUserID: conv.ToPGText(externalUserID),
		Title:          conv.ToPGText("New Chat"),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create chat", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "create chat")
	}

	// Load the current generation's stored messages and walk them against the
	// incoming request to detect divergence (client compaction or edit).
	currentGen, err := s.repo.GetMaxGenerationForChat(ctx, chatID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get chat generation", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "get chat generation")
	}

	stored, err := s.repo.ListChatMessagesForMatch(ctx, repo.ListChatMessagesForMatchParams{
		ChatID:     chatID,
		Generation: currentGen,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list chat messages for match", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "list chat messages for match")
	}

	var parentHash []byte
	matchedPrefix := 0
	diverged := false
	for i, row := range stored {
		if i >= len(request.Messages) {
			diverged = true
			break
		}
		storedHash := row.ContentHash
		if len(storedHash) == 0 {
			// Lazy backfill: historical rows have no hash. Compute from stored
			// fields and persist so subsequent turns skip this path.
			storedHash = chainMessageHash(parentHash, row.Role, row.Content, row.ToolCallID.String)
			if err := s.repo.BackfillChatMessageHash(ctx, repo.BackfillChatMessageHashParams{
				ID:          row.ID,
				ContentHash: storedHash,
			}); err != nil {
				s.logger.WarnContext(ctx, "failed to backfill chat message hash", attr.SlogError(err))
			}
		}
		if !bytes.Equal(storedHash, hashIncomingMessage(parentHash, request.Messages[i])) {
			diverged = true
			break
		}
		parentHash = storedHash
		matchedPrefix = i + 1
	}

	generation := currentGen
	if diverged {
		// Compaction or edit: start a fresh chain at a new generation. Old rows
		// stay as audit history.
		generation = currentGen + 1
		parentHash = nil
		matchedPrefix = 0
	}

	newMessages := request.Messages[matchedPrefix:]
	if len(newMessages) == 0 {
		return nil
	}

	rows := make([]chatMessageRow, len(newMessages))
	for i, msg := range newMessages {
		var toolCallID string
		if tc := openrouter.GetToolCallID(msg); tc != nil {
			toolCallID = *tc
		}

		metadata := httpMetadata{
			Source:    string(request.UsageSource),
			Origin:    "",
			UserAgent: "",
			IPAddress: "",
		}

		if request.HTTPMetadata != nil {
			metadata.Origin = request.HTTPMetadata.Origin
			metadata.UserAgent = request.HTTPMetadata.UserAgent
			metadata.IPAddress = request.HTTPMetadata.IPAddress
		}

		h := hashIncomingMessage(parentHash, msg)
		rows[i] = chatMessageRow{
			projectID:        projectID,
			chatID:           chatID,
			userID:           userID,
			externalUserID:   externalUserID,
			messageID:        "",
			toolCallID:       toolCallID,
			role:             openrouter.GetRole(msg),
			model:            request.Model,
			content:          msg,
			metadata:         metadata,
			finishReason:     nil,
			toolCalls:        nil,
			promptTokens:     0,
			completionTokens: 0,
			totalTokens:      0,
			contentHash:      h,
			generation:       generation,
		}
		parentHash = h
	}
	if err := storeMessages(ctx, s.logger, s.db, s.assetStorage, rows); err != nil {
		s.logger.ErrorContext(ctx, "failed to store chat messages", attr.SlogError(err))
	}

	return nil
}

// CaptureMessage stores a completion message in the database.
func (s *ChatMessageCaptureStrategy) CaptureMessage(
	ctx context.Context,
	request openrouter.CompletionRequest,
	response openrouter.CompletionResponse,
) error {
	// Skip if no chat ID
	if request.ChatID == uuid.Nil {
		return nil
	}

	// Convert tool calls to JSON
	var toolCallsJSON []byte
	if len(response.ToolCalls) > 0 {
		var err error
		toolCallsJSON, err = json.Marshal(response.ToolCalls)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to marshal tool calls", attr.SlogError(err))
		}
	}

	// Parse project ID
	projectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "invalid project ID", attr.SlogError(err))
		return fmt.Errorf("parse project ID: %w", err)
	}

	// Build HTTP metadata fields
	var origin, userAgent, ipAddress string
	if request.HTTPMetadata != nil {
		origin = request.HTTPMetadata.Origin
		userAgent = request.HTTPMetadata.UserAgent
		ipAddress = request.HTTPMetadata.IPAddress
	}

	// The assistant response extends whatever chain StartOrResumeChat just
	// wrote to - take the tip's generation and hash and link from there.
	var generation int32
	var parentHash []byte
	tip, err := s.repo.GetChatChainTip(ctx, request.ChatID)
	switch {
	case err == nil:
		generation = tip.Generation
		parentHash = tip.ContentHash
	case errors.Is(err, pgx.ErrNoRows):
		// First-ever message: no user turn reached StartOrResumeChat, so chain
		// starts fresh at generation 0.
	default:
		s.logger.ErrorContext(ctx, "failed to get chat chain tip", attr.SlogError(err))
		return fmt.Errorf("get chat chain tip: %w", err)
	}

	_, err = s.repo.CreateChatMessage(ctx, []repo.CreateChatMessageParams{{
		ChatID:           request.ChatID,
		Role:             "assistant",
		ProjectID:        projectID,
		Content:          response.Content,
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		Model:            conv.ToPGText(response.Model),
		MessageID:        conv.ToPGText(response.MessageID),
		ToolCallID:       conv.ToPGTextEmpty(""), // Empty for assistant messages
		UserID:           conv.ToPGText(request.UserID),
		ExternalUserID:   conv.ToPGText(request.ExternalUserID),
		FinishReason:     conv.PtrToPGText(response.FinishReason),
		ToolCalls:        toolCallsJSON,
		PromptTokens:     int64(response.Usage.PromptTokens),
		CompletionTokens: int64(response.Usage.CompletionTokens),
		TotalTokens:      int64(response.Usage.TotalTokens),
		Origin:           conv.ToPGText(origin),
		UserAgent:        conv.ToPGText(userAgent),
		IpAddress:        conv.ToPGText(ipAddress),
		Source:           conv.ToPGText(string(request.UsageSource)),
		ContentHash:      hashAssistantResponse(parentHash, response.Content),
		Generation:       generation,
	}})

	if err != nil {
		s.logger.ErrorContext(ctx, "failed to store chat message", attr.SlogError(err))
		return fmt.Errorf("store chat message: %w", err)
	}

	return nil
}

// NoOpCaptureStrategy is a message capture strategy that does nothing.
// It's useful for tests or background workflows where message capture is not needed.
type NoOpCaptureStrategy struct{}

// NewNoOpCaptureStrategy creates a new NoOpCaptureStrategy.
func NewNoOpCaptureStrategy() *NoOpCaptureStrategy {
	return &NoOpCaptureStrategy{}
}

// StartOrResumeChat does nothing.
func (s *NoOpCaptureStrategy) StartOrResumeChat(
	_ context.Context,
	_ openrouter.CompletionRequest,
) error {
	return nil
}

// CaptureMessage does nothing and always returns nil.
func (s *NoOpCaptureStrategy) CaptureMessage(
	_ context.Context,
	_ openrouter.CompletionRequest,
	_ openrouter.CompletionResponse,
) error {
	return nil
}
