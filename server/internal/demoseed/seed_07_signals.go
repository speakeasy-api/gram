package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

const (
	// signalMessagesPerDay gives long-range charts multiple observations per day.
	signalMessagesPerDay = 12
	// signalRecentDays supplies dense intraday examples during the last week.
	signalRecentDays = 7
	// signalRecentMessages places a message every ten minutes across six hours.
	signalRecentMessages = 36
	// signalSessionSize keeps several original utterances in each conversation.
	signalSessionSize = 6
	// signalMessageCount is stable across reseeds and shared by both databases.
	signalMessageCount = sigintReadingDays*signalMessagesPerDay + signalRecentDays*(signalRecentMessages-signalMessagesPerDay)
)

func (s *seeder) signalSource(n int) (chat, message uuid.UUID, at time.Time) {
	day, slot := n/signalRecentMessages, n%signalRecentMessages
	if n >= signalRecentDays*signalRecentMessages {
		older := n - signalRecentDays*signalRecentMessages
		day, slot = signalRecentDays+older/signalMessagesPerDay, older%signalMessagesPerDay
	}
	chat = s.det(fmt.Sprintf("sigint-chat-%d-%d", day, slot/signalSessionSize))
	message = s.det("sigint-message-" + strconv.Itoa(n))
	at = s.now.UTC().AddDate(0, 0, -day).Add(-time.Duration(slot+1) * 10 * time.Minute)
	return chat, message, at
}

func (s *seeder) seedSignalConversations(ctx context.Context) error {
	s.signalSequences = make(map[int]int64, signalMessageCount)
	ids := make([]uuid.UUID, 0, signalMessageCount)
	// Insert oldest first so the generated sequence agrees with transcript order.
	for n := signalMessageCount - 1; n >= 0; n-- {
		chat, message, at := s.signalSource(n)
		owner := demoUsers[(n/signalSessionSize)%len(demoUsers)]
		if n%signalSessionSize == signalSessionSize-1 {
			age := int(s.now.Sub(at) / time.Minute)
			if err := s.q.ActivityInsertChat(ctx, repo.ActivityInsertChatParams{ID: chat, ProjectID: s.projectID(), OrganizationID: s.orgID(), UserID: s.userID(owner.handle), ExternalUserID: s.email(owner.handle), Title: "Signal exploration example", SessionSurface: activityText("claude-code"), AgentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, CreatedAge: activityMinutes(age), UpdatedAge: activityMinutes(age - (signalSessionSize-1)*10)}); err != nil {
				return fmt.Errorf("seed signal conversation: %w", err)
			}
		}
		texts := [...]string{"Review the release plan and identify the remaining risks.", "The deployment needs a rollback check and a permissions review.", "Could we simplify the implementation before shipping?", "We can remove the duplicate check and keep the test focused.", "Check the validation results and explain the tradeoffs.", "The tests passed. The remaining uncertainty is the rollout window."}
		position := signalSessionSize - 1 - n%signalSessionSize
		roles := [...]string{"user", "assistant"}
		seq, err := s.q.ActivityInsertSignalMessage(ctx, repo.ActivityInsertSignalMessageParams{ID: message, ChatID: chat, ProjectID: s.projectID(), Role: roles[position%2], Content: texts[position%len(texts)], CreatedAt: conv.ToPGTimestamptz(at)})
		if err != nil {
			return fmt.Errorf("seed signal message: %w", err)
		}
		s.signalSequences[n] = seq
		ids = append(ids, message)
	}
	count, err := s.q.ActivityCountSignalMessages(ctx, repo.ActivityCountSignalMessagesParams{ProjectID: uuid.NullUUID{UUID: s.projectID(), Valid: true}, MessageIds: ids})
	if err != nil {
		return fmt.Errorf("count signal messages: %w", err)
	}
	if count != signalMessageCount {
		return fmt.Errorf("signal message postflight: expected %d, found %d", signalMessageCount, count)
	}
	return nil
}
