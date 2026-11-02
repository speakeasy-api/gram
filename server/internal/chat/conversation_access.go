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
// transcripts. Grants are evaluated as one batch rather than per-candidate refusals;
// the shared per-chat gate retains owner, runtime and external-token semantics.
func (s *Service) ReadableConversations(ctx context.Context, ids []string) ([]string, error) {
	visible := []string{}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.ProjectID == nil || len(ids) == 0 || authz.CheckSensitiveContentAccess(ctx) != nil {
		return visible, nil
	}
	checks := make([]authz.Check, len(ids))
	for i, id := range ids {
		checks[i] = authz.ChatReadCheck(id)
	}
	granted, err := s.authz.Filter(ctx, checks)
	if err != nil {
		return nil, fmt.Errorf("filter conversation grants: %w", err)
	}
	allowed := make(map[string]bool, len(granted))
	for _, id := range granted {
		allowed[id] = true
	}
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
		grant := allowed[raw]
		if err := s.authorizeChatAccess(ctx, auth, chat, chatAccessRead, &grant); err == nil {
			visible = append(visible, raw)
		}
	}
	return visible, nil
}
