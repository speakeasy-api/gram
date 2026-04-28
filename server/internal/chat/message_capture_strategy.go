package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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
	messageStore *MessageStore
}

var _ openrouter.MessageCaptureStrategy = (*ChatMessageCaptureStrategy)(nil)

// chatCaptureSession carries state between StartOrResumeChat and CaptureMessage
// so the happy path skips a redundant chain-tip lookup and the sad path can
// flush the user messages atomically alongside the assistant response.
type chatCaptureSession struct {
	generation int32
	parentHash []byte
	// pendingRows are the user/tool rows that the walk built but that upfront
	// persistence failed to store. CaptureMessage flushes these atomically with
	// the assistant row. Empty on the happy path.
	pendingRows []chatMessageRow
}

// NewChatMessageCaptureStrategy creates a new ChatMessageCaptureStrategy.
func NewChatMessageCaptureStrategy(
	logger *slog.Logger,
	db *pgxpool.Pool,
	assetStorage assets.BlobStore,
	messageStore *MessageStore,
) *ChatMessageCaptureStrategy {
	return &ChatMessageCaptureStrategy{
		logger:       logger,
		db:           db,
		repo:         repo.New(db),
		assetStorage: assetStorage,
		messageStore: messageStore,
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
	parentHash := matchResult.parentHash
	matchedPrefix := matchResult.matchedPrefix
	if matchResult.diverged {
		// Compaction or edit: start a fresh chain at a new generation. Old rows
		// stay as audit history.
		generation = matchResult.generation + 1
		parentHash = nil
		matchedPrefix = 0
	}

	newMessages := request.Messages[matchedPrefix:]
	if len(newMessages) == 0 {
		return &chatCaptureSession{
			generation:  generation,
			parentHash:  parentHash,
			pendingRows: nil,
		}, nil
	}

	rows := buildPendingRows(request, projectID, userID, externalUserID, newMessages, parentHash, generation)
	// parentHash after the last new message becomes the chain tip for the assistant.
	tipHash := rows[len(rows)-1].contentHash

	if err := storeMessages(ctx, s.logger, s.db, s.assetStorage, rows); err != nil {
		s.logger.ErrorContext(ctx, "failed to store chat messages", attr.SlogError(err))
		// Keep the rows on the session so CaptureMessage can flush them atomically
		// with the assistant response. We deliberately do not fail the request —
		// the proxy must still return a completion to the downstream client.
		return &chatCaptureSession{
			generation:  generation,
			parentHash:  tipHash,
			pendingRows: rows,
		}, nil
	}

	s.messageStore.NotifyMessagesStored(ctx, projectID)

	return &chatCaptureSession{
		generation:  generation,
		parentHash:  tipHash,
		pendingRows: nil,
	}, nil
}

type matchResult struct {
	generation    int32
	parentHash    []byte
	matchedPrefix int
	diverged      bool
}

// matchIncomingAgainstStored walks the current generation's stored messages
// against the incoming request to find the longest matching prefix. It also
// lazily backfills content hashes on pre-migration rows. Returns the match
// state needed to decide whether to extend the chain or open a new generation.
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

	var parentHash []byte
	matchedPrefix := 0
	diverged := false
	for i, row := range stored {
		if i >= len(incoming) {
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
		if !bytes.Equal(storedHash, hashIncomingMessage(parentHash, incoming[i])) {
			diverged = true
			break
		}
		parentHash = storedHash
		matchedPrefix = i + 1
	}

	return matchResult{
		generation:    currentGen,
		parentHash:    parentHash,
		matchedPrefix: matchedPrefix,
		diverged:      diverged,
	}, nil
}

// buildPendingRows turns the tail of the incoming messages into chatMessageRow
// values with hashes chained off parentHash. The caller supplies the starting
// parent hash and generation.
func buildPendingRows(
	request openrouter.CompletionRequest,
	projectID uuid.UUID,
	userID, externalUserID string,
	newMessages []or.ChatMessages,
	parentHash []byte,
	generation int32,
) []chatMessageRow {
	rows := make([]chatMessageRow, len(newMessages))
	chain := parentHash
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

		h := hashIncomingMessage(chain, msg)
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
			toolCalls:        nil,
			promptTokens:     0,
			completionTokens: 0,
			totalTokens:      0,
			contentHash:      h,
			generation:       generation,
		}
		chain = h
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

	assistantRows := buildAssistantRows(request, response, projectID, toolCallsJSON, origin, userAgent, ipAddress, session.parentHash, session.generation)

	if len(session.pendingRows) == 0 {
		if _, err := s.repo.CreateChatMessage(ctx, assistantRows); err != nil {
			s.logger.ErrorContext(ctx, "failed to store chat message", attr.SlogError(err))
			return fmt.Errorf("store chat message: %w", err)
		}
		s.messageStore.NotifyMessagesStored(ctx, projectID)
		return nil
	}

	// Sad path: upfront persistence failed. Flush pending user/tool rows +
	// assistant row atomically so either the whole turn lands or none of it
	// does. A partial write would orphan the assistant and force divergence
	// detection to open a new generation on the next turn.
	if err := s.flushTurnAtomically(ctx, session.pendingRows, assistantRows); err != nil {
		return err
	}
	s.messageStore.NotifyMessagesStored(ctx, projectID)
	return nil
}

// buildAssistantRows turns a single completion response into 1 or 2 chained
// assistant rows. When the response carries both text content and tool_calls,
// it splits into a text-only row followed by a tool-calls-only row so the
// stored shape matches what NormalizeAssistantMessages produces on replay.
// Tokens and finish_reason land on the final row to keep per-turn accounting
// on a single row.
func buildAssistantRows(
	request openrouter.CompletionRequest,
	response openrouter.CompletionResponse,
	projectID uuid.UUID,
	toolCallsJSON []byte,
	origin, userAgent, ipAddress string,
	parentHash []byte,
	generation int32,
) []repo.CreateChatMessageParams {
	// Whitespace-only content is treated as "no text" to stay in sync with
	// NormalizeAssistantMessages, which trims before deciding whether to split
	// a replayed combined message. Any divergence here would hash the stored
	// rows against a different incoming shape and bump generation every turn.
	content := response.Content
	if strings.TrimSpace(content) == "" {
		content = ""
	}
	hasText := content != ""
	hasTools := len(toolCallsJSON) > 0

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

	if hasText && hasTools {
		text := base
		text.Content = content
		text.ContentHash = hashAssistantResponse(parentHash, content)

		tools := base
		tools.ToolCalls = toolCallsJSON
		tools.FinishReason = finishReason
		tools.PromptTokens = promptTokens
		tools.CompletionTokens = completionTokens
		tools.TotalTokens = totalTokens
		tools.ContentHash = hashAssistantResponse(text.ContentHash, "")

		return []repo.CreateChatMessageParams{text, tools}
	}

	only := base
	only.Content = content
	only.ToolCalls = toolCallsJSON
	only.FinishReason = finishReason
	only.PromptTokens = promptTokens
	only.CompletionTokens = completionTokens
	only.TotalTokens = totalTokens
	only.ContentHash = hashAssistantResponse(parentHash, content)

	return []repo.CreateChatMessageParams{only}
}

// resolveSession returns the session produced by StartOrResumeChat. If the
// caller did not supply one (older callers, unexpected nil), it falls back to
// a chain-tip lookup so CaptureMessage remains correct. The fallback path
// preserves the pre-session behavior.
func (s *ChatMessageCaptureStrategy) resolveSession(ctx context.Context, raw openrouter.CaptureSession, request openrouter.CompletionRequest) (*chatCaptureSession, error) {
	if raw != nil {
		sess, ok := raw.(*chatCaptureSession)
		if !ok {
			return nil, fmt.Errorf("capture session has unexpected type %T", raw)
		}
		return sess, nil
	}

	tip, err := s.repo.GetChatChainTip(ctx, request.ChatID)
	switch {
	case err == nil:
		return &chatCaptureSession{
			generation:  tip.Generation,
			parentHash:  tip.ContentHash,
			pendingRows: nil,
		}, nil
	case errors.Is(err, pgx.ErrNoRows):
		return &chatCaptureSession{
			generation:  0,
			parentHash:  nil,
			pendingRows: nil,
		}, nil
	default:
		s.logger.ErrorContext(ctx, "failed to get chat chain tip", attr.SlogError(err))
		return nil, fmt.Errorf("get chat chain tip: %w", err)
	}
}

// flushTurnAtomically writes the pending user rows (via storeMessages, which
// also handles asset-storage upload) and the assistant rows inside a single
// Postgres transaction.
func (s *ChatMessageCaptureStrategy) flushTurnAtomically(ctx context.Context, pending []chatMessageRow, assistants []repo.CreateChatMessageParams) error {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to begin transaction for catch-up flush", attr.SlogError(err))
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := storeMessages(ctx, s.logger, dbtx, s.assetStorage, pending); err != nil {
		return fmt.Errorf("store pending chat messages: %w", err)
	}

	txRepo := repo.New(dbtx)
	if _, err := txRepo.CreateChatMessage(ctx, assistants); err != nil {
		s.logger.ErrorContext(ctx, "failed to store assistant chat message", attr.SlogError(err))
		return fmt.Errorf("store assistant chat message: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit catch-up transaction: %w", err)
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
