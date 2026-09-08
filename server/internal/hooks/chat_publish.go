package hooks

import (
	"context"
	"time"

	"github.com/google/uuid"

	chatv1 "github.com/speakeasy-api/gram/infra/gen/gram/chat/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// asyncChatPersist reports whether this project's transcript rows go onto the
// topic instead of being written on the request path.
//
// Evaluated locally: this runs on every captured hook event, and a decide call
// per event would put a round trip on the path this change exists to speed up.
// The project ID is the distinct ID so a percentage rollout keeps a project
// wholly on one path rather than splitting a session's rows across both.
//
// Any failure answers false: being wrong costs latency, not a lost row.
func (s *Service) asyncChatPersist(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	// Only the request-scoped values are checked. s.flags and s.chatMessages are
	// constructor arguments and always set — a Service missing them is a wiring
	// bug, and answering false here would hide it as a silent fallback to the
	// synchronous path for every event.
	if authCtx == nil || authCtx.ProjectID == nil {
		return false
	}

	personProperties := map[string]string{
		"organization_slug": authCtx.OrganizationSlug,
	}
	if authCtx.ProjectSlug != nil {
		personProperties["project_slug"] = *authCtx.ProjectSlug
	}

	enabled, err := s.flags.IsFlagEnabledLocal(
		ctx,
		feature.FlagChatMessageAsyncPersist,
		authCtx.ProjectID.String(),
		nil,
		personProperties,
	)
	if err != nil {
		s.logger.WarnContext(ctx, "async chat persist flag lookup failed; writing synchronously",
			attr.SlogEvent("chat_message_async_persist_flag_failed"),
			attr.SlogError(err),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
		return false
	}
	return enabled
}

// publishChatMessage sends a resolved transcript row to the streams process.
//
// The row's id is minted here rather than by the database: that is what makes
// the consumer's insert idempotent under at-least-once delivery. See
// chatv1.HookMessage.
//
// The publish result is not awaited — that would put a broker round trip back
// on the request path — so the ack is drained on a detached goroutine where a
// failure is still logged and counted.
func (s *Service) publishChatMessage(
	ctx context.Context,
	metadata *SessionMetadata,
	authCtx *contextvalues.AuthContext,
	msg chatRepo.CreateChatMessageParams,
	title string,
	hookSource string,
	adapter string,
	uncorrelatedPrompt bool,
	nativePrompt bool,
) error {
	createdAt := msg.CreatedAt.Time
	if !msg.CreatedAt.Valid {
		createdAt = s.now()
	}

	// Nano, not second, precision: readers order on
	// (chat_id, generation, created_at, seq), so truncating ties rows within a
	// second and drops them onto seq — here Pub/Sub order, not event order.
	createdAtRFC3339 := createdAt.UTC().Format(time.RFC3339Nano)
	rowID := uuid.Must(uuid.NewV7()).String()
	chatID := msg.ChatID.String()
	projectID := msg.ProjectID.String()

	out := chatv1.HookMessage_builder{
		Id:        &rowID,
		ChatId:    &chatID,
		ProjectId: &projectID,

		Role:           &msg.Role,
		Content:        &msg.Content,
		Model:          &msg.Model.String,
		MessageId:      &msg.MessageID.String,
		ToolCallId:     &msg.ToolCallID.String,
		UserId:         &msg.UserID.String,
		ExternalUserId: &msg.ExternalUserID.String,
		FinishReason:   &msg.FinishReason.String,
		ToolCalls:      msg.ToolCalls,
		UserAgent:      &msg.UserAgent.String,
		Source:         &msg.Source.String,
		Replayed:       &msg.Replayed,
		CreatedAt:      &createdAtRFC3339,

		Session: chatv1.HookMessage_SessionRef_builder{
			SessionId:      &metadata.SessionID,
			OrganizationId: &metadata.GramOrgID,
			UserId:         &metadata.UserID,
			UserEmail:      &metadata.UserEmail,
			UserAccountId:  &metadata.UserAccountID,
			Cwd:            &metadata.Cwd,
			Provider:       &metadata.Provider,
			HookHostname:   &metadata.Hostname,
			AccountType:    &metadata.AccountType,
			BillingMode:    &metadata.BillingMode,
		}.Build(),
		HookSource:         &hookSource,
		Adapter:            &adapter,
		ChatTitle:          &title,
		UncorrelatedPrompt: &uncorrelatedPrompt,
		NativePrompt:       &nativePrompt,
	}.Build()

	// Detach cancellation: the hook response is about to return and some clients
	// close immediately. A publish abandoned mid-flight is a row that never
	// existed, with no error for the caller to retry on.
	publishCtx := context.WithoutCancel(ctx)
	result := s.chatMessages.Publish(publishCtx, out)

	go func() {
		drainCtx, cancel := context.WithTimeout(publishCtx, chatMessagePublishAckTimeout)
		defer cancel()

		if _, err := result.Get(drainCtx); err != nil {
			s.logger.ErrorContext(drainCtx, "failed to publish hook chat message",
				attr.SlogEvent("chat_message_publish_failed"),
				attr.SlogError(err),
				attr.SlogChatID(out.GetChatId()),
				attr.SlogProjectID(out.GetProjectId()),
				attr.SlogGenAIConversationID(metadata.SessionID),
			)
		}
	}()

	s.logger.DebugContext(ctx, "published hook chat message",
		attr.SlogEvent("chat_message_published"),
		attr.SlogChatID(out.GetChatId()),
		attr.SlogProjectID(out.GetProjectId()),
	)
	return nil
}

// chatMessagePublishAckTimeout bounds the detached ack drain. It only has to
// outlast the publisher's own send timeout; past that there is nothing left to
// wait for and the goroutine should go away.
const chatMessagePublishAckTimeout = 30 * time.Second
