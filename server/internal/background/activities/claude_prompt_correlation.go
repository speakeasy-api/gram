package activities

import (
	"context"
	"errors"
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

	// Fetch one extra row to detect whether the workflow should immediately run
	// another bounded drain pass.
	claudePromptCorrelationMessageBatchSize = 25

	// claudePromptCorrelationMatchTimeout bounds the ClickHouse candidate query
	// for a single message so one slow query cannot consume the whole activity
	// budget.
	claudePromptCorrelationMatchTimeout = 5 * time.Second
)

var errClaudePromptCorrelationMatchTimeout = errors.New("claude prompt correlation match timed out")

type claudePromptCorrelationStore interface {
	ListUnlinkedClaudeUserMessagesForCorrelation(context.Context, activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams) ([]activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationRow, error)
	BackfillClaudeUserMessagePromptID(context.Context, activitiesrepo.BackfillClaudeUserMessagePromptIDParams) error
}

type claudePromptCorrelationTelemetry interface {
	ListClaudeUserPromptCandidatesForCorrelation(context.Context, telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams) ([]telemetryrepo.ClaudeUserPromptCandidate, error)
}

type CorrelateClaudePrompts struct {
	logger       *slog.Logger
	store        claudePromptCorrelationStore
	telemetry    claudePromptCorrelationTelemetry
	matchTimeout time.Duration
}

func NewCorrelateClaudePrompts(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn) *CorrelateClaudePrompts {
	return &CorrelateClaudePrompts{
		logger:       logger.With(attr.SlogComponent("correlate_claude_prompts")),
		store:        activitiesrepo.New(db),
		telemetry:    telemetryrepo.New(chConn),
		matchTimeout: claudePromptCorrelationMatchTimeout,
	}
}

type CorrelateClaudePromptsArgs struct {
	ProjectID              uuid.UUID
	ChatID                 uuid.UUID
	SessionID              string
	AfterMessageSeq        int64
	AfterEventSequence     int64
	AfterEventTimeUnixNano int64
}

type CorrelateClaudePromptsResult struct {
	HasMore                bool
	AfterMessageSeq        int64
	AfterEventSequence     int64
	AfterEventTimeUnixNano int64
}

type claudeUnlinkedUserMessage struct {
	id        uuid.UUID
	seq       int64
	content   string
	createdAt time.Time
}

func (c *CorrelateClaudePrompts) Do(ctx context.Context, args CorrelateClaudePromptsArgs) (*CorrelateClaudePromptsResult, error) {
	messages, hasMore, err := c.listUnlinkedUserMessages(ctx, args.ProjectID, args.ChatID, args.AfterMessageSeq)
	if err != nil {
		return nil, fmt.Errorf("list unlinked Claude user messages: %w", err)
	}
	result := &CorrelateClaudePromptsResult{
		HasMore:                hasMore,
		AfterMessageSeq:        args.AfterMessageSeq,
		AfterEventSequence:     args.AfterEventSequence,
		AfterEventTimeUnixNano: args.AfterEventTimeUnixNano,
	}
	if len(messages) == 0 {
		return result, nil
	}

	deadline, hasDeadline := ctx.Deadline()

	cursor := claudePromptCorrelationCursor{
		eventSequence: args.AfterEventSequence,
		timeUnixNano:  args.AfterEventTimeUnixNano,
	}
	for _, message := range messages {
		// Stop before the activity deadline so a partially drained backlog
		// completes successfully and the remainder is picked up by a later run,
		// rather than the whole activity failing and re-running from scratch.
		if hasDeadline && time.Until(deadline) <= c.matchTimeout {
			result.HasMore = true
			break
		}

		match, ok, err := c.findClaudeUserPromptMatch(ctx, args.ProjectID, args.ChatID, args.SessionID, message, cursor)
		if err != nil {
			return nil, fmt.Errorf("find Claude user prompt match: %w", err)
		}
		result.AfterMessageSeq = message.seq
		if !ok {
			continue
		}

		if err := c.backfillMessagePromptID(ctx, args.ProjectID, args.ChatID, message.id, match.PromptID); err != nil {
			return nil, fmt.Errorf("backfill Claude prompt ID: %w", err)
		}
		cursor.eventSequence = match.EventSequence
		cursor.timeUnixNano = match.TimeUnixNano
		result.AfterEventSequence = match.EventSequence
		result.AfterEventTimeUnixNano = match.TimeUnixNano
	}

	return result, nil
}

func (c *CorrelateClaudePrompts) listUnlinkedUserMessages(ctx context.Context, projectID uuid.UUID, chatID uuid.UUID, afterMessageSeq int64) ([]claudeUnlinkedUserMessage, bool, error) {
	rows, err := c.store.ListUnlinkedClaudeUserMessagesForCorrelation(ctx, activitiesrepo.ListUnlinkedClaudeUserMessagesForCorrelationParams{
		ChatID:          chatID,
		ProjectID:       conv.ToNullUUID(projectID),
		AfterMessageSeq: afterMessageSeq,
		LimitCount:      claudePromptCorrelationMessageBatchSize + 1,
	})
	if err != nil {
		return nil, false, fmt.Errorf("query unlinked user messages: %w", err)
	}

	hasMore := len(rows) > claudePromptCorrelationMessageBatchSize
	if hasMore {
		rows = rows[:claudePromptCorrelationMessageBatchSize]
	}

	messages := make([]claudeUnlinkedUserMessage, 0, len(rows))
	for _, row := range rows {
		messages = append(messages, claudeUnlinkedUserMessage{
			id:        row.ID,
			seq:       row.Seq,
			content:   row.Content,
			createdAt: row.CreatedAt.Time,
		})
	}
	return messages, hasMore, nil
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

	queryCtx, cancel := context.WithTimeout(ctx, c.matchTimeout)
	defer cancel()

	candidates, err := c.telemetry.ListClaudeUserPromptCandidatesForCorrelation(queryCtx, telemetryrepo.ListClaudeUserPromptCandidatesForCorrelationParams{
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
		if errors.Is(queryCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return noMatch, false, errClaudePromptCorrelationMatchTimeout
		}
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
	err := c.store.BackfillClaudeUserMessagePromptID(ctx, activitiesrepo.BackfillClaudeUserMessagePromptIDParams{
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
