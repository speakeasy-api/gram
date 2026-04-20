package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"go.opentelemetry.io/otel/trace"
)

// ChatMessageCaptureStrategy captures completion messages to the database.
// It implements the MessageCaptureStrategy interface.
type ChatMessageCaptureStrategy struct {
	logger       *slog.Logger
	db           *pgxpool.Pool
	repo         *repo.Queries
	assetStorage assets.BlobStore
	observers    []MessageObserver
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
		observers:    nil,
	}
}

// AddObserver registers a MessageObserver to be notified when messages are stored.
func (s *ChatMessageCaptureStrategy) AddObserver(obs MessageObserver) {
	s.observers = append(s.observers, obs)
}

func (s *ChatMessageCaptureStrategy) notifyObservers(ctx context.Context, projectID uuid.UUID) {
	// Dispatch observer notifications in a single goroutine to avoid spawning
	// one goroutine per observer. Observers run sequentially within the
	// goroutine — they are lightweight (e.g. signaling a Temporal workflow)
	// and should not block each other meaningfully.
	go func() { //nolint:gosec // intentionally detached from request context so observers survive request cancellation
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:contextcheck // intentionally detached from request context
		defer cancel()

		if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
			bgCtx = trace.ContextWithSpanContext(bgCtx, span.SpanContext())
		}

		for _, obs := range s.observers {
			obs.OnMessagesStored(bgCtx, projectID)
		}
	}()
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

	// Get the number of already-stored messages so we can insert any new ones
	chatCount, err := s.repo.CountChatMessages(ctx, chatID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get chat history", attr.SlogError(err))
		return oops.E(oops.CodeUnexpected, err, "count chat messages")
	}

	// This shouldn't happen, and also it doesn't really matter if it does, but we error anyway so we can fix it
	if int(chatCount) > len(request.Messages) {
		return oops.E(oops.CodeInvalid, nil, "chat history mismatch")
	}

	// If the stored chat history is shorter than the request, insert the missing messages
	// Most of the time, this just serves to store the new message the user just sent
	if int(chatCount) < len(request.Messages) {
		newMessages := request.Messages[int(chatCount):]
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
			}
		}
		if err := storeMessages(ctx, s.logger, s.db, s.assetStorage, rows); err != nil {
			s.logger.ErrorContext(ctx, "failed to store chat messages", attr.SlogError(err))
		} else {
			s.notifyObservers(ctx, projectID)
		}
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

	// Store message
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
	}})

	if err != nil {
		s.logger.ErrorContext(ctx, "failed to store chat message", attr.SlogError(err))
		return fmt.Errorf("store chat message: %w", err)
	}

	s.notifyObservers(ctx, projectID)

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
