package chat

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// GetMemory reports a session's standing in Organizational Memory to anyone
// who can read the session.
func (s *Service) GetMemory(ctx context.Context, payload *gen.GetMemoryPayload) (*gen.ChatMemory, error) {
	authCtx, chat, err := s.authorizeChatMemory(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	return s.chatMemory(ctx, authCtx, chat)
}

// SetMemoryExcluded keeps a session out of Organizational Memory or returns
// it. Excluding only ever narrows what memory shares, so anyone who can read
// the session may do it. Returning a session shares its summary with the
// team again, so a non-owner needs chat:write, the grant that already covers
// changing someone else's session.
func (s *Service) SetMemoryExcluded(ctx context.Context, payload *gen.SetMemoryExcludedPayload) (*gen.ChatMemory, error) {
	authCtx, chat, err := s.authorizeChatMemory(ctx, payload.ID)
	if err != nil {
		return nil, err
	}
	if !payload.Excluded {
		if err := s.authorizeChatAccess(ctx, authCtx, chat, chatAccessWrite, nil); err != nil {
			return nil, err
		}
	}
	if err := s.repo.SetChatMemoryExcluded(ctx, repo.SetChatMemoryExcludedParams{
		Excluded:  payload.Excluded,
		ID:        chat.ID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set chat memory exclusion").LogError(ctx, s.logger)
	}
	return s.chatMemory(ctx, authCtx, chat)
}

// authorizeChatMemory loads a chat the caller can read and requires the
// org-memory rollout. A disabled rollout reads as not found so the dashboard
// hides memory controls rather than showing an error.
func (s *Service) authorizeChatMemory(ctx context.Context, id string) (*contextvalues.AuthContext, repo.GetChatRow, error) {
	var none repo.GetChatRow
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, none, oops.C(oops.CodeUnauthorized)
	}
	chatID, err := uuid.Parse(id)
	if err != nil {
		return nil, none, oops.E(oops.CodeBadRequest, err, "invalid chat id")
	}
	chat, err := s.loadAuthorizedChat(ctx, authCtx, chatID, chatAccessRead)
	if err != nil {
		return nil, none, err
	}
	enabled, err := OrgMemoryFlag(ctx, s.db, s.flags, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, none, oops.E(oops.CodeUnexpected, err, "check organizational memory rollout").LogError(ctx, s.logger)
	}
	if !enabled {
		return nil, none, oops.E(oops.CodeNotFound, nil, "organizational memory is not enabled")
	}
	return authCtx, chat, nil
}

func (s *Service) chatMemory(ctx context.Context, authCtx *contextvalues.AuthContext, chat repo.GetChatRow) (*gen.ChatMemory, error) {
	row, err := s.repo.GetChatMemory(ctx, repo.GetChatMemoryParams{ID: chat.ID, ProjectID: *authCtx.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load chat memory").LogError(ctx, s.logger)
	}
	// Memos of an excluded or personal session are out of memory with it, so
	// they are not listed either.
	var memos []repo.ListChatMemoryMemosRow
	if row.MemoryState == "remembered" || row.MemoryState == "not_summarized" {
		memos, err = s.repo.ListChatMemoryMemos(ctx, repo.ListChatMemoryMemosParams{ChatID: uuid.NullUUID{UUID: chat.ID, Valid: true}, ProjectID: *authCtx.ProjectID})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list chat memos").LogError(ctx, s.logger)
		}
	}

	result := &gen.ChatMemory{
		State:                row.MemoryState,
		Summary:              nil,
		SummarizedAt:         nil,
		ExcludedAt:           formatOptionalTimestamptz(row.MemoryExcludedAt),
		Memos:                make([]*gen.ChatMemoryMemo, 0, len(memos)),
		DayDigestID:          conv.PtrEmpty(row.DayDigestID),
		DayDigestPeriodStart: nil,
		CanExclude:           true,
		CanInclude:           s.canWriteChat(ctx, authCtx, chat),
	}
	// Only the summary written from the masked transcript is what memory
	// reads; an older on-demand summary was built from the raw transcript.
	if row.SummaryRedacted && row.Summary.Valid {
		result.Summary = &row.Summary.String
		result.SummarizedAt = formatOptionalTimestamptz(row.SummaryGeneratedAt)
	}
	if row.DayDigestPeriodStart.Valid {
		result.DayDigestPeriodStart = new(row.DayDigestPeriodStart.Time.Format(time.DateOnly))
	}
	for _, m := range memos {
		result.Memos = append(result.Memos, &gen.ChatMemoryMemo{
			ID:        m.ID.String(),
			Content:   m.Content,
			CreatedAt: m.CreatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	return result, nil
}

// canWriteChat reports, without recording a denial, whether the caller owns
// the chat or holds chat:write on it.
func (s *Service) canWriteChat(ctx context.Context, authCtx *contextvalues.AuthContext, chat repo.GetChatRow) bool {
	if authCtx.UserID != "" && chat.UserID.Valid && chat.UserID.String == authCtx.UserID {
		return true
	}
	allowed, err := s.authz.Evaluate(ctx, authz.ChatWriteCheck(chat.ID.String()))
	return err == nil && allowed
}
