package hooks

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
)

// toolCallBlockWriteTimeout bounds the detached block-row insert. Callers run it
// with a non-cancelable context (context.WithoutCancel) so the deny response
// doesn't wait on it, which means a stalled DB/network call would otherwise leak
// the goroutine indefinitely.
const toolCallBlockWriteTimeout = 10 * time.Second

// pgForeignKeyViolation is SQLSTATE 23503.
const pgForeignKeyViolation = "23503"

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
	ctx, cancel := context.WithTimeout(ctx, toolCallBlockWriteTimeout)
	defer cancel()
	params := repo.InsertToolCallBlockParams{
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
	}

	// The URL was already handed to the agent, so losing this row means the
	// user opens a block page that does not exist. The links are optional
	// enrichment (every one is ON DELETE SET NULL), and any of them can point
	// at a row that is not written yet: enforcement runs before the hook's
	// chat and finding rows are persisted, so a block early in a session
	// races its own chat. Drop whichever link the database rejects and keep
	// the block rather than the other way round. Bounded by the number of
	// optional links, since clearing one can reveal the next.
	var err error
	for attempt := 0; attempt <= blockEnrichmentLinkCount; attempt++ {
		if err = s.repo.InsertToolCallBlock(ctx, params); err == nil {
			return
		}
		dropped, ok := clearRejectedBlockLink(&params, err)
		if !ok {
			break
		}
		s.logger.WarnContext(ctx, "tool call block: dropping unresolvable link to keep the block",
			attr.SlogError(err),
			attr.SlogOrganizationID(p.OrganizationID),
			attr.SlogProjectID(p.ProjectID.String()),
			attr.SlogValueAny(map[string]any{"block_id": blockID.String(), "dropped_link": dropped}),
		)
	}
	s.logger.WarnContext(ctx, "tool call block: failed to insert row",
		attr.SlogError(err),
		attr.SlogOrganizationID(p.OrganizationID),
		attr.SlogProjectID(p.ProjectID.String()),
	)
}

// blockEnrichmentLinkCount is the number of optional foreign keys on
// tool_call_blocks, bounding the retry loop below.
const blockEnrichmentLinkCount = 4

// clearRejectedBlockLink clears the one optional foreign key a
// foreign-key-violation error names, reporting which. It returns false for any
// other error, and for a violation naming a link that is already null or a
// column that is not optional (organization_id, project_id) — those are real
// failures with nothing to salvage.
func clearRejectedBlockLink(params *repo.InsertToolCallBlockParams, err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgForeignKeyViolation {
		return "", false
	}
	switch pgErr.ConstraintName {
	case "tool_call_blocks_chat_id_fkey":
		if !params.ChatID.Valid {
			return "", false
		}
		params.ChatID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		return "chat_id", true
	case "tool_call_blocks_chat_message_id_fkey":
		if !params.ChatMessageID.Valid {
			return "", false
		}
		params.ChatMessageID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		return "chat_message_id", true
	case "tool_call_blocks_risk_result_id_fkey":
		if !params.RiskResultID.Valid {
			return "", false
		}
		params.RiskResultID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		return "risk_result_id", true
	case "tool_call_blocks_risk_policy_id_fkey":
		if !params.RiskPolicyID.Valid {
			return "", false
		}
		params.RiskPolicyID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		return "risk_policy_id", true
	default:
		return "", false
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
