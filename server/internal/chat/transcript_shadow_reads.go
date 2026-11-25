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
// them untouched for every other project. A row with no shadow at all keeps its
// plaintext body, so messages written before capture began still load. A stale
// or unopenable shadow fails the read: it means capture or the data is broken,
// and plaintext must not paper over that.
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
		message, ok := opened[b.id]
		if !ok {
			continue
		}
		*b.content = message.Content
		*b.contentRaw = message.ContentRaw
		*b.toolCalls = message.ToolCalls
	}
	return nil
}
