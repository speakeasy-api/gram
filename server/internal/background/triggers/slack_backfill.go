package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	// slackBackfillLimit caps the thread messages added to an event. Slack
	// limits conversations.replies to 15 messages per call for apps
	// distributed outside the Marketplace, so a single call reads the same
	// amount for every install. The target reads further with the Slack
	// platform tools.
	slackBackfillLimit = 15

	// slackThreadContextKey is the event JSON key that carries a
	// slackThreadContext.
	slackThreadContextKey = "thread_context"
)

// backfillSlackThread adds the thread messages the target has not seen to a
// Slack task's event. A failure to read the thread marks the context
// unavailable instead of failing the dispatch.
func (a *App) backfillSlackThread(ctx context.Context, task Task) (Task, error) {
	if task.ThreadBackfill == nil || task.DefinitionSlug != DefinitionSlugSlack {
		return task, nil
	}

	var event slackTriggerEvent
	if err := json.Unmarshal(task.EventJSON, &event); err != nil {
		return task, fmt.Errorf("decode slack event: %w", err)
	}
	if event.ChannelID == "" || event.ThreadID == "" {
		return task, nil
	}

	logger := a.logger.With(attr.SlogTriggerInstanceID(task.TriggerInstanceID))
	threadContext := slackThreadContext{Messages: nil, Truncated: false, Unavailable: true}
	token, err := a.slackBotToken(ctx, task.TriggerInstanceID)
	switch {
	case err != nil:
		logger.WarnContext(ctx, "load slack token for thread backfill", attr.SlogError(err))
	case token != "":
		threadContext = a.readSlackThreadContext(ctx, logger, token, task.ThreadBackfill.After, event)
	}

	eventJSON, err := setEventJSONField(task.EventJSON, slackThreadContextKey, threadContext)
	if err != nil {
		return task, err
	}
	task.EventJSON = eventJSON
	return task, nil
}

// readSlackThreadContext reads the messages of the event's thread after the
// after cursor and before the event, leaving out the app's own messages.
func (a *App) readSlackThreadContext(ctx context.Context, logger *slog.Logger, token, after string, event slackTriggerEvent) slackThreadContext {
	replies, err := a.slackClient.ThreadReplies(ctx, token, slackclient.SlackThreadRepliesInput{
		ChannelID: event.ChannelID,
		ThreadTS:  event.ThreadID,
		Oldest:    after,
		Latest:    event.Timestamp,
		Limit:     slackBackfillLimit,
	})
	if err != nil {
		logger.WarnContext(ctx, "read slack thread for backfill", attr.SlogError(err))
		return slackThreadContext{Messages: nil, Truncated: false, Unavailable: true}
	}

	messages := make([]slackThreadContextMessage, 0, len(replies.Messages))
	for _, message := range replies.Messages {
		// Slack returns the thread's parent even when it falls before
		// oldest, so bounds are enforced here too.
		outside := (after != "" && message.Ts <= after) || message.Ts >= event.Timestamp
		if outside || message.Hidden || slackSelfAuthored(message.User, message.BotID, event) {
			continue
		}
		messages = append(messages, slackThreadContextMessage{
			Ts:     message.Ts,
			UserID: message.User,
			BotID:  message.BotID,
			Text:   message.Text,
		})
	}
	return slackThreadContext{Messages: messages, Truncated: replies.HasMore, Unavailable: false}
}

func (a *App) slackBotToken(ctx context.Context, triggerInstanceID string) (string, error) {
	instanceID, err := uuid.Parse(triggerInstanceID)
	if err != nil {
		return "", fmt.Errorf("parse trigger instance id: %w", err)
	}
	instance, err := a.repo.GetTriggerInstanceByIDPublic(ctx, instanceID)
	if err != nil {
		return "", fmt.Errorf("get trigger instance: %w", err)
	}
	if !instance.EnvironmentID.Valid {
		return "", nil
	}
	envMap, err := a.loadEnvironmentMap(ctx, instance.ProjectID, instance.EnvironmentID.UUID)
	if err != nil {
		return "", err
	}
	return toolconfig.CIEnvFrom(envMap).Get("SLACK_BOT_TOKEN"), nil
}
