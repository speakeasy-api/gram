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
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// ChatPersister writes a hook-captured transcript row in the streams process:
// the consumer end of the gram.chat.v1.HookMessage topic, holding what
// persistence needs rather than the whole hooks Service.
//
// Every insert is keyed on the producer-minted message id, so a redelivery is a
// no-op rather than a duplicate row. See the note on chatv1.HookMessage.
type ChatPersister struct {
	logger          *slog.Logger
	db              *pgxpool.Pool
	cache           cache.Cache
	repo            *repo.Queries
	productFeatures ProductFeaturesClient
	notifier        StoredNotifier
	meter           StorageMeter
}

// StorageMeter measures a stored transcript row for billing. The reading is
// enqueued in the transaction that inserts the row, so a customer is charged if
// and only if the row is durably stored: metering at publish time would bill
// for rows a failed publish dropped.
//
// Required. There is no unmetered mode — a persister that stored rows without
// billing them is the bug this exists to prevent, so a missing meter fails
// loudly rather than degrading into one.
type StorageMeter interface {
	StorageReading(ctx context.Context, input chat.StorageReadingInput) ([]metering.Reading, error)
}

// StoredNotifier is told that transcript rows landed for a project.
//
// Not optional: the risk analysis coordinator sleeps until signalled and
// completes when no signal is pending, with no sweep behind it, so a row
// written without a wake is one nothing analyses. Skill efficacy and chat
// analysis have the same shape. *chat.ChatMessageWriter satisfies it.
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
	meter StorageMeter,
) *ChatPersister {
	return &ChatPersister{
		logger:          logger.With(attr.SlogComponent("chat-persister")),
		db:              db,
		cache:           cacheImpl,
		repo:            repo.New(db),
		productFeatures: productFeatures,
		notifier:        notifier,
		meter:           meter,
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
	n, err := p.insertRow(ctx, session, params)
	if err == nil {
		return n > 0, nil
	}
	if !isForeignKeyViolation(err) {
		return false, fmt.Errorf("insert hook chat message: %w", err)
	}

	if _, err := p.upsertSession(ctx, p.db, session, chatID, projectID, title); err != nil {
		return false, err
	}

	n, err = p.insertRow(ctx, session, params)
	if err != nil {
		return false, fmt.Errorf("insert hook chat message after creating chat: %w", err)
	}
	return n > 0, nil
}

// insertRow performs the insert appropriate to the row and meters it in the same
// transaction when it is new, mirroring the branch the synchronous writer takes.
//
// A correlation id means the LiteLLM proxy and the agent's hook stream both
// observed the turn; the correlated upsert collapses them into one row by
// promoting the proxied row. Inserting plainly would keep both copies.
//
// The two inserts are idempotent by different keys: the producer-minted id, and
// (chat_id, external_message_id) whose DO UPDATE only fires for a promotion.
func (p *ChatPersister) insertRow(
	ctx context.Context,
	session *chatv1.HookMessage_SessionRef,
	params chatRepo.CreateChatMessageIdempotentParams,
) (int64, error) {
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin hook chat message transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := chatRepo.New(tx)

	var n int64
	reading := storageReadingInput(session, params)
	if strings.HasPrefix(params.MessageID.String, agentPromptCorrelationPrefix) {
		// The upsert returns the persisted row rather than a count, so a
		// redelivery that promotes nothing surfaces as ErrNoRows — the same
		// "affected no rows" the synchronous writer reads it as.
		stored, upsertErr := q.UpsertCorrelatedChatMessage(ctx, correlatedUpsertParams(params))
		if upsertErr != nil {
			if !errors.Is(upsertErr, pgx.ErrNoRows) {
				return 0, fmt.Errorf("upsert correlated hook chat message: %w", upsertErr)
			}
		} else {
			n = 1
			// Bill the row that is actually persisted. A promotion lands on the
			// proxied row and keeps that row's identity and measured content,
			// and the reading id is derived from the identity — metering the
			// incoming hook's id instead would charge the promoted row a second
			// time under a key that cannot converge with its first reading.
			reading.MessageID = stored.ID
			reading.Content = stored.Content
			reading.ToolCalls = stored.ToolCalls
			reading.Model = stored.Model
			reading.MessageUserID = stored.UserID
			reading.MessageExternalUserID = stored.ExternalUserID
			reading.Source = stored.Source
		}
	} else {
		n, err = q.CreateChatMessageIdempotent(ctx, params)
		if err != nil {
			return 0, fmt.Errorf("insert hook chat message: %w", err)
		}
	}

	if n > 0 {
		if err := p.meterStored(ctx, tx, reading); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit hook chat message: %w", err)
	}
	return n, nil
}

// meterStored enqueues the storage reading onto the transaction that inserted
// the row, so charge and row commit together or not at all.
//
// A reading that cannot be generated is logged and skipped rather than failing
// the write, matching the synchronous writer. Aborting would roll back the
// transcript row and nack the message, so a tokenizer failure would cost the
// customer their transcript to protect a billing record.
func (p *ChatPersister) meterStored(ctx context.Context, tx pgx.Tx, input chat.StorageReadingInput) error {
	readings, err := p.meter.StorageReading(ctx, input)
	if err != nil {
		p.logger.ErrorContext(ctx, "generate hook chat message storage reading",
			attr.SlogError(err),
			attr.SlogProjectID(input.ProjectID.String()),
			attr.SlogMessageID(input.MessageID.String()),
		)
		return nil
	}
	if err := metering.Enqueue(ctx, tx, readings); err != nil {
		return fmt.Errorf("enqueue hook chat message reading: %w", err)
	}
	return nil
}

// storageReadingInput describes the row as the producer sent it. The correlated
// upsert overrides the identity and content fields with the row it actually
// persisted.
//
// occurredAt is the event's own timestamp, so a redelivery lands in the billing
// period the message describes rather than the one it was retried in.
func storageReadingInput(
	session *chatv1.HookMessage_SessionRef,
	params chatRepo.CreateChatMessageIdempotentParams,
) chat.StorageReadingInput {
	return chat.StorageReadingInput{
		OrganizationID:        session.GetOrganizationId(),
		ProjectID:             params.ProjectID,
		MessageID:             params.ID,
		ChatID:                params.ChatID,
		Content:               params.Content,
		ToolCalls:             params.ToolCalls,
		Model:                 params.Model,
		Provider:              session.GetProvider(),
		Source:                params.Source,
		HookHostname:          session.GetHookHostname(),
		AccountType:           session.GetAccountType(),
		BillingMode:           session.GetBillingMode(),
		BillingUserID:         session.GetUserId(),
		WorkloadSource:        metering.WorkloadSourceHook,
		MessageUserID:         params.UserID,
		MessageExternalUserID: params.ExternalUserID,
		MessageUserEmail:      session.GetUserEmail(),
		OccurredAt:            params.CreatedAt.Time,
	}
}

// correlatedUpsertParams widens the insert parameters to the correlated
// upsert's. The columns it adds are ones the ingest path always leaves empty.
//
// The id only takes effect when the upsert inserts rather than promotes, and
// there it is what makes a redelivery a primary key conflict.
func correlatedUpsertParams(params chatRepo.CreateChatMessageIdempotentParams) chatRepo.UpsertCorrelatedChatMessageParams {
	return chatRepo.UpsertCorrelatedChatMessageParams{
		ID:                params.ID,
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
	if n > 0 {
		if err := p.meterStored(ctx, tx, storageReadingInput(session, params)); err != nil {
			return false, err
		}
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
		Cwd:            conv.ToPGTextEmpty(session.GetCwd()),
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
