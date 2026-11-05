package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// seedAgentOwnerSessions: the agent owner persona's sessions. The agent's own
// sessions name it as actor and no user, so the persona reaches them only by
// owning the agent; the persona's own sessions are theirs outright. Both sets
// cascade with the project delete.
func (s *seeder) seedAgentOwnerSessions(ctx context.Context) error {
	for _, sess := range ownerSessions {
		chatID := s.ownerSessionChatID(sess.key)
		if err := s.insertOwnerChat(ctx, chatID, sess); err != nil {
			return err
		}
		if err := s.activityInsertMessages(ctx, chatID, s.ownerTranscript(sess)); err != nil {
			return err
		}
	}
	return nil
}

func (s *seeder) insertOwnerChat(ctx context.Context, chatID uuid.UUID, sess ownerSession) error {
	created := activityMinutes(sess.ageMin)
	updated := activityMinutes(sess.ageMin - len(sess.tools) - 2)
	if sess.agent {
		if err := s.q.ActivityInsertAgentChat(ctx, repo.ActivityInsertAgentChatParams{
			ID:             chatID,
			ProjectID:      s.projectID(),
			OrganizationID: s.orgID(),
			Title:          sess.title,
			SessionSurface: activityNull,
			AgentID:        s.ownerAgentID(),
			CreatedAge:     created,
			UpdatedAge:     updated,
		}); err != nil {
			return fmt.Errorf("insert demo agent owner chat %q: %w", sess.key, err)
		}
		return nil
	}
	if err := s.q.ActivityInsertChat(ctx, repo.ActivityInsertChatParams{
		ID:             chatID,
		ProjectID:      s.projectID(),
		OrganizationID: s.orgID(),
		UserID:         s.userID(demoAgentOwner.handle),
		ExternalUserID: s.email(demoAgentOwner.handle),
		Title:          sess.title,
		SessionSurface: activityText("claude-code"),
		AgentID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CreatedAge:     created,
		UpdatedAge:     updated,
	}); err != nil {
		return fmt.Errorf("insert demo agent owner chat %q: %w", sess.key, err)
	}
	return nil
}

// ownerTranscript is the session's messages: the prompt, one assistant turn
// calling every tool, a tool result per call carrying the same payload the
// telemetry row records, and the answer.
func (s *seeder) ownerTranscript(sess ownerSession) []activityMessage {
	model := activityText(activityModel)
	src := activityText("claude-code")
	if sess.agent {
		src = activityNull
	}
	key := "owner-session-" + sess.key
	msgs := []activityMessage{
		{key + "-prompt", "user", sess.prompt, activityTextRaw(sess.prompt), nil, src, model, activityMinutes(sess.ageMin), activityNull},
	}
	calls := make([]string, 0, len(sess.tools))
	for k, tool := range sess.tools {
		calls = append(calls, `{"id":"`+ownerToolCallID(sess.key, k+1)+`","type":"function","function":{"name":"`+tool+`","arguments":"`+s.ownerToolArgs(sess, k+1)+`"}}`)
	}
	msgs = append(msgs, activityMessage{key + "-calls", "assistant", "", nil, []byte("[" + strings.Join(calls, ",") + "]"), src, model, activityMinutes(sess.ageMin - 1), activityNull})
	for k := range sess.tools {
		// Telemetry payloads are escaped for embedding inside the attributes
		// JSON document; the transcript stores the JSON itself.
		result := strings.ReplaceAll(s.ownerToolResult(sess, k+1), `\"`, `"`)
		msgs = append(msgs, activityMessage{key + "-result-" + strconv.Itoa(k+1), "tool", result, nil, nil, src, model,
			activityMinutes(sess.ageMin - 2 - k), activityText(ownerToolCallID(sess.key, k+1))})
	}
	return append(msgs, activityMessage{key + "-answer", "assistant", sess.answer, activityTextRaw(sess.answer), nil, src, model,
		activityMinutes(sess.ageMin - len(sess.tools) - 2), activityNull})
}
