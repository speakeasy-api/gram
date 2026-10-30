package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	rememberToolName = "remember"
	// memoMaxRunes matches the memory_memos content check.
	memoMaxRunes = 4000
)

// RememberInput saves one fact to a project's team memory.
type RememberInput struct {
	ProjectID string `json:"project_id,omitempty"`

	ProjectSlug string `json:"project_slug,omitempty"`

	// Content is the fact to remember, such as a decision and its reason.
	Content string `json:"content"`

	// ChatID optionally names the chat the fact came from.
	ChatID string `json:"chat_id,omitempty"`
}

// TeamMemoryMemo is a stored memo as everyone with memory:read sees it.
type TeamMemoryMemo struct {
	Source TeamMemorySource `json:"source"`

	// Content is the stored, redacted text.
	Content string `json:"content"`

	AuthorUserID string `json:"author_user_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// RememberOutput is the memo as stored.
type RememberOutput struct {
	Memo TeamMemoryMemo `json:"memo"`

	// Redacted reports that detected secrets were masked before storing.
	Redacted bool `json:"redacted"`
}

// Remember redacts the fact, stores it as a memo attributed to the caller,
// and audits the write in the same transaction.
func (s *TeamMemoryService) Remember(ctx context.Context, principal Principal, input RememberInput) (RememberOutput, error) {
	var zero RememberOutput
	content := strings.TrimSpace(input.Content)
	if content == "" || !utf8.ValidString(content) || strings.ContainsRune(content, 0) || utf8.RuneCountInString(content) > memoMaxRunes {
		return zero, errTeamMemoryInvalid
	}
	chatID, err := parseOptionalUUID(input.ChatID)
	if err != nil {
		return zero, err
	}
	project, err := s.authorizeWrite(ctx, principal, input.ProjectID, input.ProjectSlug)
	if err != nil {
		return zero, err
	}
	safe, changed, err := s.redactor.Text(ctx, content)
	if err != nil {
		return zero, fmt.Errorf("redact memo: %w", err)
	}
	// Masks can lengthen text; refuse rather than truncate through a mask.
	if utf8.RuneCountInString(safe) > memoMaxRunes {
		return zero, errTeamMemoryInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin remember: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := memoryrepo.New(tx)
	if chatID.Valid {
		_, err := queries.GetTeamMemoryChat(ctx, memoryrepo.GetTeamMemoryChatParams{OrganizationID: principal.OrganizationID, ChatID: chatID.UUID, ProjectID: project.ID})
		if errors.Is(err, pgx.ErrNoRows) {
			return zero, ErrRiskReadNotFound
		}
		if err != nil {
			return zero, fmt.Errorf("get memo chat: %w", err)
		}
	}
	row, err := queries.InsertMemoryMemo(ctx, memoryrepo.InsertMemoryMemoParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, ChatID: chatID,
		AuthorUserID: conv.ToPGText(principal.UserID), Content: safe,
	})
	if err != nil {
		return zero, fmt.Errorf("insert memo: %w", err)
	}
	if err := s.audit.LogMemoryMemoCreate(ctx, tx, audit.LogMemoryMemoCreateEvent{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), ActorDisplayName: nil, ActorSlug: nil,
		MemoryMemoURN: urn.NewMemoryMemo(row.ID), ChatID: chatID,
	}); err != nil {
		return zero, fmt.Errorf("audit memo: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit remember: %w", err)
	}

	source := TeamMemorySource{Kind: TeamMemorySourceMemo, ID: row.ID.String(), ChatID: ""}
	if chatID.Valid {
		source.ChatID = chatID.UUID.String()
	}
	return RememberOutput{
		Memo:     TeamMemoryMemo{Source: source, Content: safe, AuthorUserID: principal.UserID, CreatedAt: row.CreatedAt.Time.UTC()},
		Redacted: changed,
	}, nil
}
