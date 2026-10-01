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
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// maxInlineConversationBodyBytes leaves 1 MiB for event metadata below the
// outbox's 9 MiB serialized-message limit before spilling the body to an asset.
const maxInlineConversationBodyBytes = 8 << 20 // 8 MiB

// PreparedPublications holds bodies and assets prepared before a transaction.
// Non-body fields (such as a generation selected under a lock) may still change.
// Changing a prepared body's content requires preparing it again.
type PreparedPublications struct {
	// projectID prevents reusing another tenant's prepared references.
	projectID uuid.UUID

	// messages follows the input write order, independently of IDs assigned later.
	messages []preparedPublication
}

// preparedPublication owns a snapshot of body inputs as well as their prepared
// representation. Checking the inputs avoids rebuilding from jsonb-normalized
// database bytes while rejecting caller mutations after preparation.
type preparedPublication struct {
	content     string
	raw         []byte
	toolCalls   []byte
	asset       pgtype.Text
	attachments []publicationAttachment
	body        *conversationv1.Message_Body
	reference   *conversationv1.Message_ContentReference
}

type publicationAttachment struct {
	uri        string
	externalID pgtype.Text
	metadata   []byte
}

// publicationAttachmentMetadata is the stored attachment metadata used by the
// provider-independent conversation contract.
type publicationAttachmentMetadata struct {
	// DisplayPath is the source-observed filename or display path.
	DisplayPath string `json:"display_path"`
}

func (p preparedPublication) matches(write MessageWrite, attached []repo.CreateChatContentPartParams) bool {
	w := write.Params
	if p.content != w.Content || !bytes.Equal(p.raw, w.ContentRaw) || !bytes.Equal(p.toolCalls, w.ToolCalls) || p.asset != w.ContentAssetUrl || len(p.attachments) != len(attached) {
		return false
	}
	for i, part := range attached {
		if p.attachments[i].uri != part.ContentAssetUrl || p.attachments[i].externalID != part.ExternalID || !bytes.Equal(p.attachments[i].metadata, part.Metadata) {
			return false
		}
	}
	return true
}

// PreparePublications uploads oversized bodies before the caller opens its
// transaction. Message rows and outbox entries must still be committed together.
// Failed or deduplicated writes can leave unreferenced content-addressed assets.
func (w *ChatMessageWriter) PreparePublications(ctx context.Context, projectID uuid.UUID, writes []MessageWrite) (*PreparedPublications, error) {
	return w.preparePublications(ctx, projectID, writes, nil)
}

func (w *ChatMessageWriter) preparePublications(ctx context.Context, projectID uuid.UUID, writes []MessageWrite, attached map[uuid.UUID][]repo.CreateChatContentPartParams) (*PreparedPublications, error) {
	prepared := &PreparedPublications{projectID: projectID, messages: make([]preparedPublication, 0, len(writes))}
	refs := make(map[[32]byte]*conversationv1.Message_ContentReference)
	for _, write := range writes {
		p := write.Params
		if p.ProjectID != projectID {
			return nil, fmt.Errorf("publication project does not match message")
		}
		item := preparedPublication{content: p.Content, raw: bytes.Clone(p.ContentRaw), toolCalls: bytes.Clone(p.ToolCalls), asset: p.ContentAssetUrl, attachments: nil, body: nil, reference: nil}
		for _, part := range attached[p.ID] {
			item.attachments = append(item.attachments, publicationAttachment{uri: part.ContentAssetUrl, externalID: part.ExternalID, metadata: bytes.Clone(part.Metadata)})
		}
		body, err := publicationBody(item.content, item.raw, item.toolCalls, item.asset, attached[p.ID])
		if err != nil {
			return nil, err
		}
		if proto.Size(body) <= maxInlineConversationBodyBytes {
			item.body = body
			prepared.messages = append(prepared.messages, item)
			continue
		}
		var marshalOptions proto.MarshalOptions
		marshalOptions.Deterministic = true
		data, err := marshalOptions.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode conversation body: %w", err)
		}
		key := sha256.Sum256(data)
		ref := refs[key]
		if ref == nil {
			ref, err = w.storePublicationContent(ctx, projectID, data, "application/x-protobuf", ".pb")
			if err != nil {
				return nil, err
			}
			refs[key] = ref
		}
		item.reference = ref
		prepared.messages = append(prepared.messages, item)
	}
	return prepared, nil
}

// enqueueMessages publishes inserted rows. Callers exclude metadata-only
// correlated promotions and conflict no-ops, and pass the mutation transaction.
// The snapshot's text precedes tool calls because storage does not retain their
// original interleaving. Attached content parts follow in supplied order.
func (w *ChatMessageWriter) enqueueMessages(ctx context.Context, tx repo.DBTX, organizationID string, projectID uuid.UUID, writes []MessageWrite, attached map[uuid.UUID][]repo.CreateChatContentPartParams, producedAt time.Time, prepared *PreparedPublications) error {
	if prepared != nil && prepared.projectID != projectID {
		return fmt.Errorf("prepared publication belongs to another project")
	}
	if len(writes) == 0 {
		return nil
	}
	if prepared == nil || len(prepared.messages) != len(writes) {
		return fmt.Errorf("conversation bodies require preparation before transaction")
	}
	ids := make([]uuid.UUID, len(writes))
	metadata := make(map[uuid.UUID]MessageWrite, len(writes))
	bodies := make(map[uuid.UUID]preparedPublication, len(writes))
	for i, write := range writes {
		if !prepared.messages[i].matches(write, attached[write.Params.ID]) {
			return fmt.Errorf("conversation body changed after preparation")
		}
		ids[i] = write.Params.ID
		metadata[write.Params.ID] = write
		bodies[write.Params.ID] = prepared.messages[i]
	}
	rows, err := repo.New(tx).GetMessagesForPublication(ctx, repo.GetMessagesForPublicationParams{ProjectID: projectID, Ids: ids})
	if err != nil {
		return fmt.Errorf("load persisted messages for publication: %w", err)
	}
	if len(rows) != len(writes) {
		return fmt.Errorf("publication requires one persisted row per message")
	}
	publications := make([]outbox.Message, 0, len(rows))
	for _, p := range rows {
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
		if write.BillingUserID != "" {
			provenance.SetBillingUserId(write.BillingUserID)
		}
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
		if p.UserAccountID.Valid {
			account.SetUserAccountId(p.UserAccountID.UUID.String())
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
		if p.ExternalChatID.Valid {
			conversation.SetExternalConversationId(p.ExternalChatID.String)
		}
		if p.Cwd.Valid {
			conversation.SetWorkingDirectory(p.Cwd.String)
		}
		if proto.Size(conversation) > 0 {
			msg.SetConversationContext(conversation)
		}

		// Only insertions publish. Their body is exactly the prepared write input;
		// persisted rows supply identity and attribution, not a second body build.
		body := bodies[p.ID]
		if body.reference != nil {
			msg.SetBodyReference(body.reference)
		} else {
			msg.SetBody(body.body)
		}
		publications = append(publications, outbox.Message{Proto: msg, PublicID: uuid.Nil, Attributes: map[string]string{"role": p.Role, "source": provenance.GetSource()}})
	}
	if _, err := outbox.PublishBatch(ctx, tx, organizationID, publications); err != nil {
		return fmt.Errorf("enqueue conversation messages: %w", err)
	}
	return nil
}

func publicationBody(content string, raw, toolCalls []byte, asset pgtype.Text, attached []repo.CreateChatContentPartParams) (*conversationv1.Message_Body, error) {
	body := &conversationv1.Message_Body{}
	parts := make([]*conversationv1.Message_Part, 0)
	if content != "" {
		part := &conversationv1.Message_Part{}
		part.SetText(content)
		parts = append(parts, part)
	}
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if len(toolCalls) > 0 {
		toolCalls = bytes.TrimSpace(toolCalls)
		// Legacy rows can contain a JSON string wrapping the tool-call array.
		if len(toolCalls) > 0 && toolCalls[0] == '"' {
			var encoded string
			if err := json.Unmarshal(toolCalls, &encoded); err != nil {
				return nil, fmt.Errorf("decode wrapped tool calls for publication: %w", err)
			}
			toolCalls = []byte(encoded)
		}
		if err := json.Unmarshal(toolCalls, &calls); err != nil {
			return nil, fmt.Errorf("decode persisted tool calls for publication: %w", err)
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
	for _, attachedPart := range attached {
		ref := &conversationv1.Message_ContentReference{}
		ref.SetUri(attachedPart.ContentAssetUrl)
		ref.SetMediaType("text/plain; charset=utf-8")
		if attachedPart.ExternalID.Valid {
			ref.SetExternalId(attachedPart.ExternalID.String)
		}
		if len(attachedPart.Metadata) > 0 {
			var metadata publicationAttachmentMetadata
			if err := json.Unmarshal(attachedPart.Metadata, &metadata); err != nil {
				return nil, fmt.Errorf("decode publication attachment metadata: %w", err)
			}
			if metadata.DisplayPath != "" {
				ref.SetFilename(metadata.DisplayPath)
			}
		}
		part := &conversationv1.Message_Part{}
		part.SetContentReference(ref)
		parts = append(parts, part)
	}
	body.SetParts(parts)
	if len(raw) > 0 {
		body.SetSourceContentJson(raw)
	} else if asset.Valid && asset.String != "" {
		ref := &conversationv1.Message_ContentReference{}
		ref.SetUri(asset.String)
		ref.SetMediaType("application/json")
		body.SetSourceContent(ref)
	}
	return body, nil
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

// externalPublication carries body preparation fields and producer-only provenance
// alongside the ID used to load the authoritative imported row.
func externalPublication(write ExternalMessageWrite) MessageWrite {
	var params repo.CreateChatMessageParams
	params.ID = write.Params.ID
	params.ProjectID = write.Params.ProjectID
	params.Content = write.Params.Content
	params.ContentRaw = write.Params.ContentRaw
	params.ToolCalls = write.Params.ToolCalls
	params.ContentAssetUrl = write.Params.ContentAssetUrl
	if write.PublishRowLocalContent {
		// Archival content may contain the whole source message on every split
		// row. Publishing it would reevaluate sibling content under each row ID.
		params.ContentRaw = nil
		params.ContentAssetUrl = pgtype.Text{String: "", Valid: false}
	}
	return MessageWrite{Params: params, BillingUserID: write.BillingUserID, AssistantID: uuid.Nil, WorkloadSource: write.WorkloadSource, UserEmail: write.UserEmail, Provider: write.Provider, HookHostname: write.HookHostname, AccountType: write.AccountType, BillingMode: write.BillingMode}
}
