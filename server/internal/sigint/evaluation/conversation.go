package evaluation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	sigintv1 "github.com/speakeasy-api/gram/infra/gen/gram/sigint/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// ConversationMessageKind namespaces persisted conversation message identities.
const ConversationMessageKind = "conversation.message"

// MessageSource resolves a tenant's persisted messages in a single batch.
type MessageSource interface {
	LoadMessages(context.Context, string, uuid.UUID, []uuid.UUID) ([]repo.LoadEvaluationMessagesRow, error)
}

// LoadMessages reads current message state and attachment locators together.
func (r *Repository) LoadMessages(ctx context.Context, org string, project uuid.UUID, ids []uuid.UUID) ([]repo.LoadEvaluationMessagesRow, error) {
	rows, err := repo.New(r.db).LoadEvaluationMessages(ctx, repo.LoadEvaluationMessagesParams{OrganizationID: org, ProjectID: project, MessageIds: ids})
	if err != nil {
		return nil, fmt.Errorf("load evaluation messages: %w", err)
	}

	return rows, nil
}

// ConversationHandler resolves creation events before evaluating their content.
type ConversationHandler struct {
	evaluator *Evaluator
	blobs     BlobReader
	messages  MessageSource
}

// NewConversationHandler binds the receiver to current-state message storage.
func NewConversationHandler(evaluator *Evaluator, blobs BlobReader, messages MessageSource) *ConversationHandler {
	return &ConversationHandler{evaluator: evaluator, blobs: blobs, messages: messages}
}

func (h *ConversationHandler) eligible(ctx context.Context, m *conversationv1.MessageEvent) bool {
	if m == nil || m.GetType() != conversationv1.MessageEvent_TYPE_CREATED {
		return false
	}
	if m.GetRole() != conversationv1.MessageEvent_ROLE_USER && m.GetRole() != conversationv1.MessageEvent_ROLE_ASSISTANT {
		return false
	}
	for _, id := range []string{m.GetId(), m.GetMessageId(), m.GetConversationId(), m.GetProjectId()} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil {
			var event Event
			h.evaluator.terminal(ctx, event, "invalid_identity")
			return false
		}
	}
	return m.GetOrganizationId() != ""
}

// HandleBatchWithResult reads once per organization/project, not once per event.
// A failed tenant read nacks only that tenant's events. Missing/deleted rows are
// acknowledged; redelivery cannot restore them. Non-created events are ignored.
func (h *ConversationHandler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*conversationv1.MessageEvent]) error {
	type tenant struct {
		org     string
		project uuid.UUID
	}
	batches := make(map[tenant][]streams.BatchMessage[*conversationv1.MessageEvent])
	for _, m := range messages {
		if h.eligible(ctx, m.Message) {
			key := tenant{org: m.Message.GetOrganizationId(), project: uuid.MustParse(m.Message.GetProjectId())}
			batches[key] = append(batches[key], m)
		}
	}
	var group errgroup.Group
	group.SetLimit(4)
	for key, batch := range batches {
		ids := make([]uuid.UUID, 0, len(batch))
		seen := make(map[uuid.UUID]bool, len(batch))
		for _, m := range batch {
			id := uuid.MustParse(m.Message.GetMessageId())
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}

		rows, err := h.messages.LoadMessages(ctx, key.org, key.project, ids)
		if err != nil {
			for _, m := range batch {
				m.Fail(err)
			}
			continue
		}

		byID := make(map[uuid.UUID]repo.LoadEvaluationMessagesRow, len(rows))
		for _, row := range rows {
			byID[row.ChatMessage.ID] = row
		}
		for _, m := range batch {
			row, ok := byID[uuid.MustParse(m.Message.GetMessageId())]
			if !ok {
				continue
			}
			group.Go(func() error {
				if err := h.evaluate(ctx, m.Message, row); err != nil {
					m.Fail(err)
				}

				return nil
			})
		}
	}

	if err := group.Wait(); err != nil {
		return fmt.Errorf("evaluate conversation batch: %w", err)
	}

	return nil
}

// Handle supports individual deliveries using the same tenant-pinned lookup.
func (h *ConversationHandler) Handle(ctx context.Context, m *conversationv1.MessageEvent, _ gcp.MessageMetadata) error {
	if !h.eligible(ctx, m) {
		return nil
	}

	rows, err := h.messages.LoadMessages(ctx, m.GetOrganizationId(), uuid.MustParse(m.GetProjectId()), []uuid.UUID{uuid.MustParse(m.GetMessageId())})
	if err != nil {
		return fmt.Errorf("load conversation message: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}
	return h.evaluate(ctx, m, rows[0])
}

func (h *ConversationHandler) evaluate(ctx context.Context, m *conversationv1.MessageEvent, row repo.LoadEvaluationMessagesRow) error {
	p := row.ChatMessage
	if p.ID.String() != m.GetMessageId() || p.ProjectID.UUID.String() != m.GetProjectId() || p.ChatID.String() != m.GetConversationId() {
		var event Event
		h.evaluator.terminal(ctx, event, "invalid_identity")
		return nil
	}
	if p.Role != "user" && p.Role != "assistant" {
		return nil
	}
	return h.evaluator.Evaluate(ctx, &conversationInput{message: m, stored: row, blobs: h.blobs})
}

type conversationInput struct {
	message *conversationv1.MessageEvent
	stored  repo.LoadEvaluationMessagesRow
	blobs   BlobReader
}

func (in *conversationInput) Resolve(ctx context.Context) (classifier.Entry, error) {
	return input(ctx, in.blobs, in.message.GetProjectId(), in.stored)
}

func (in *conversationInput) MatchingMessage() *matching.Message {
	return &matching.Message{Role: in.stored.ChatMessage.Role}
}

func (in *conversationInput) Event() Event {
	m := in.message
	p := in.stored.ChatMessage
	messageContext := &sigintv1.Reading_ConversationMessage{}
	messageContext.SetConversationId(m.GetConversationId())
	messageContext.SetRole(sigintv1.Reading_ConversationMessage_ROLE_USER)
	if p.Role == "assistant" {
		messageContext.SetRole(sigintv1.Reading_ConversationMessage_ROLE_ASSISTANT)
	}
	subject := &sigintv1.Reading_Event{}
	subject.SetKind(ConversationMessageKind)
	subject.SetId(m.GetMessageId())
	subject.SetOccurredAt(m.GetMessageCreatedAt())
	subject.SetConversationMessage(messageContext)
	event := Event{OrganizationID: m.GetOrganizationId(), ProjectID: m.GetProjectId(), Subject: subject, Actor: nil, BillingUserID: nil, Source: nil, Account: nil, AssistantID: nil, Replayed: nil}
	actor := &sigintv1.Reading_Actor{}
	if p.UserID.Valid {
		actor.SetUserId(p.UserID.String)
	}
	if p.ExternalUserID.Valid {
		actor.SetExternalUserId(p.ExternalUserID.String)
	}
	account := &sigintv1.Reading_Account{}
	if in.stored.UserAccountID.Valid {
		account.SetUserAccountId(in.stored.UserAccountID.UUID.String())
	}

	if source := m.GetIngestion(); source != nil {
		if source.HasObservedUserEmail() {
			actor.SetUserEmail(source.GetObservedUserEmail())
		}
		if source.HasBillingUserId() {
			event.BillingUserID = new(source.GetBillingUserId())
		}
		if source.HasSource() {
			event.Source = new(source.GetSource())
		}
		if source.HasAssistantId() {
			event.AssistantID = new(source.GetAssistantId())
		}
		if source.HasReplayed() {
			event.Replayed = new(source.GetReplayed())
		}
		if source.HasAccountType() {
			account.SetAccountType(source.GetAccountType())
		}
		if source.HasBillingMode() {
			account.SetBillingMode(source.GetBillingMode())
		}
	}

	if actor.HasUserId() || actor.HasExternalUserId() || actor.HasUserEmail() {
		event.Actor = actor
	}
	if account.HasUserAccountId() || account.HasAccountType() || account.HasBillingMode() {
		event.Account = account
	}
	return event
}
