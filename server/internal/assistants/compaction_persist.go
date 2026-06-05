package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// compactionMessageSource tags chat_messages rows the runner wrote post-
// compaction. Lets dashboards/queries distinguish a compacted-history
// generation from a user-initiated turn.
const compactionMessageSource = "assistant-compaction"

// RecordCompactedGeneration persists a runner-produced compacted transcript
// as a new chat_messages generation. The runner calls this after the
// in-process compactor finishes so that the next cold cron bootstrap loads
// the shorter history instead of re-reading the un-compacted prior
// generation.
//
// Caller is responsible for confirming the requesting principal is scoped
// to the thread's assistant — same contract as BuildThreadBootstrap.
func (s *ServiceCore) RecordCompactedGeneration(ctx context.Context, projectID, threadID, principalAssistantID uuid.UUID, messages []runtimeMessage) error {
	logAttrs := []slog.Attr{
		attr.SlogProjectID(projectID.String()),
		attr.SlogAssistantID(principalAssistantID.String()),
		attr.SlogAssistantThreadID(threadID.String()),
	}
	if s.chatWriter == nil {
		return oops.E(oops.CodeUnexpected, nil, "chat writer not configured").Log(ctx, s.logger, logAttrs...)
	}
	if len(messages) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "compacted transcript is empty").Log(ctx, s.logger, logAttrs...)
	}

	threadRow, err := assistantrepo.New(s.db).LoadAssistantThreadForBootstrap(ctx, assistantrepo.LoadAssistantThreadForBootstrapParams{
		ThreadID:  threadID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "assistant thread not found").Log(ctx, s.logger, logAttrs...)
		}
		return oops.E(oops.CodeUnexpected, err, "load assistant thread").Log(ctx, s.logger, logAttrs...)
	}
	if threadRow.AssistantID != principalAssistantID {
		return oops.E(oops.CodeForbidden, nil, "thread does not belong to assistant").Log(ctx, s.logger, logAttrs...)
	}

	chatRow, err := chatrepo.New(s.db).GetChat(ctx, threadRow.ChatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, nil, "assistant chat not found").Log(ctx, s.logger, logAttrs...)
		}
		return oops.E(oops.CodeUnexpected, err, "load assistant chat").Log(ctx, s.logger, logAttrs...)
	}

	currentGen, err := chatrepo.New(s.db).GetMaxGenerationForChat(ctx, threadRow.ChatID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load chat generation").Log(ctx, s.logger, logAttrs...)
	}
	nextGen := currentGen + 1

	params := make([]chatrepo.CreateChatMessageParams, 0, len(messages))
	for _, m := range messages {
		toolCallsJSON, err := encodeRuntimeToolCalls(m.ToolCalls)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "encode tool_calls").Log(ctx, s.logger, logAttrs...)
		}
		empty := pgtype.Text{String: "", Valid: false}
		params = append(params, chatrepo.CreateChatMessageParams{
			ChatID:           threadRow.ChatID,
			Role:             m.Role,
			ProjectID:        chatRow.ProjectID,
			Content:          m.Content,
			ContentRaw:       nil,
			ContentAssetUrl:  empty,
			StorageError:     empty,
			Model:            empty,
			MessageID:        empty,
			ToolCallID:       conv.ToPGTextEmpty(m.ToolCallID),
			UserID:           chatRow.UserID,
			ExternalUserID:   chatRow.ExternalUserID,
			FinishReason:     empty,
			ToolCalls:        toolCallsJSON,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           empty,
			UserAgent:        empty,
			IpAddress:        empty,
			Source:           conv.ToPGText(compactionMessageSource),
			ContentHash:      nil,
			Generation:       nextGen,
		})
	}

	if _, err := s.chatWriter.Write(ctx, chatRow.ProjectID, params); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write compacted chat messages").Log(ctx, s.logger, logAttrs...)
	}

	s.logger.LogAttrs(ctx, slog.LevelInfo, "assistant compacted generation persisted", logAttrs...)
	return nil
}

// encodeRuntimeToolCalls inverts decodePersistedToolCalls so a compacted
// assistant row's tool_calls JSONB matches the wire shape the capture
// path writes — `[]openrouter.ToolCall` with Type="function".
func encodeRuntimeToolCalls(calls []runtimeToolCall) ([]byte, error) {
	if len(calls) == 0 {
		return nil, nil
	}
	encoded := make([]openrouter.ToolCall, 0, len(calls))
	for _, c := range calls {
		encoded = append(encoded, openrouter.ToolCall{
			Index: 0,
			ID:    c.ID,
			Type:  "function",
			Function: openrouter.ToolCallFunction{
				Name:      c.Name,
				Arguments: c.Arguments,
			},
		})
	}
	out, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("marshal tool_calls: %w", err)
	}
	return out, nil
}
