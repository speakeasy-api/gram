package activities

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	claudePromptCorrelationMinFuzzyLength = 40
	claudePromptCorrelationMinSimilarity  = 0.95
	claudePromptCorrelationMinScoreGap    = 0.02
	claudePromptCorrelationMaxTimeDelta   = 10 * time.Minute

	// claudePromptCorrelationLookback bounds how far back unlinked messages are
	// considered. It comfortably exceeds claudePromptCorrelationMaxTimeDelta plus
	// telemetry ingestion lag, so a message older than this can no longer match a
	// prompt and re-scanning it on every run would be wasted work.
	claudePromptCorrelationLookback = 1 * time.Hour

	// claudePromptCorrelationMatchTimeout bounds the ClickHouse candidate query
	// for a single message so one slow query cannot consume the whole activity
	// budget.
	claudePromptCorrelationMatchTimeout = 5 * time.Second
)

type CorrelateClaudePrompts struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	chConn clickhouse.Conn
}

func NewCorrelateClaudePrompts(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn) *CorrelateClaudePrompts {
	return &CorrelateClaudePrompts{
		logger: logger.With(attr.SlogComponent("correlate_claude_prompts")),
		db:     db,
		chConn: chConn,
	}
}

type CorrelateClaudePromptsArgs struct {
	ProjectID uuid.UUID
	ChatID    uuid.UUID
	SessionID string
}

type claudeUnlinkedUserMessage struct {
	id        uuid.UUID
	content   string
	createdAt time.Time
}

func (c *CorrelateClaudePrompts) Do(ctx context.Context, args CorrelateClaudePromptsArgs) error {
	messages, err := c.listUnlinkedUserMessages(ctx, args.ProjectID, args.ChatID)
	if err != nil {
		return fmt.Errorf("list unlinked Claude user messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	deadline, hasDeadline := ctx.Deadline()

	var cursor claudePromptCorrelationCursor
	for _, message := range messages {
		// Stop before the activity deadline so a partially drained backlog
		// completes successfully and the remainder is picked up by a later run,
		// rather than the whole activity failing and re-running from scratch.
		if hasDeadline && time.Until(deadline) <= claudePromptCorrelationMatchTimeout {
			break
		}

		match, ok, err := c.findClaudeUserPromptMatch(ctx, args.ProjectID, args.ChatID, args.SessionID, message, cursor)
		if err != nil {
			// One message's correlation failing (typically a slow ClickHouse
			// query) must not abort the run: that would leave the backlog
			// unprocessed and, because a run is scheduled per prompt event, spin
			// into a retry storm. Skip it and keep draining the rest.
			c.logger.WarnContext(ctx, "skipped Claude prompt correlation for message",
				attr.SlogChatID(args.ChatID.String()),
				attr.SlogMessageID(message.id.String()),
				attr.SlogError(err),
			)
			continue
		}
		if !ok {
			continue
		}

		if err := c.backfillMessagePromptID(ctx, args.ProjectID, args.ChatID, message.id, match.PromptID); err != nil {
			c.logger.WarnContext(ctx, "skipped Claude prompt id backfill for message",
				attr.SlogChatID(args.ChatID.String()),
				attr.SlogMessageID(message.id.String()),
				attr.SlogError(err),
			)
			continue
		}
		cursor.eventSequence = match.EventSequence
		cursor.timeUnixNano = match.TimeUnixNano
	}

	return nil
}

func (c *CorrelateClaudePrompts) listUnlinkedUserMessages(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID) ([]claudeUnlinkedUserMessage, error) {
	rows, err := activitiesrepo.New(c.db).ListUnlinkedClaudeUserMessagesForCorrelation(ctx, activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams{
		ChatID:       chatID,
		ProjectID:    conv.ToNullUUID(projectID),
		CreatedAfter: conv.ToPGTimestamptz(time.Now().Add(-claudePromptCorrelationLookback)),
	})
	if err != nil {
		return nil, fmt.Errorf("query unlinked user messages: %w", err)
	}

	messages := make([]claudeUnlinkedUserMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, claudeUnlinkedUserMessage{
			id:        row.ID,
			content:   row.Content,
			createdAt: row.CreatedAt.Time,
		})
	}
	return messages, nil
}

type claudePromptCorrelationCursor struct {
	eventSequence int64
	timeUnixNano  int64
}

func (c *CorrelateClaudePrompts) findClaudeUserPromptMatch(
	ctx context.Context,
	projectID uuid.UUID,
	chatID uuid.UUID,
	sessionID string,
	message claudeUnlinkedUserMessage,
	cursor claudePromptCorrelationCursor,
) (telemetryrepo.ClaudeUserPromptCandidate, bool, error) {
	var noMatch telemetryrepo.ClaudeUserPromptCandidate

	messagePrompt := normalizePromptForCorrelation(message.content)
	if messagePrompt == "" {
		return noMatch, false, nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, claudePromptCorrelationMatchTimeout)
	defer cancel()

	candidates, err := telemetryrepo.New(c.chConn).ListClaudeUserPromptCandidatesForCorrelation(queryCtx, telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams{
		GramProjectID:          projectID.String(),
		GramChatID:             chatID.String(),
		SessionID:              sessionID,
		MessagePrompt:          messagePrompt,
		MessageTimeUnixNano:    message.createdAt.UnixNano(),
		AfterEventSequence:     cursor.eventSequence,
		AfterEventTimeUnixNano: cursor.timeUnixNano,
		MinFuzzyLength:         claudePromptCorrelationMinFuzzyLength,
		MaxTimeDeltaNanos:      claudePromptCorrelationMaxTimeDelta.Nanoseconds(),
	})
	if err != nil {
		return noMatch, false, fmt.Errorf("query Claude user prompt candidates: %w", err)
	}
	if len(candidates) == 0 {
		return noMatch, false, nil
	}
	if isAcceptedClaudePromptCandidate(candidates) {
		return candidates[0], true, nil
	}
	return noMatch, false, nil
}

func (c *CorrelateClaudePrompts) backfillMessagePromptID(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, messageID uuid.UUID, promptID string) error {
	if strings.TrimSpace(promptID) == "" {
		return nil
	}
	err := activitiesrepo.New(c.db).BackfillClaudeUserMessagePromptID(ctx, activitiesrepo.BackfillClaudeUserMessagePromptIDParams{
		PromptID:  conv.ToPGText(promptID),
		MessageID: messageID,
		ChatID:    chatID,
		ProjectID: conv.ToNullUUID(projectID),
	})
	if err != nil {
		return fmt.Errorf("update chat message prompt ID: %w", err)
	}
	return nil
}

func normalizePromptForCorrelation(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func isAcceptedClaudePromptCandidate(candidates []telemetryrepo.ClaudeUserPromptCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	best := candidates[0]
	if best.IsExact {
		return true
	}
	if best.Similarity < claudePromptCorrelationMinSimilarity {
		return false
	}
	if len(candidates) > 1 && best.Similarity-candidates[1].Similarity < claudePromptCorrelationMinScoreGap {
		return false
	}
	return true
}
