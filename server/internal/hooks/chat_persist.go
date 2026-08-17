package hooks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	chatv1 "github.com/speakeasy-api/gram/infra/gen/gram/chat/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// ChatPersister writes a hook-captured transcript row. It is the consumer end
// of the gram.chat.v1.HookMessage topic: everything the hook request path used
// to do against Postgres between resolving the row and having it committed
// lives here, and runs in the streams process instead.
//
// It deliberately holds the narrow set of dependencies that persistence needs
// rather than the whole hooks Service, so the streams process can construct one
// without standing up the API surface: the service has a few dozen
// collaborators and none of the rest of them are reachable from this path.
//
// Every insert it performs is keyed on the producer-minted message id, so a
// redelivery is a no-op rather than a duplicate transcript row. That is the
// contract the topic depends on; see the note on chatv1.HookMessage.
type ChatPersister struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	cache           cache.Cache
	repo            *repo.Queries
	productFeatures ProductFeaturesClient
	notifier        StoredNotifier
}

// StoredNotifier is told that transcript rows landed for a project.
//
// This is not an optional nicety. The risk analysis coordinator is a workflow
// that sleeps until signalled and COMPLETES when no signal is pending — there
// is no periodic sweep behind it. On the synchronous path the chat message
// writer's observers are what wake it, and a row written without that wake is
// a row nothing ever analyses. Skill efficacy and chat analysis have the same
// shape.
//
// *chat.ChatMessageWriter satisfies this. The persister takes the interface
// rather than the writer because the only thing it needs from it is the wake,
// and because the wake's implementation reaches Temporal, which is a heavier
// dependency than the rest of this type wants.
type StoredNotifier interface {
	NotifyStored(ctx context.Context, projectID uuid.UUID)
}

// NewChatPersister builds the consumer-side writer.
//
// notifier may be nil, and nil means the coordinators never wake for rows this
// persister writes. That is only appropriate in a test that asserts on the row
// rather than on what the row triggers; in the streams process it must be set.
func NewChatPersister(
	logger *slog.Logger,
	db *pgxpool.Pool,
	cacheImpl cache.Cache,
	productFeatures ProductFeaturesClient,
	notifier StoredNotifier,
) *ChatPersister {
	return &ChatPersister{
		logger:          logger.With(attr.SlogComponent("chat-persister")),
		db:              db,
		cache:           cacheImpl,
		repo:            repo.New(db),
		productFeatures: productFeatures,
		notifier:        notifier,
	}
}

// notifyStored wakes the coordinators that consume transcript rows. Called only
// after a write that durably stored a row — an early wake is worse than none,
// because the coordinator polls, finds nothing, completes, and the signal that
// would have caught the row has already been spent.
func (p *ChatPersister) notifyStored(ctx context.Context, projectID uuid.UUID) {
	if p.notifier == nil {
		return
	}
	p.notifier.NotifyStored(ctx, projectID)
}

// Persist writes the row the message describes. It reports whether a user
// prompt was newly stored, which is the same signal the inline path returns —
// the caller of the inline path used it to mark the session as natively
// captured, and the handler does the same.
//
// A redelivery returns false with a nil error: nothing was stored because the
// row is already there, which is success, not failure.
func (p *ChatPersister) Persist(ctx context.Context, msg *chatv1.HookMessage) (bool, error) {
	if msg == nil {
		return false, nil
	}
	session := msg.GetSession()
	if session == nil {
		return false, fmt.Errorf("hook message carries no session")
	}

	id, err := uuid.Parse(msg.GetId())
	if err != nil {
		return false, fmt.Errorf("parse hook message id: %w", err)
	}
	chatID, err := uuid.Parse(msg.GetChatId())
	if err != nil {
		return false, fmt.Errorf("parse hook message chat id: %w", err)
	}
	projectID, err := uuid.Parse(msg.GetProjectId())
	if err != nil {
		return false, fmt.Errorf("parse hook message project id: %w", err)
	}

	// The entitlement is checked here rather than at publish time on purpose: an
	// org that turns session capture on should start seeing rows without a
	// redeploy, and the check is a cached lookup either way.
	enabled, err := p.sessionCaptureEnabled(ctx, session, projectID)
	if err != nil || !enabled {
		return false, err
	}

	params, err := hookMessageInsertParams(msg, id, chatID, projectID)
	if err != nil {
		return false, err
	}

	// Proxied rows flag the chat whether or not their own row survives: a
	// natively captured session suppresses them as duplicates, so the marker is
	// the only durable trace that the session was routed through LiteLLM.
	// Deferred so it lands after whichever path created the chat row.
	if proxiedTranscriptSource(msg.GetHookSource()) {
		defer p.markChatLiteLLMProxied(ctx, chatID, projectID)

		// The proxy observes the same completion the session's own hook stream
		// reports as its assistant turn. Prompts carry a turn identity that
		// collapses the two into one row; assistant turns carry none, so a
		// proxied assistant row for a natively captured session is dropped
		// rather than persisted alongside the native one.
		if params.Role == "assistant" {
			duplicate, dupErr := p.proxiedTurnDuplicatesNativeStream(ctx, session.GetSessionId(), chatID, projectID)
			if dupErr != nil {
				return false, dupErr
			}
			if duplicate {
				return false, nil
			}
		}
	}

	if msg.GetUncorrelatedPrompt() {
		stored, err := p.insertUncorrelatedPrompt(ctx, msg, session, params, chatID, projectID)
		if err != nil {
			return false, err
		}
		if stored {
			p.notifyStored(ctx, projectID)
		}
		return stored, nil
	}

	stored, err := p.insertWithChatFallback(ctx, session, params, chatID, projectID, msg.GetChatTitle())
	if err != nil {
		return false, err
	}
	if stored {
		p.notifyStored(ctx, projectID)
	}
	return stored && params.Role == "user", nil
}

// hookMessageInsertParams maps the transported row onto insert parameters. The
// columns the ingest path never varies are set here rather than carried on the
// message; see the note on chatv1.HookMessage for why they are absent from it.
func hookMessageInsertParams(msg *chatv1.HookMessage, id, chatID, projectID uuid.UUID) (chatRepo.CreateChatMessageIdempotentParams, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, msg.GetCreatedAt())
	if err != nil {
		return chatRepo.CreateChatMessageIdempotentParams{}, fmt.Errorf("parse hook message created_at: %w", err)
	}

	return chatRepo.CreateChatMessageIdempotentParams{
		ID:             id,
		ChatID:         chatID,
		ProjectID:      projectID,
		Role:           msg.GetRole(),
		Content:        msg.GetContent(),
		Model:          conv.ToPGTextEmpty(msg.GetModel()),
		MessageID:      conv.ToPGTextEmpty(msg.GetMessageId()),
		ToolCallID:     conv.ToPGTextEmpty(msg.GetToolCallId()),
		UserID:         conv.ToPGTextEmpty(msg.GetUserId()),
		ExternalUserID: conv.ToPGTextEmpty(msg.GetExternalUserId()),
		FinishReason:   conv.ToPGTextEmpty(msg.GetFinishReason()),
		ToolCalls:      msg.GetToolCalls(),
		UserAgent:      conv.ToPGTextEmpty(msg.GetUserAgent()),
		Source:         conv.ToPGTextEmpty(msg.GetSource()),
		Replayed:       msg.GetReplayed(),
		CreatedAt:      conv.ToPGTimestamptz(createdAt),
	}, nil
}

// insertWithChatFallback inserts the row, creating the chat and retrying once
// if the row lost a race with (or arrived before) the chat's own creation.
func (p *ChatPersister) insertWithChatFallback(
	ctx context.Context,
	session *chatv1.HookMessage_SessionRef,
	params chatRepo.CreateChatMessageIdempotentParams,
	chatID, projectID uuid.UUID,
	title string,
) (bool, error) {
	n, err := p.insertRow(ctx, params)
	if err == nil {
		return n > 0, nil
	}
	if !isForeignKeyViolation(err) {
		return false, fmt.Errorf("insert hook chat message: %w", err)
	}

	if _, err := p.upsertSession(ctx, p.db, session, chatID, projectID, title); err != nil {
		return false, err
	}

	n, err = p.insertRow(ctx, params)
	if err != nil {
		return false, fmt.Errorf("insert hook chat message after creating chat: %w", err)
	}
	return n > 0, nil
}

// insertRow performs the insert appropriate to the row, mirroring the branch
// the synchronous writer takes.
//
// A message carrying an agent-prompt correlation id is not a plain insert: the
// LiteLLM proxy and the agent's own hook stream both observe that turn, and the
// correlation id is what collapses the two observations into one row by
// promoting an earlier proxied row to the authoritative native source. Insert it
// plainly and the transcript gets both copies.
//
// The two inserts are idempotent by different keys, and both are correct.
// CreateChatMessageIdempotent dedupes on the producer-minted id; the correlated
// upsert dedupes on (chat_id, external_message_id), and its DO UPDATE only fires
// when a proxied row is being promoted, so a redelivery of a native row matches
// nothing and affects no rows.
func (p *ChatPersister) insertRow(ctx context.Context, params chatRepo.CreateChatMessageIdempotentParams) (int64, error) {
	q := chatRepo.New(p.db)

	if strings.HasPrefix(params.MessageID.String, agentPromptCorrelationPrefix) {
		n, err := q.UpsertCorrelatedChatMessage(ctx, correlatedUpsertParams(params))
		if err != nil {
			return 0, fmt.Errorf("upsert correlated hook chat message: %w", err)
		}
		return n, nil
	}

	n, err := q.CreateChatMessageIdempotent(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("insert hook chat message: %w", err)
	}
	return n, nil
}

// correlatedUpsertParams widens the insert parameters to the correlated
// upsert's. The columns it adds are the ones the ingest path always leaves
// empty, so they are zero here for the same reason they are absent from
// chatv1.HookMessage.
//
// Note the id is dropped: the correlated upsert's whole purpose is to land on
// an existing row when one is there, so it cannot also assert a new row's
// identity.
func correlatedUpsertParams(params chatRepo.CreateChatMessageIdempotentParams) chatRepo.UpsertCorrelatedChatMessageParams {
	return chatRepo.UpsertCorrelatedChatMessageParams{
		ChatID:            params.ChatID,
		Role:              params.Role,
		ProjectID:         params.ProjectID,
		Content:           params.Content,
		ContentRaw:        nil,
		ContentAssetUrl:   conv.ToPGTextEmpty(""),
		StorageError:      conv.ToPGTextEmpty(""),
		Model:             params.Model,
		MessageID:         params.MessageID,
		ToolCallID:        params.ToolCallID,
		UserID:            params.UserID,
		ExternalUserID:    params.ExternalUserID,
		ExternalMessageID: conv.ToPGText(params.MessageID.String),
		FinishReason:      params.FinishReason,
		ToolCalls:         params.ToolCalls,
		PromptTokens:      0,
		CompletionTokens:  0,
		TotalTokens:       0,
		Origin:            conv.ToPGTextEmpty(""),
		UserAgent:         params.UserAgent,
		IpAddress:         conv.ToPGTextEmpty(""),
		Source:            params.Source,
		ContentHash:       nil,
		Generation:        0,
		Replayed:          params.Replayed,
		CreatedAt:         params.CreatedAt,
	}
}

// insertUncorrelatedPrompt writes a prompt that carries no turn identity to
// correlate on. Two sources can observe the same prompt — the agent's own hook
// stream and the LiteLLM proxy — and with no shared id the only way to keep
// them from both landing is to serialise the decision, so the insert happens
// under the chat's prompt-correlation lock.
func (p *ChatPersister) insertUncorrelatedPrompt(
	ctx context.Context,
	msg *chatv1.HookMessage,
	session *chatv1.HookMessage_SessionRef,
	params chatRepo.CreateChatMessageIdempotentParams,
	chatID, projectID uuid.UUID,
) (bool, error) {
	native := msg.GetNativePrompt()
	sessionID := session.GetSessionId()

	if !native {
		var nativeSource string
		if cacheErr := p.cache.Get(ctx, sessionNativeHooksCacheKey(projectID.String(), sessionID), &nativeSource); cacheErr == nil && strings.TrimSpace(nativeSource) != "" {
			return false, nil
		}
	}

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin prompt correlation transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := chatRepo.New(tx)
	if err := queries.AcquireChatPromptCorrelationLock(ctx, chatRepo.AcquireChatPromptCorrelationLockParams{
		ProjectID: projectID,
		ChatID:    chatID,
	}); err != nil {
		return false, fmt.Errorf("lock prompt correlation: %w", err)
	}

	if !native {
		latestSource, latestErr := queries.GetLatestChatUserPromptSource(ctx, chatRepo.GetLatestChatUserPromptSourceParams{
			ChatID:    chatID,
			ProjectID: projectID,
		})
		if latestErr != nil && !errors.Is(latestErr, pgx.ErrNoRows) {
			return false, fmt.Errorf("get latest chat user prompt source: %w", latestErr)
		}
		if latestErr == nil && latestSource.Valid && usesNativeTranscriptFallback(latestSource.String) {
			p.markNativePromptSession(ctx, projectID.String(), sessionID, latestSource.String)
			return false, nil
		}
	}

	// The chat is upserted inside the transaction rather than relying on the
	// FK-violation retry the plain path uses: the lock is taken on the chat, so
	// by this point it either exists or this transaction has to create it.
	if _, err := p.upsertSession(ctx, tx, session, chatID, projectID, msg.GetChatTitle()); err != nil {
		return false, err
	}

	// Inside the lock the plain insert is always right: an uncorrelated prompt
	// is by definition one with no correlation id to upsert on.
	n, err := queries.CreateChatMessageIdempotent(ctx, params)
	if err != nil {
		return false, fmt.Errorf("insert uncorrelated hook prompt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit uncorrelated hook prompt: %w", err)
	}
	return n > 0, nil
}

func (p *ChatPersister) upsertSession(
	ctx context.Context,
	db repo.DBTX,
	session *chatv1.HookMessage_SessionRef,
	chatID, projectID uuid.UUID,
	title string,
) (uuid.UUID, error) {
	row, err := repo.New(db).UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: session.GetOrganizationId(),
		UserID:         conv.ToPGTextEmpty(session.GetUserId()),
		ExternalUserID: conv.ToPGTextEmpty(session.GetUserEmail()),
		UserAccountID:  conv.StringToNullUUID(session.GetUserAccountId()),
		Title:          conv.ToPGText(title),
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert chat session: %w", err)
	}
	return row, nil
}

func (p *ChatPersister) sessionCaptureEnabled(ctx context.Context, session *chatv1.HookMessage_SessionRef, projectID uuid.UUID) (bool, error) {
	if p.productFeatures == nil {
		return false, nil
	}
	enabled, err := p.productFeatures.IsFeatureEnabled(ctx, session.GetOrganizationId(), productfeatures.FeatureSessionCapture)
	if err != nil {
		return false, fmt.Errorf("check session_capture feature flag: %w", err)
	}
	if !enabled {
		p.logger.DebugContext(ctx, "session capture disabled; skipping hook chat persistence",
			attr.SlogEvent("hook_chat_persist_session_capture_disabled"),
			attr.SlogOrganizationID(session.GetOrganizationId()),
			attr.SlogProjectID(projectID.String()),
			attr.SlogGenAIConversationID(session.GetSessionId()),
		)
	}
	return enabled, nil
}

// proxiedTurnDuplicatesNativeStream reports whether the session's transcript is
// already owned by a hook stream that reports its own assistant turns. The
// marker written when a native prompt lands answers this without a query; the
// latest user prompt's source is the durable fallback for sessions whose marker
// expired. Unlike the prompt path this does not repair the marker: the marker
// also gates prompt suppression, and only the prompt path's own writes decide
// what belongs there.
func (p *ChatPersister) proxiedTurnDuplicatesNativeStream(ctx context.Context, sessionID string, chatID, projectID uuid.UUID) (bool, error) {
	if sessionID == "" {
		return false, nil
	}
	var nativeSource string
	if err := p.cache.Get(ctx, sessionNativeHooksCacheKey(projectID.String(), sessionID), &nativeSource); err == nil && nativeAssistantTurnSource(nativeSource) {
		return true, nil
	}
	latestSource, err := chatRepo.New(p.db).GetLatestChatUserPromptSource(ctx, chatRepo.GetLatestChatUserPromptSourceParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("get latest chat user prompt source: %w", err)
	}
	return latestSource.Valid && nativeAssistantTurnSource(latestSource.String), nil
}

func (p *ChatPersister) markNativePromptSession(ctx context.Context, projectID, sessionID, source string) {
	if sessionID == "" {
		return
	}
	cacheCtx, cancel := context.WithTimeout(ctx, canonicalSessionCacheWriteTimeout)
	defer cancel()
	if err := p.cache.Set(cacheCtx, sessionNativeHooksCacheKey(projectID, sessionID), source, 24*time.Hour); err != nil {
		p.logger.WarnContext(ctx, "failed to mark native prompt session",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
	}
}

func (p *ChatPersister) markChatLiteLLMProxied(ctx context.Context, chatID, projectID uuid.UUID) {
	err := chatRepo.New(p.db).MarkChatLiteLLMProxied(ctx, chatRepo.MarkChatLiteLLMProxiedParams{
		ID:        chatID,
		ProjectID: projectID,
	})
	if err != nil {
		p.logger.WarnContext(ctx, "failed to mark chat as LiteLLM proxied",
			attr.SlogError(err),
			attr.SlogProjectID(projectID.String()),
			attr.SlogChatID(chatID.String()),
		)
	}
}
