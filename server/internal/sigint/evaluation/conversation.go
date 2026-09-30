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
	"github.com/speakeasy-api/gram/server/internal/streams"
)

// ConversationMessageKind namespaces persisted conversation message identities.
const ConversationMessageKind = "conversation.message"

// ConversationHandler adapts conversation snapshots to the shared evaluator.
// Role policy, message UUID validation, and asset projection belong here.
type ConversationHandler struct {
	evaluator *Evaluator
	blobs     BlobReader
}

// NewConversationHandler binds the conversation receiver to shared evaluation.
func NewConversationHandler(evaluator *Evaluator, blobs BlobReader) *ConversationHandler {
	return &ConversationHandler{evaluator: evaluator, blobs: blobs}
}

// HandleBatchWithResult nacks only transiently failed messages in the batch.
func (h *ConversationHandler) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*conversationv1.Message]) error {
	var group errgroup.Group
	group.SetLimit(4)
	for _, message := range messages {
		group.Go(func() error {
			if err := h.Handle(ctx, message.Message, message.Metadata); err != nil {
				message.Fail(err)
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return fmt.Errorf("evaluate conversation batch: %w", err)
	}
	return nil
}

// Handle evaluates user/assistant snapshots, including historical imports.
func (h *ConversationHandler) Handle(ctx context.Context, m *conversationv1.Message, _ gcp.MessageMetadata) error {
	if m == nil {
		var event Event
		h.evaluator.terminal(ctx, event, "invalid_message")
		return nil
	}
	if m.GetRole() != conversationv1.Message_ROLE_USER && m.GetRole() != conversationv1.Message_ROLE_ASSISTANT {
		return nil
	}
	in := &conversationInput{message: m, blobs: h.blobs}
	for _, id := range []string{m.GetId(), m.GetConversationId()} {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed == uuid.Nil {
			h.evaluator.terminal(ctx, in.Event(), "invalid_identity")
			return nil
		}
	}
	return h.evaluator.Evaluate(ctx, in)
}

type conversationInput struct {
	message *conversationv1.Message
	blobs   BlobReader
}

func (in *conversationInput) Resolve(ctx context.Context) (classifier.Entry, error) {
	return input(ctx, in.blobs, in.message)
}

func (in *conversationInput) Event() Event {
	m := in.message
	messageContext := &sigintv1.Reading_ConversationMessage{}
	messageContext.SetConversationId(m.GetConversationId())
	messageContext.SetRole(sigintv1.Reading_ConversationMessage_ROLE_USER)
	if m.GetRole() == conversationv1.Message_ROLE_ASSISTANT {
		messageContext.SetRole(sigintv1.Reading_ConversationMessage_ROLE_ASSISTANT)
	}
	subject := &sigintv1.Reading_Event{}
	subject.SetKind(ConversationMessageKind)
	subject.SetId(m.GetId())
	subject.SetOccurredAt(m.GetCreatedAt())
	subject.SetConversationMessage(messageContext)
	event := Event{OrganizationID: m.GetOrganizationId(), ProjectID: m.GetProjectId(), Subject: subject, Actor: nil, BillingUserID: nil, Source: nil, Account: nil, AssistantID: nil, Replayed: nil}
	if provenance := m.GetProvenance(); provenance != nil {
		actor := &sigintv1.Reading_Actor{}
		if provenance.HasUserId() {
			actor.SetUserId(provenance.GetUserId())
		}
		if provenance.HasExternalUserId() {
			actor.SetExternalUserId(provenance.GetExternalUserId())
		}
		if provenance.HasUserEmail() {
			actor.SetUserEmail(provenance.GetUserEmail())
		}
		if actor.HasUserId() || actor.HasExternalUserId() || actor.HasUserEmail() {
			event.Actor = actor
		}
		if provenance.HasBillingUserId() {
			event.BillingUserID = new(provenance.GetBillingUserId())
		}
		if provenance.HasSource() {
			event.Source = new(provenance.GetSource())
		}
		if provenance.HasAssistantId() {
			event.AssistantID = new(provenance.GetAssistantId())
		}
		if provenance.HasReplayed() {
			event.Replayed = new(provenance.GetReplayed())
		}
		if source := provenance.GetAccount(); source != nil {
			account := &sigintv1.Reading_Account{}
			if source.HasUserAccountId() {
				account.SetUserAccountId(source.GetUserAccountId())
			}
			if source.HasAccountType() {
				account.SetAccountType(source.GetAccountType())
			}
			if source.HasBillingMode() {
				account.SetBillingMode(source.GetBillingMode())
			}
			event.Account = account
		}
	}
	return event
}
