package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// enqueueMessages publishes creation events in the insertion transaction.
// Callers exclude correlated promotions and conflict no-ops. Consumers resolve
// message content from storage; publication never reads or uploads body assets.
func (w *ChatMessageWriter) enqueueMessages(ctx context.Context, tx repo.DBTX, organizationID string, projectID uuid.UUID, writes []MessageWrite, occurredAt time.Time) error {
	if len(writes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(writes))
	metadata := make(map[uuid.UUID]MessageWrite, len(writes))
	for i, write := range writes {
		if write.Params.ProjectID != projectID {
			return fmt.Errorf("publication project does not match message")
		}
		ids[i] = write.Params.ID
		metadata[write.Params.ID] = write
	}
	rows, err := repo.New(tx).GetMessagesForPublication(ctx, repo.GetMessagesForPublicationParams{ProjectID: projectID, Ids: ids})
	if err != nil {
		return fmt.Errorf("load persisted messages for publication: %w", err)
	}
	if len(rows) != len(writes) {
		return fmt.Errorf("publication requires one persisted row per message")
	}
	publications := make([]outbox.Message, 0, len(rows))
	for _, row := range rows {
		write := metadata[row.ID]
		role, ok := conversationv1.MessageEvent_Role_value["ROLE_"+strings.ToUpper(row.Role)]
		if !ok {
			return fmt.Errorf("unsupported persisted message role %q", row.Role)
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate conversation event id: %w", err)
		}
		event := conversationv1.MessageEvent_builder{Id: new(id.String())}.Build()
		event.SetType(conversationv1.MessageEvent_TYPE_CREATED)
		event.SetOrganizationId(organizationID)
		event.SetProjectId(projectID.String())
		event.SetConversationId(row.ChatID.String())
		event.SetMessageId(row.ID.String())
		event.SetRole(conversationv1.MessageEvent_Role(role))
		event.SetOccurredAt(occurredAt.UTC().Format(time.RFC3339Nano))
		event.SetMessageCreatedAt(row.CreatedAt.Time.UTC().Format(time.RFC3339Nano))
		ingestion := &conversationv1.MessageEvent_IngestionContext{}
		if row.Source.Valid {
			ingestion.SetSource(CanonicalSource(row.Source.String))
		}
		if write.BillingUserID != "" {
			ingestion.SetBillingUserId(write.BillingUserID)
		}
		if write.AssistantID != uuid.Nil {
			ingestion.SetAssistantId(write.AssistantID.String())
		}
		if write.UserEmail != "" {
			ingestion.SetObservedUserEmail(write.UserEmail)
		}
		if write.Provider != "" {
			ingestion.SetProvider(write.Provider)
		}
		if write.WorkloadSource == metering.WorkloadSourceHook && row.Source.Valid {
			ingestion.SetHookSource(row.Source.String)
		}
		if write.HookHostname != "" {
			ingestion.SetHostname(write.HookHostname)
		}
		if write.AccountType != "" {
			ingestion.SetAccountType(write.AccountType)
		}
		if write.BillingMode != "" {
			ingestion.SetBillingMode(write.BillingMode)
		}
		ingestion.SetReplayed(row.Replayed)
		event.SetIngestion(ingestion)
		publications = append(publications, outbox.Message{Proto: event, PublicID: id, Attributes: map[string]string{
			"type": event.GetType().String(), "role": row.Role, "source": ingestion.GetSource(),
		}})
	}
	if _, err := outbox.PublishBatch(ctx, tx, organizationID, publications); err != nil {
		return fmt.Errorf("enqueue conversation events: %w", err)
	}
	return nil
}

// externalPublication carries identity and producer-only provenance for an
// imported message. Body fields are unnecessary for reference-only events.
func externalPublication(write ExternalMessageWrite) MessageWrite {
	var params repo.CreateChatMessageParams
	params.ID = write.Params.ID
	params.ProjectID = write.Params.ProjectID
	return MessageWrite{Params: params, BillingUserID: write.BillingUserID, AssistantID: uuid.Nil, WorkloadSource: write.WorkloadSource, UserEmail: write.UserEmail, Provider: write.Provider, HookHostname: write.HookHostname, AccountType: write.AccountType, BillingMode: write.BillingMode}
}
