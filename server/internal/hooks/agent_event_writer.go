package hooks

import (
	"context"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/eventsink"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

type AgentEventWriter struct {
	service *Service
}

func (s *Service) writeCursorTelemetry(ctx context.Context, ev agentevents.Event[*gen.CursorPayload]) {
	logs, err := eventsink.BuildTelemetryLogs(ev, providerDisplayName(ev.Provider()))
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to build Cursor telemetry events", attr.SlogError(err))
		return
	}
	if s.telemetryLogger == nil {
		return
	}
	for _, params := range logs {
		s.telemetryLogger.Log(ctx, params)
	}
}

func (s *Service) writeCursorChatMessages(ctx context.Context, ev agentevents.Event[*gen.CursorPayload]) {
	if ev.Context.ConversationID == "" {
		return
	}
	projectID, err := uuid.Parse(ev.Context.ProjectID)
	if err != nil {
		s.logger.ErrorContext(ctx, "Invalid project ID for Cursor conversation", attr.SlogError(err))
		return
	}
	source := providerDisplayName(ev.Provider())
	chatID := sessionIDToUUID(firstNonEmpty(ev.Context.ChatID, ev.Context.ConversationID))

	messages, err := eventsink.BuildChatMessages(ev, chatID, source)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to build Cursor conversation events", attr.SlogError(err))
		return
	}
	for _, message := range messages {
		if err := s.writeCursorChatMessage(ctx, ev, message, projectID, chatID, source); err != nil {
			s.logger.ErrorContext(ctx, "Failed to persist Cursor conversation event", attr.SlogError(err))
		}
	}
}

func (s *Service) writeCursorChatMessage(ctx context.Context, ev agentevents.Event[*gen.CursorPayload], message eventsink.ChatMessage, projectID uuid.UUID, chatID uuid.UUID, source string) error {
	metadata := &SessionMetadata{
		SessionID:   ev.Context.ConversationID,
		ServiceName: source,
		UserEmail:   ev.Context.UserEmail,
		UserID:      ev.Context.UserID,
		GramOrgID:   ev.Context.OrgID,
		ProjectID:   ev.Context.ProjectID,
	}

	if err := s.insertMessageWithFallbackUpsert(ctx, metadata, chatID, projectID, message.Params, defaultTitleForProvider(ev.Provider())); err != nil {
		return err
	}
	if message.ScheduleTitle && s.chatTitleGenerator != nil {
		if err := s.chatTitleGenerator.ScheduleChatTitleGeneration(
			context.WithoutCancel(ctx),
			chatID.String(),
			ev.Context.OrgID,
			ev.Context.ProjectID,
		); err != nil {
			s.logger.WarnContext(ctx, "failed to schedule chat title generation for agent event", attr.SlogError(err))
		}
	}
	return nil
}

func providerDisplayName(provider types.Provider) string {
	switch provider {
	case "cursor":
		return "Cursor"
	default:
		return string(provider)
	}
}

func defaultTitleForProvider(provider types.Provider) string {
	switch provider {
	case "cursor":
		return activities.DefaultCursorChatTitle
	default:
		return activities.DefaultClaudeAmbiguous
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
