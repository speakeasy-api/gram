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
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// DefaultChatTitle is the placeholder a chat is seeded with until the async
// title generator produces a real one. It must be a sentinel recognized by
// isDefaultChatTitle (background/activities/generate_chat_title.go), or the
// chat is treated as deliberately titled and never retitled.
const DefaultChatTitle = "New Chat"

// meterChatDroppedGenerations counts assistant generations dropped at capture
// because the model produced malformed tool_call arguments.
const meterChatDroppedGenerations = "chat.capture.dropped_generations"

// ChatMessageCaptureStrategy captures completion messages to the database.
// It implements the MessageCaptureStrategy interface.
type ChatMessageCaptureStrategy struct {
	logger             *slog.Logger
	db                 *pgxpool.Pool
	repo               *repo.Queries
	writer             *ChatMessageWriter
	droppedGenerations metric.Int64Counter
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
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	writer *ChatMessageWriter,
) *ChatMessageCaptureStrategy {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/chat")
	droppedGenerations, err := meter.Int64Counter(
		meterChatDroppedGenerations,
		metric.WithDescription("Assistant generations dropped at capture because the model produced malformed tool_call arguments"),
		metric.WithUnit("{generation}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterChatDroppedGenerations), attr.SlogError(err))
	}

	return &ChatMessageCaptureStrategy{
		logger:             logger,
		db:                 db,
		repo:               repo.New(db),
		writer:             writer,
		droppedGenerations: droppedGenerations,
	}
}

func (s *ChatMessageCaptureStrategy) StartOrResumeChat(ctx context.Context, request openrouter.CompletionRequest) (openrouter.CaptureSession, error) {
	chatID := request.ChatID
	if chatID == uuid.Nil {
		return nil, nil // No chat ID, so no need to start or resume a chat
	}

	projectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse project ID").LogError(ctx, s.logger)
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
		Title:          conv.ToPGText(DefaultChatTitle),
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

	// A client can replay a previous turn's aborted assistant message, whose
	// tool_call arguments are malformed JSON. Drop it here for the same reason
	// CaptureMessage drops the response: neither ingress may persist a row the
	// runner can't replay.
	newMessages := s.dropPoisonedAssistantMessages(ctx, request, projectID, request.Messages[matchedPrefix:])
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

// matchIncomingAgainstStored finds the longest matching prefix between
// stored and incoming by slot identity. Blank-assistant slots are skipped
// on both sides — the server may persist them while clients omit them on
// replay. Any other mismatch, or stored content past the end of incoming,
// signals divergence and triggers a new generation.
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

	storedSlotAt := func(i int) messageSlot {
		row := stored[i]
		return slotFromStored(row.Role, row.Content, row.ToolCallID.String, row.ToolCalls)
	}

	si, ii := 0, 0
	diverged := false
	for si < len(stored) && ii < len(incoming) {
		storedSlot := storedSlotAt(si)
		if storedSlot.isBlankAssistant() {
			si++
			continue
		}
		incomingSlot := slotFromIncoming(incoming[ii])
		if incomingSlot.isBlankAssistant() {
			ii++
			continue
		}
		if storedSlot != incomingSlot {
			diverged = true
			break
		}
		si++
		ii++
	}

	if !diverged {
		// Drain trailing blanks on both sides: a real stored row past
		// incoming must trip divergence, and phantom blanks at the head of
		// the new tail must not be re-persisted by buildPendingRows.
		for si < len(stored) && storedSlotAt(si).isBlankAssistant() {
			si++
		}
		for ii < len(incoming) && slotFromIncoming(incoming[ii]).isBlankAssistant() {
			ii++
		}
		if si < len(stored) {
			diverged = true
		}
	}

	return matchResult{
		generation:    currentGen,
		matchedPrefix: ii,
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

	// A truncated/aborted completion stream can leave a tool_call with
	// unterminated JSON arguments. The runner's normalize_history rejects such
	// a row on replay and the turn then retries without a cap, so the poison
	// must never enter the transcript. Drop the assistant row but still flush
	// the preceding user/tool input, keeping the transcript tail drivable.
	if toolName, ok := firstInvalidToolCall(response.ToolCalls); ok {
		s.recordDroppedGeneration(ctx, request.ChatID, projectID, toolName, "dropping assistant generation with malformed tool_call arguments")
		if len(session.pendingRows) > 0 {
			if err := s.writer.WriteWithAssets(ctx, projectID, session.pendingRows); err != nil {
				s.logger.ErrorContext(ctx, "failed to store chat input messages after dropping assistant generation", attr.SlogError(err))
				return fmt.Errorf("store chat input messages: %w", err)
			}
		}
		return nil
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

// firstInvalidToolCall mirrors the runner's normalize_history validation:
// empty arguments are treated as an empty object; anything else must be valid
// JSON. A truncated/aborted completion stream leaves unterminated JSON here.
func firstInvalidToolCall(toolCalls []openrouter.ToolCall) (string, bool) {
	for _, tc := range toolCalls {
		args := tc.Function.Arguments
		if args == "" {
			continue
		}
		if !json.Valid([]byte(args)) {
			return tc.Function.Name, true
		}
	}
	return "", false
}

// recordDroppedGeneration logs and counts a generation dropped at capture
// because the model produced malformed tool_call arguments.
func (s *ChatMessageCaptureStrategy) recordDroppedGeneration(ctx context.Context, chatID, projectID uuid.UUID, toolName, msg string) {
	s.logger.WarnContext(ctx, msg,
		attr.SlogChatID(chatID.String()),
		attr.SlogProjectID(projectID.String()),
		attr.SlogToolName(toolName),
	)
	if s.droppedGenerations != nil {
		s.droppedGenerations.Add(ctx, 1, metric.WithAttributes(
			attr.ProjectID(projectID.String()),
			attr.ToolName(toolName),
		))
	}
}

// dropPoisonedAssistantMessages removes incoming assistant messages whose
// tool_call arguments are malformed JSON, along with any tool results that pair
// with the dropped calls — keeping a tool_result without its tool_use orphans
// it and the model rejects the replay. This is the input-side counterpart to
// the drop in CaptureMessage.
func (s *ChatMessageCaptureStrategy) dropPoisonedAssistantMessages(ctx context.Context, request openrouter.CompletionRequest, projectID uuid.UUID, msgs []or.ChatMessages) []or.ChatMessages {
	droppedCallIDs := make(map[string]struct{})
	for _, msg := range msgs {
		calls := assistantToolCalls(msg)
		toolName, ok := firstInvalidToolCall(calls)
		if !ok {
			continue
		}
		for _, c := range calls {
			droppedCallIDs[c.ID] = struct{}{}
		}
		s.recordDroppedGeneration(ctx, request.ChatID, projectID, toolName, "dropping replayed assistant message with malformed tool_call arguments")
	}
	if len(droppedCallIDs) == 0 {
		return msgs
	}

	kept := make([]or.ChatMessages, 0, len(msgs))
	for _, msg := range msgs {
		if pairsWithDroppedCall(msg, droppedCallIDs) {
			continue
		}
		kept = append(kept, msg)
	}
	return kept
}

// pairsWithDroppedCall reports whether msg is a dropped assistant message (one
// of its tool_calls is in the set) or a tool result for one of those calls.
func pairsWithDroppedCall(msg or.ChatMessages, droppedCallIDs map[string]struct{}) bool {
	for _, c := range assistantToolCalls(msg) {
		if _, ok := droppedCallIDs[c.ID]; ok {
			return true
		}
	}
	if tcID := openrouter.GetToolCallID(msg); tcID != nil {
		if _, ok := droppedCallIDs[*tcID]; ok {
			return true
		}
	}
	return false
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
