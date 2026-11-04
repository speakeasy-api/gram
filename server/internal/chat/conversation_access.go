package chat

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// ReadableConversations filters bounded reference candidates without reading
// transcripts. Each chat's read permission, ownership included, is matched
// as one batch rather than as per-candidate refusals; the shared per-chat
// gate retains the runtime and external-token semantics.
func (s *Service) ReadableConversations(ctx context.Context, ids []string) ([]string, error) {
	visible := []string{}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.ProjectID == nil || len(ids) == 0 || authz.CheckSensitiveContentAccess(ctx) != nil {
		return visible, nil
	}
	chats := make([]repo.GetChatRow, 0, len(ids))
	ops := make([]authz.Operation, 0, len(ids))
	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("parse conversation reference: %w", err)
		}
		chat, err := s.repo.GetChat(ctx, repo.GetChatParams{ID: id, ProjectID: *auth.ProjectID})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load conversation reference: %w", err)
		}
		op, err := s.chatOperation(ctx, chat, chatAccessRead)
		if err != nil {
			return nil, fmt.Errorf("name conversation check: %w", err)
		}
		chats = append(chats, chat)
		ops = append(ops, op)
	}
	if len(ops) == 0 {
		return visible, nil
	}
	granted, err := s.authzClient.MatchMany(ctx, ops)
	if err != nil {
		return nil, fmt.Errorf("filter conversation grants: %w", err)
	}
	for i, chat := range chats {
		grant := granted[i]
		if err := s.authorizeChatAccess(ctx, auth, chat, chatAccessRead, &grant); err == nil {
			visible = append(visible, chat.ID.String())
		}
	}
	return visible, nil
}
