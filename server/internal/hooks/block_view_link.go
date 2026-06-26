package hooks

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

// toolCallBlockParams describes a hook-time block to persist. Only the reason
// and tenancy are required; the chat / finding / policy links are optional
// enrichment used by the dashboard.
type toolCallBlockParams struct {
	Provider       string
	OrganizationID string
	ProjectID      uuid.UUID
	Reason         string
	ToolName       string
	// UserID is the Gram user whose agent was blocked, used to authorize the
	// block page. Empty string when the user could not be resolved at deny time.
	UserID        string
	RiskPolicyID  uuid.NullUUID
	RiskResultID  uuid.NullUUID
	ChatID        uuid.NullUUID
	ChatMessageID uuid.NullUUID
}

// blockViewURL builds the durable block-page URL for a pre-minted block id. The
// id is minted on the hot path so the URL can go in the deny response
// immediately, while the backing row is inserted off the hot path (see
// insertToolCallBlock). Returns "" when no site URL is configured.
func (s *Service) blockViewURL(blockID uuid.UUID) string {
	if s.siteURL == nil {
		return ""
	}
	return s.siteURL.JoinPath("blocks", blockID.String()).String()
}

// insertToolCallBlock persists the durable block row for a pre-minted id. It is
// meant to run detached (the deny response doesn't wait on it); the row becomes
// visible to the block page within moments. Best-effort: logs and returns on
// failure.
func (s *Service) insertToolCallBlock(ctx context.Context, blockID uuid.UUID, p toolCallBlockParams) {
	if s.repo == nil || strings.TrimSpace(p.OrganizationID) == "" || p.ProjectID == uuid.Nil {
		return
	}
	if err := s.repo.InsertToolCallBlock(ctx, repo.InsertToolCallBlockParams{
		ID:             blockID,
		OrganizationID: p.OrganizationID,
		ProjectID:      p.ProjectID,
		Provider:       p.Provider,
		Reason:         strings.TrimSpace(p.Reason),
		ToolName:       conv.ToPGTextEmpty(p.ToolName),
		UserID:         strings.TrimSpace(p.UserID),
		RiskPolicyID:   p.RiskPolicyID,
		RiskResultID:   p.RiskResultID,
		ChatID:         p.ChatID,
		ChatMessageID:  p.ChatMessageID,
	}); err != nil {
		s.logger.WarnContext(ctx, "tool call block: failed to insert row",
			attr.SlogError(err),
			attr.SlogOrganizationID(p.OrganizationID),
			attr.SlogProjectID(p.ProjectID.String()),
		)
	}
}

// recordToolCallBlockAsync mints a block id, persists the block row off the hot
// path, and returns the durable block URL to append to the deny message. Use
// this from providers whose persistence already runs detached (Cursor, Codex);
// the page becomes valid within moments. Returns "" when it can't proceed.
func (s *Service) recordToolCallBlockAsync(ctx context.Context, p toolCallBlockParams) string {
	// Only mint a URL when the block row can actually be persisted; otherwise
	// the link would resolve to a /blocks/<id> page with no backing row. These
	// preconditions must mirror insertToolCallBlock's guard.
	if s.repo == nil || strings.TrimSpace(p.OrganizationID) == "" || p.ProjectID == uuid.Nil {
		return ""
	}
	blockID, err := uuid.NewV7()
	if err != nil {
		s.logger.ErrorContext(ctx, "tool call block: failed to generate id", attr.SlogError(err))
		return ""
	}
	go s.insertToolCallBlock(context.WithoutCancel(ctx), blockID, p)
	return s.blockViewURL(blockID)
}

// chatIDForBlock derives the chat a blocked tool call belongs to from its
// session/conversation id, using the same deterministic mapping as the hook PG
// write paths so the block row links to the chat the call is recorded under.
// Returns an invalid NullUUID when no session id is available.
func chatIDForBlock(sessionID string) uuid.NullUUID {
	if strings.TrimSpace(sessionID) == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: sessionIDToUUID(sessionID), Valid: true}
}

// appendBlockURL appends the durable block link to an agent-facing block
// message so the agent (and user) can open the page and leave feedback.
func appendBlockURL(message, blockURL string) string {
	if blockURL == "" {
		return message
	}
	return strings.TrimSpace(message) + "\n\nView information about why we blocked this request and leave feedback:\n" + blockURL
}
