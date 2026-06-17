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

	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	claudePromptCorrelationMinFuzzyLength = 40
	claudePromptCorrelationMinSimilarity  = 0.95
	claudePromptCorrelationMinScoreGap    = 0.02
	claudePromptCorrelationMaxTimeDelta   = 10 * time.Minute
)

type CorrelateClaudePrompts struct {
	repo   *activitiesrepo.Queries
	chRepo *telemetryrepo.Queries
}

func NewCorrelateClaudePrompts(_ *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn) *CorrelateClaudePrompts {
	var repo *activitiesrepo.Queries
	if db != nil {
		repo = activitiesrepo.New(db)
	}

	var chRepo *telemetryrepo.Queries
	if chConn != nil {
		chRepo = telemetryrepo.New(chConn)
	}

	return &CorrelateClaudePrompts{
		repo:   repo,
		chRepo: chRepo,
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
	if args.SessionID == "" || args.ProjectID == uuid.Nil || args.ChatID == uuid.Nil {
		return nil
	}
	if c.repo == nil || c.chRepo == nil {
		return nil
	}

	messages, err := c.listUnlinkedUserMessages(ctx, args.ProjectID, args.ChatID)
	if err != nil {
		return fmt.Errorf("list unlinked Claude user messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	var cursor claudePromptCorrelationCursor
	for _, message := range messages {
		match, ok, err := c.findClaudeUserPromptMatch(ctx, args.ProjectID, args.ChatID, args.SessionID, message, cursor)
		if err != nil {
			return fmt.Errorf("find Claude user prompt match: %w", err)
		}
		if !ok {
			continue
		}

		if err := c.backfillMessagePromptID(ctx, args.ProjectID, args.ChatID, message.id, match.PromptID); err != nil {
			return fmt.Errorf("backfill Claude prompt ID: %w", err)
		}
		cursor.eventSequence = match.EventSequence
		cursor.timeUnixNano = match.TimeUnixNano
	}

	return nil
}

func (c *CorrelateClaudePrompts) listUnlinkedUserMessages(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID) ([]claudeUnlinkedUserMessage, error) {
	rows, err := c.repo.ListUnlinkedClaudeUserMessagesForCorrelation(ctx, activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams{
		ChatID:    chatID,
		ProjectID: conv.ToNullUUID(projectID),
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

	candidates, err := c.chRepo.ListClaudeUserPromptCandidatesForCorrelation(ctx, telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams{
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
	err := c.repo.BackfillClaudeUserMessagePromptID(ctx, activitiesrepo.BackfillClaudeUserMessagePromptIDParams{
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
