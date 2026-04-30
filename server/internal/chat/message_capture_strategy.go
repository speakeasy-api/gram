package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// ChatMessageCaptureStrategy captures completion messages to the database.
// It implements the MessageCaptureStrategy interface.
type ChatMessageCaptureStrategy struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	writer *ChatMessageWriter
}

var _ openrouter.MessageCaptureStrategy = (*ChatMessageCaptureStrategy)(nil)

// chatCaptureSession carries state between StartOrResumeChat and CaptureMessage
// so the sad path can flush user messages atomically alongside the assistant
// response.
type chatCaptureSession struct {
	generation int32
	// pendingRows are the user/tool rows that the walk built but that upfront
	// persistence failed to store. CaptureMessage flushes these atomically with
	// the assistant row. Empty on the happy path.
	pendingRows []chatMessageRow
}

// NewChatMessageCaptureStrategy creates a new ChatMessageCaptureStrategy.
func NewChatMessageCaptureStrategy(
	logger *slog.Logger,
	db *pgxpool.Pool,
	writer *ChatMessageWriter,
) *ChatMessageCaptureStrategy {
	return &ChatMessageCaptureStrategy{
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		writer: writer,
	}
}

func (s *ChatMessageCaptureStrategy) StartOrResumeChat(ctx context.Context, request openrouter.CompletionRequest) (openrouter.CaptureSession, error) {
	chatID := request.ChatID
	if chatID == uuid.Nil {
		return nil, nil // No chat ID, so no need to start or resume a chat
	}

	projectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse project ID").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "create chat")
	}

	matchResult, err := s.matchIncomingAgainstStored(ctx, chatID, request.Messages)
	if err != nil {
		return nil, err
	}

	generation := matchResult.generation
	matchedPrefix := matchResult.matchedPrefix
	if matchResult.diverged {
		// Compaction or edit: start a fresh generation. Old rows stay as
		// audit history.
		generation = matchResult.generation + 1
		matchedPrefix = 0
	}

	newMessages := request.Messages[matchedPrefix:]
	if len(newMessages) == 0 {
		return &chatCaptureSession{
			generation:  generation,
			pendingRows: nil,
		}, nil
	}

	rows := buildPendingRows(request, projectID, userID, externalUserID, newMessages, generation)

	if err := s.writer.WriteWithAssets(ctx, projectID, rows); err != nil {
		s.logger.ErrorContext(ctx, "failed to store chat messages", attr.SlogError(err))
		// Keep the rows on the session so CaptureMessage can flush them atomically
		// with the assistant response. We deliberately do not fail the request —
		// the proxy must still return a completion to the downstream client.
		return &chatCaptureSession{
			generation:  generation,
			pendingRows: rows,
		}, nil
	}

	return &chatCaptureSession{
		generation:  generation,
		pendingRows: nil,
	}, nil
}

type matchResult struct {
	generation    int32
	matchedPrefix int
	diverged      bool
}

// matchIncomingAgainstStored walks the current generation's stored messages
// against the incoming request to find the longest matching prefix by message
// slot identity. The first slot mismatch (or a stored row past the end of
// incoming) signals divergence and triggers a new generation.
func (s *ChatMessageCaptureStrategy) matchIncomingAgainstStored(ctx context.Context, chatID uuid.UUID, incoming []or.ChatMessages) (matchResult, error) {
	currentGen, err := s.repo.GetMaxGenerationForChat(ctx, chatID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get chat generation", attr.SlogError(err))
		return matchResult{}, oops.E(oops.CodeUnexpected, err, "get chat generation")
	}

	stored, err := s.repo.ListChatMessagesForMatch(ctx, repo.ListChatMessagesForMatchParams{
		ChatID:     chatID,
		Generation: currentGen,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list chat messages for match", attr.SlogError(err))
		return matchResult{}, oops.E(oops.CodeUnexpected, err, "list chat messages for match")
	}

	matchedPrefix := 0
	diverged := false
	for i, row := range stored {
		if i >= len(incoming) {
			diverged = true
			break
		}
		if slotFromStored(row.Role, row.Content, row.ToolCallID.String, row.ToolCalls) != slotFromIncoming(incoming[i]) {
			diverged = true
			break
		}
		matchedPrefix = i + 1
	}

	return matchResult{
		generation:    currentGen,
		matchedPrefix: matchedPrefix,
		diverged:      diverged,
	}, nil
}

// buildPendingRows turns the tail of the incoming messages into chatMessageRow
// values for the given generation.
func buildPendingRows(
	request openrouter.CompletionRequest,
	projectID uuid.UUID,
	userID, externalUserID string,
	newMessages []or.ChatMessages,
	generation int32,
) []chatMessageRow {
	rows := make([]chatMessageRow, len(newMessages))
	for i, msg := range newMessages {
		var toolCallID string
		if tc := openrouter.GetToolCallID(msg); tc != nil {
			toolCallID = *tc
		}

		// Persist assistant tool_use blocks alongside the row so the replay
		// path (loadChatHistory → runner normalize_history) reconstructs the
		// tool_use that pairs with the following tool_result. Dropping these
		// produces orphaned tool_results which Anthropic rejects with
		// "tool_use_id has no corresponding tool_use block".
		toolCallsJSON, err := assistantToolCallsJSON(msg)
		if err != nil {
			toolCallsJSON = nil
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
			chatID:           request.ChatID,
			userID:           userID,
			externalUserID:   externalUserID,
			messageID:        "",
			toolCallID:       toolCallID,
			role:             openrouter.GetRole(msg),
			model:            request.Model,
			content:          msg,
			metadata:         metadata,
			finishReason:     nil,
			toolCalls:        toolCallsJSON,
			promptTokens:     0,
			completionTokens: 0,
			totalTokens:      0,
			generation:       generation,
		}
	}
	return rows
}

// CaptureMessage stores a completion message in the database.
func (s *ChatMessageCaptureStrategy) CaptureMessage(
	ctx context.Context,
	rawSession openrouter.CaptureSession,
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

	session, err := s.resolveSession(ctx, rawSession, request)
	if err != nil {
		return err
	}

	assistantRows := buildAssistantRows(request, response, projectID, toolCallsJSON, origin, userAgent, ipAddress, session.generation)

	if len(session.pendingRows) == 0 {
		if _, err := s.writer.Write(ctx, projectID, assistantRows); err != nil {
			s.logger.ErrorContext(ctx, "failed to store chat message", attr.SlogError(err))
			return fmt.Errorf("store chat message: %w", err)
		}
		return nil
	}

	// Sad path: upfront persistence failed. Flush pending user/tool rows +
	// assistant row atomically so either the whole turn lands or none of it
	// does. A partial write would orphan the assistant and force divergence
	// detection to open a new generation on the next turn.
	if err := s.flushTurnAtomically(ctx, projectID, session.pendingRows, assistantRows); err != nil {
		return err
	}
	return nil
}

// buildAssistantRows turns a single completion response into one assistant row.
// The row preserves any narrative text and tool_calls from the model response;
// provider-specific replay normalization happens at the OpenRouter boundary.
func buildAssistantRows(
	request openrouter.CompletionRequest,
	response openrouter.CompletionResponse,
	projectID uuid.UUID,
	toolCallsJSON []byte,
	origin, userAgent, ipAddress string,
	generation int32,
) []repo.CreateChatMessageParams {
	// Whitespace-only content is treated as no text; preserving invisible
	// assistant narrative around tool calls does not add useful replay context.
	content := response.Content
	if strings.TrimSpace(content) == "" {
		content = ""
	}
	base := repo.CreateChatMessageParams{
		ChatID:           request.ChatID,
		Role:             "assistant",
		ProjectID:        projectID,
		Content:          "",
		ContentRaw:       nil,
		ContentAssetUrl:  conv.ToPGTextEmpty(""),
		StorageError:     conv.ToPGTextEmpty(""),
		Model:            conv.ToPGText(response.Model),
		MessageID:        conv.ToPGText(response.MessageID),
		ToolCallID:       conv.ToPGTextEmpty(""),
		UserID:           conv.ToPGText(request.UserID),
		ExternalUserID:   conv.ToPGText(request.ExternalUserID),
		FinishReason:     conv.ToPGTextEmpty(""),
		ToolCalls:        nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           conv.ToPGText(origin),
		UserAgent:        conv.ToPGText(userAgent),
		IpAddress:        conv.ToPGText(ipAddress),
		Source:           conv.ToPGText(string(request.UsageSource)),
		ContentHash:      nil,
		Generation:       generation,
	}

	finishReason := conv.PtrToPGText(response.FinishReason)
	promptTokens := int64(response.Usage.PromptTokens)
	completionTokens := int64(response.Usage.CompletionTokens)
	totalTokens := int64(response.Usage.TotalTokens)

	only := base
	only.Content = content
	only.ToolCalls = toolCallsJSON
	only.FinishReason = finishReason
	only.PromptTokens = promptTokens
	only.CompletionTokens = completionTokens
	only.TotalTokens = totalTokens

	return []repo.CreateChatMessageParams{only}
}

// resolveSession returns the session produced by StartOrResumeChat. If the
// caller did not supply one (older callers, unexpected nil), it falls back to
// a generation lookup so CaptureMessage remains correct.
func (s *ChatMessageCaptureStrategy) resolveSession(ctx context.Context, raw openrouter.CaptureSession, request openrouter.CompletionRequest) (*chatCaptureSession, error) {
	if raw != nil {
		sess, ok := raw.(*chatCaptureSession)
		if !ok {
			return nil, fmt.Errorf("capture session has unexpected type %T", raw)
		}
		return sess, nil
	}

	generation, err := s.repo.GetMaxGenerationForChat(ctx, request.ChatID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to get chat generation", attr.SlogError(err))
		return nil, fmt.Errorf("get chat generation: %w", err)
	}
	return &chatCaptureSession{
		generation:  generation,
		pendingRows: nil,
	}, nil
}

// flushTurnAtomically writes the pending user rows and the assistant rows in
// a single transaction so the turn lands as a unit.
func (s *ChatMessageCaptureStrategy) flushTurnAtomically(ctx context.Context, projectID uuid.UUID, pending []chatMessageRow, assistants []repo.CreateChatMessageParams) error {
	if err := s.writer.WriteTurn(ctx, projectID, pending, assistants); err != nil {
		s.logger.ErrorContext(ctx, "failed to flush chat turn", attr.SlogError(err))
		return fmt.Errorf("flush chat turn: %w", err)
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
) (openrouter.CaptureSession, error) {
	return nil, nil
}

// CaptureMessage does nothing and always returns nil.
func (s *NoOpCaptureStrategy) CaptureMessage(
	_ context.Context,
	_ openrouter.CaptureSession,
	_ openrouter.CompletionRequest,
	_ openrouter.CompletionResponse,
) error {
	return nil
}
