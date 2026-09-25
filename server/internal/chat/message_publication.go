package chat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// enqueueMessages publishes inserted rows and successful correlated promotions.
// Callers exclude conflict no-ops and pass the mutation transaction.
// The snapshot's text precedes tool calls because storage does not retain their
// original interleaving. Attached content parts follow in supplied order.
func (w *ChatMessageWriter) enqueueMessages(ctx context.Context, tx repo.DBTX, organizationID string, projectID uuid.UUID, writes []MessageWrite, attached map[uuid.UUID][]repo.CreateChatContentPartParams, producedAt time.Time) error {
	if len(writes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(writes))
	metadata := make(map[uuid.UUID]MessageWrite, len(writes))
	for i, write := range writes {
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
		p := row.ChatMessage
		write := metadata[p.ID]
		role, ok := conversationv1.Message_Role_value["ROLE_"+strings.ToUpper(p.Role)]
		if !ok {
			return fmt.Errorf("unsupported persisted message role %q", p.Role)
		}
		msg := conversationv1.Message_builder{Id: new(p.ID.String())}.Build()
		msg.SetOrganizationId(organizationID)
		msg.SetProjectId(projectID.String())
		msg.SetConversationId(p.ChatID.String())
		msg.SetRole(conversationv1.Message_Role(role))
		msg.SetCreatedAt(p.CreatedAt.Time.UTC().Format(time.RFC3339Nano))
		msg.SetProducedAt(producedAt.UTC().Format(time.RFC3339Nano))
		if p.MessageID.Valid {
			msg.SetCorrelationId(p.MessageID.String)
		}
		if p.ToolCallID.Valid {
			msg.SetToolCallId(p.ToolCallID.String)
		}
		if p.FinishReason.Valid {
			msg.SetFinishReason(p.FinishReason.String)
		}
		provenance := &conversationv1.Message_Provenance{}
		if p.Source.Valid {
			provenance.SetSource(CanonicalSource(p.Source.String))
		}
		if p.UserID.Valid {
			provenance.SetUserId(p.UserID.String)
		}
		if p.ExternalUserID.Valid {
			provenance.SetExternalUserId(p.ExternalUserID.String)
		}
		if p.ExternalMessageID.Valid {
			provenance.SetExternalMessageId(p.ExternalMessageID.String)
		}
		if p.Model.Valid {
			provenance.SetModel(p.Model.String)
		}
		if p.UserAgent.Valid {
			provenance.SetUserAgent(p.UserAgent.String)
		}
		provenance.SetReplayed(p.Replayed)
		if write.Provider != "" {
			provenance.SetProvider(write.Provider)
		}
		if write.UserEmail != "" {
			provenance.SetUserEmail(write.UserEmail)
		}
		if write.HookHostname != "" {
			provenance.SetHostname(write.HookHostname)
		}
		if write.AssistantID != uuid.Nil {
			provenance.SetAssistantId(write.AssistantID.String())
		}
		if write.WorkloadSource == metering.WorkloadSourceHook && p.Source.Valid {
			provenance.SetHookSource(p.Source.String)
		}
		account := &conversationv1.Message_Account{}
		if row.UserAccountID.Valid {
			account.SetUserAccountId(row.UserAccountID.UUID.String())
		}
		if write.AccountType != "" {
			account.SetAccountType(write.AccountType)
		}
		if write.BillingMode != "" {
			account.SetBillingMode(write.BillingMode)
		}
		if proto.Size(account) > 0 {
			provenance.SetAccount(account)
		}
		msg.SetProvenance(provenance)
		conversation := &conversationv1.Message_ConversationContext{}
		if row.ExternalChatID.Valid {
			conversation.SetExternalConversationId(row.ExternalChatID.String)
		}
		if row.Cwd.Valid {
			conversation.SetWorkingDirectory(row.Cwd.String)
		}
		if proto.Size(conversation) > 0 {
			msg.SetConversationContext(conversation)
		}

		body := &conversationv1.Message_Body{}
		parts := make([]*conversationv1.Message_Part, 0)
		if p.Content != "" {
			part := &conversationv1.Message_Part{}
			part.SetText(p.Content)
			parts = append(parts, part)
		}
		var calls []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if len(p.ToolCalls) > 0 {
			toolCalls := bytes.TrimSpace(p.ToolCalls)
			// Legacy rows can contain a JSON string wrapping the tool-call array.
			if len(toolCalls) > 0 && toolCalls[0] == '"' {
				var encoded string
				if err := json.Unmarshal(toolCalls, &encoded); err != nil {
					return fmt.Errorf("decode wrapped tool calls for publication: %w", err)
				}
				toolCalls = []byte(encoded)
			}
			if err := json.Unmarshal(toolCalls, &calls); err != nil {
				return fmt.Errorf("decode persisted tool calls for publication: %w", err)
			}
		}
		for _, call := range calls {
			tool := &conversationv1.Message_ToolCall{}
			tool.SetId(call.ID)
			tool.SetName(call.Function.Name)
			tool.SetArgumentsJson(call.Function.Arguments)
			part := &conversationv1.Message_Part{}
			part.SetToolCall(tool)
			parts = append(parts, part)
		}
		for _, attachedPart := range attached[p.ID] {
			ref := &conversationv1.Message_ContentReference{}
			ref.SetUri(attachedPart.ContentAssetUrl)
			ref.SetMediaType("text/plain; charset=utf-8")
			part := &conversationv1.Message_Part{}
			part.SetContentReference(ref)
			parts = append(parts, part)
		}
		body.SetParts(parts)
		if len(p.ContentRaw) > 0 {
			body.SetSourceContentJson(p.ContentRaw)
		} else if p.ContentAssetUrl.Valid && p.ContentAssetUrl.String != "" {
			ref := &conversationv1.Message_ContentReference{}
			ref.SetUri(p.ContentAssetUrl.String)
			ref.SetMediaType("application/json")
			body.SetSourceContent(ref)
		}
		msg.SetBody(body)
		// Keep normal events inline. Exceptional large bodies are immutable blobs,
		// leaving ample room below the outbox's 9 MiB serialized-message limit.
		if proto.Size(body) > 1024*1024 {
			var marshalOptions proto.MarshalOptions
			marshalOptions.Deterministic = true
			data, err := marshalOptions.Marshal(body)
			if err != nil {
				return fmt.Errorf("encode conversation body: %w", err)
			}
			ref, err := w.storePublicationContent(ctx, projectID, data, "application/x-protobuf", ".pb")
			if err != nil {
				return err
			}
			msg.SetBodyReference(ref)
		}
		publications = append(publications, outbox.Message{Proto: msg, PublicID: uuid.Nil, Attributes: map[string]string{"role": p.Role, "source": provenance.GetSource()}})
	}
	if _, err := outbox.PublishBatch(ctx, tx, organizationID, publications); err != nil {
		return fmt.Errorf("enqueue conversation messages: %w", err)
	}
	return nil
}

func (w *ChatMessageWriter) storePublicationContent(ctx context.Context, projectID uuid.UUID, data []byte, mediaType, extension string) (*conversationv1.Message_ContentReference, error) {
	if w.assetStorage == nil {
		return nil, fmt.Errorf("conversation publication asset storage unavailable")
	}
	sum := sha256.Sum256(data)
	key := path.Join(projectID.String(), "conversations", "publications", hex.EncodeToString(sum[:])+extension)
	writer, uri, err := w.assetStorage.Write(ctx, key, mediaType, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open conversation publication asset: %w", err)
	}
	if _, err := io.Copy(writer, bytes.NewReader(data)); err != nil {
		defer o11y.NoLogDefer(writer.Close)
		return nil, fmt.Errorf("write conversation publication asset: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close conversation publication asset: %w", err)
	}
	ref := &conversationv1.Message_ContentReference{}
	ref.SetUri(uri.String())
	ref.SetMediaType(mediaType)
	ref.SetSizeBytes(uint64(len(data)))
	ref.SetSha256(sum[:])
	return ref, nil
}

// externalPublication carries producer-only provenance alongside the ID used to
// load the authoritative imported row. Other Params fields are not consumed.
func externalPublication(write ExternalMessageWrite) MessageWrite {
	var params repo.CreateChatMessageParams
	params.ID = write.Params.ID
	return MessageWrite{Params: params, BillingUserID: write.BillingUserID, AssistantID: uuid.Nil, WorkloadSource: write.WorkloadSource, UserEmail: write.UserEmail, Provider: write.Provider, HookHostname: write.HookHostname, AccountType: write.AccountType, BillingMode: write.BillingMode}
}
