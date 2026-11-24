package transcriptcrypto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	encryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

const column = "chat_message_transcripts.ciphertext"
const version = 1

// Message contains only the sensitive fields covered by the transcript codec.
// Origin, metadata, assets and derived data are outside this
// message-body payload. Raw JSON fields must be valid JSON or nil; nil stays
// distinct from an explicit JSON null when the message is opened.
type Message struct {
	StorageError      *string         `json:"storage_error"`
	Content           string          `json:"content"`
	ContentRaw        json.RawMessage `json:"content_raw,omitempty"`
	ToolCalls         json.RawMessage `json:"tool_calls,omitempty"`
	ToolOutcomeNotes  *string         `json:"tool_outcome_notes"`
	ToolCallSummaries json.RawMessage `json:"tool_call_summaries,omitempty"`
}

// Identity binds a body to its tenant-owned project, chat and message rows.
// Legacy messages without a project require explicit backfill before encryption.
type Identity struct {
	ProjectID uuid.UUID `json:"project_id"`
	ChatID    uuid.UUID `json:"chat_id"`
	MessageID uuid.UUID `json:"message_id"`
}

type payload struct {
	Version int `json:"version"`
	Identity
	Message *Message `json:"message"`
}

// Codec uses a concrete organization-scoped encryptor, never a caller-selected
// use case. Construct it with New; its zero value refuses all operations.
type Codec struct{ encryptor *orgencryption.Encryptor }

// New scopes the codec to an organization and the chat-transcripts use case.
// It performs no I/O; key availability is checked by encryption operations.
func New(service *orgencryption.Service, organizationID string) (*Codec, error) {
	if service == nil || strings.TrimSpace(organizationID) == "" || strings.ContainsRune(organizationID, '\x00') {
		return nil, errors.New("transcript encryption requires a service and organization")
	}
	encryptor, err := service.For(organizationID, orgencryption.UseCaseChatTranscripts)
	if err != nil {
		return nil, &operationError{operation: "scope transcript encryption", cause: err}
	}
	return &Codec{encryptor: encryptor}, nil
}

// NewIn scopes key/configuration reads to the caller's database transaction.
// It avoids acquiring a second pooled connection while the caller holds one.
// The returned codec must not outlive that transaction.
func NewIn(service *orgencryption.Service, db encryptionrepo.DBTX, organizationID string) (*Codec, error) {
	if db == nil {
		return nil, errors.New("transcript encryption requires a transaction")
	}
	codec, err := New(service, organizationID)
	if err != nil {
		return nil, err
	}
	codec.encryptor, err = service.ForIn(db, organizationID, orgencryption.UseCaseChatTranscripts)
	if err != nil {
		return nil, &operationError{operation: "scope transcript encryption transaction", cause: err}
	}
	return codec, nil
}

type cryptor interface {
	Encrypt(context.Context, string, []byte) (string, error)
	Decrypt(context.Context, string, string) ([]byte, error)
}

// Encrypt seals a versioned payload bound to all three row identities.
func (c *Codec) Encrypt(ctx context.Context, identity Identity, message Message) (string, error) {
	if c == nil || c.encryptor == nil {
		return "", errors.New("transcript codec is not initialized")
	}
	return encrypt(ctx, c.encryptor, identity, message)
}

// Decrypt authenticates the payload and checks the expected row identities.
// Every failure returns a zero Message, never partial plaintext.
func (c *Codec) Decrypt(ctx context.Context, identity Identity, ciphertext string) (Message, error) {
	if c == nil || c.encryptor == nil {
		return Message{}, errors.New("transcript codec is not initialized")
	}
	return decrypt(ctx, c.encryptor, identity, ciphertext)
}

func encrypt(ctx context.Context, e cryptor, identity Identity, message Message) (string, error) {
	if identity.ProjectID == uuid.Nil || identity.MessageID == uuid.Nil || identity.ChatID == uuid.Nil {
		return "", errors.New("transcript project, message and chat identities are required")
	}
	// omitempty preserves absent raw values (SQL NULL) independently of an
	// explicit JSON null. Validate first so an empty nonnil value is not
	// silently omitted as though it were absent.
	for _, raw := range []json.RawMessage{message.ContentRaw, message.ToolCalls, message.ToolCallSummaries} {
		if raw != nil && !json.Valid(raw) {
			return "", errors.New("encode transcript payload")
		}
	}
	plaintext, err := json.Marshal(payload{Version: version, Identity: identity, Message: &message})
	if err != nil {
		return "", errors.New("encode transcript payload")
	}
	ciphertext, err := e.Encrypt(ctx, column, plaintext)
	if err != nil {
		return "", &operationError{operation: "encrypt transcript payload", cause: err}
	}
	if ciphertext == "" {
		return "", errors.New("encrypt transcript payload: empty ciphertext")
	}
	return ciphertext, nil
}

func decrypt(ctx context.Context, e cryptor, identity Identity, ciphertext string) (Message, error) {
	if identity.ProjectID == uuid.Nil || identity.MessageID == uuid.Nil || identity.ChatID == uuid.Nil {
		return Message{}, errors.New("transcript project, message and chat identities are required")
	}
	plaintext, err := e.Decrypt(ctx, column, ciphertext)
	if err != nil {
		return Message{}, &operationError{operation: "decrypt transcript payload", cause: err}
	}
	var p payload
	if err := json.Unmarshal(plaintext, &p); err != nil {
		return Message{}, fmt.Errorf("decode transcript payload: %w", ErrUnauthentic)
	}
	if p.Version != version || p.Identity != identity || p.Message == nil {
		return Message{}, fmt.Errorf("invalid transcript version or identity: %w", ErrUnauthentic)
	}
	return *p.Message, nil
}

// ErrUnauthentic marks a payload that decrypts but is not this message's: it
// does not decode, or it carries another version or identity.
var ErrUnauthentic = errors.New("transcript payload is not authentic")

// operationError preserves classification without displaying provider details.
type operationError struct {
	operation string
	cause     error
}

func (e *operationError) Error() string { return e.operation }
func (e *operationError) Unwrap() error { return e.cause }
