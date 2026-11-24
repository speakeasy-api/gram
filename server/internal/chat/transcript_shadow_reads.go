package chat

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// messageBody points at the body fields of one loaded message row, so a chat
// read can swap them for the opened shadow whatever query loaded the row.
type messageBody struct {
	id         uuid.UUID
	content    *string
	contentRaw *[]byte
	toolCalls  *[]byte
}

func chatMessageBody(m *repo.ChatMessage) messageBody {
	return messageBody{id: m.ID, content: &m.Content, contentRaw: &m.ContentRaw, toolCalls: &m.ToolCalls}
}

func riskWindowedMessageBody(m *repo.ListRiskWindowedMessagesRow) messageBody {
	return messageBody{id: m.ID, content: &m.Content, contentRaw: &m.ContentRaw, toolCalls: &m.ToolCalls}
}

func searchWindowedMessageBody(m *repo.ListSearchWindowedMessagesRow) messageBody {
	return messageBody{id: m.ID, content: &m.Content, contentRaw: &m.ContentRaw, toolCalls: &m.ToolCalls}
}

// openShadowRows replaces the body fields of a page of rows with their
// encrypted shadows when the caller's project reads through shadows, and leaves
// them untouched for every other project. A missing, stale or unopenable
// shadow fails the read: serving plaintext instead would hide a broken capture
// or an unfinished backfill.
func openShadowRows[T any](ctx context.Context, s *Service, authCtx *contextvalues.AuthContext, rows []T, body func(*T) messageBody) error {
	if s.transcriptShadow == nil || authCtx == nil || authCtx.ProjectID == nil || len(rows) == 0 {
		return nil
	}
	if _, enabled := s.transcriptShadowReadProjects[*authCtx.ProjectID]; !enabled {
		return nil
	}

	bodies := make([]messageBody, len(rows))
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		bodies[i] = body(&rows[i])
		ids[i] = bodies[i].id
	}

	opened, err := s.transcriptShadow.LoadMany(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, ids)
	if err != nil {
		return fmt.Errorf("open encrypted chat transcript: %w", err)
	}
	for _, b := range bodies {
		message := opened[b.id]
		*b.content = message.Content
		*b.contentRaw = message.ContentRaw
		*b.toolCalls = message.ToolCalls
	}
	return nil
}
