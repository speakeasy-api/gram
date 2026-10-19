package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

var (
	ErrKitRuntimeBinding         = errors.New("Kit runtime authority or session binding is unavailable")
	ErrKitRuntimeSessionConflict = errors.New("Kit session is already bound to another thread")
	ErrKitGatewayTokenNotFound   = errors.New("Kit gateway credential is not stored")
	errKitGatewayCredential      = errors.New("Kit gateway credential is unavailable")
)

// RuntimeID is the authority generation, not a backend process identifier.
type kitRuntimeSessionKey struct {
	ProjectID   uuid.UUID
	AssistantID uuid.UUID
	RuntimeID   uuid.UUID
	ThreadID    uuid.UUID
}

// ChatID is always the existing server thread's chat, never an ACP identifier.
// HasPriorTranscript carries existence only; it is not a replay instruction.
type kitRuntimeSession struct {
	SessionID          string
	ChatID             uuid.UUID
	HasPriorTranscript bool
}

// LoadKitRuntimeSession returns the canonical chat even when no ACP session has
// been recorded yet. A missing mapping is distinct from an invalid authority.
func (s *ServiceCore) LoadKitRuntimeSession(ctx context.Context, key kitRuntimeSessionKey) (kitRuntimeSession, bool, error) {
	tx, queries, err := s.beginKitRuntimeTransaction(ctx, key.ProjectID, key.AssistantID, key.RuntimeID)
	if err != nil {
		return kitRuntimeSession{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	session, found, err := loadKitSessionBinding(ctx, queries, key)
	if err != nil {
		return kitRuntimeSession{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return kitRuntimeSession{}, false, fmt.Errorf("commit Kit session read: %w", err)
	}
	return session, found, nil
}

// StoreKitRuntimeSession must complete before any prompt is sent. It never
// replaces an existing mapping: concurrent session/new responses converge on
// the persisted winner returned here. The caller must discard its own response
// if it differs and prompt only the returned session. An ambiguous session/new
// failure therefore leaves at most an empty remote orphan, never a lost prompt.
func (s *ServiceCore) StoreKitRuntimeSession(ctx context.Context, key kitRuntimeSessionKey, sessionID string) (kitRuntimeSession, error) {
	if sessionID == "" {
		return kitRuntimeSession{}, ErrKitRuntimeBinding
	}
	tx, queries, err := s.beginKitRuntimeTransaction(ctx, key.ProjectID, key.AssistantID, key.RuntimeID)
	if err != nil {
		return kitRuntimeSession{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existing, found, err := loadKitSessionBinding(ctx, queries, key)
	if err != nil {
		return kitRuntimeSession{}, err
	}
	if !found {
		_, err = queries.InsertKitRuntimeSession(ctx, assistantrepo.InsertKitRuntimeSessionParams{
			SessionID: sessionID, ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID, ThreadID: key.ThreadID,
		})
		if err != nil {
			if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.UniqueViolation {
				return kitRuntimeSession{}, ErrKitRuntimeSessionConflict
			}
			return kitRuntimeSession{}, fmt.Errorf("store Kit session binding: %w", err)
		}
		existing, found, err = loadKitSessionBinding(ctx, queries, key)
		if err != nil {
			return kitRuntimeSession{}, err
		}
		if !found {
			return kitRuntimeSession{}, ErrKitRuntimeBinding
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return kitRuntimeSession{}, fmt.Errorf("commit Kit session binding: %w", err)
	}
	return existing, nil
}

// ValidateKitRuntimeSessionInTx is for transcript callbacks. It acquires the
// same assistant fence as switching and retains row locks until the caller ends
// tx. The caller must append to the returned ChatID within that transaction;
// validating and then writing on a separate connection is not authoritative.
func (s *ServiceCore) ValidateKitRuntimeSessionInTx(ctx context.Context, tx pgx.Tx, key kitRuntimeSessionKey, sessionID string) (kitRuntimeSession, error) {
	if tx == nil || sessionID == "" {
		return kitRuntimeSession{}, ErrKitRuntimeBinding
	}
	queries := assistantrepo.New(tx)
	if err := queries.AcquireAssistantAdvisoryLock(ctx, key.AssistantID.String()); err != nil {
		return kitRuntimeSession{}, fmt.Errorf("lock Kit callback binding: %w", err)
	}
	binding, found, err := loadKitSessionBinding(ctx, queries, key)
	if err != nil {
		return kitRuntimeSession{}, err
	}
	if !found || binding.SessionID != sessionID {
		return kitRuntimeSession{}, ErrKitRuntimeBinding
	}
	return binding, nil
}

func loadKitSessionBinding(ctx context.Context, queries *assistantrepo.Queries, key kitRuntimeSessionKey) (kitRuntimeSession, bool, error) {
	row, err := queries.GetKitRuntimeSessionBinding(ctx, assistantrepo.GetKitRuntimeSessionBindingParams{
		ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID, ThreadID: key.ThreadID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return kitRuntimeSession{}, false, ErrKitRuntimeBinding
	}
	if err != nil {
		return kitRuntimeSession{}, false, fmt.Errorf("read Kit session binding: %w", err)
	}
	return kitRuntimeSession{SessionID: row.AcpSessionID.String, ChatID: row.ChatID, HasPriorTranscript: row.HasPriorTranscript}, row.AcpSessionID.Valid, nil
}

func (s *ServiceCore) beginKitRuntimeTransaction(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID) (pgx.Tx, *assistantrepo.Queries, error) {
	if projectID == uuid.Nil || assistantID == uuid.Nil || runtimeID == uuid.Nil {
		return nil, nil, ErrKitRuntimeBinding
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin Kit authority transaction: %w", err)
	}
	queries := assistantrepo.New(tx)
	if err := queries.AcquireAssistantAdvisoryLock(ctx, assistantID.String()); err != nil {
		_ = tx.Rollback(ctx)
		return nil, nil, fmt.Errorf("lock Kit runtime authority: %w", err)
	}
	_, err = queries.GetLiveKitRuntimeAuthority(ctx, assistantrepo.GetLiveKitRuntimeAuthorityParams{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID})
	if err != nil {
		_ = tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrKitRuntimeBinding
		}
		return nil, nil, fmt.Errorf("validate Kit runtime authority: %w", err)
	}
	return tx, queries, nil
}

// The encrypted envelope binds ciphertext to its immutable authority so copying
// a credential between rows cannot silently reconnect a different runtime.
type kitGatewayTokenEnvelope struct {
	ProjectID   uuid.UUID `json:"project_id"`
	AssistantID uuid.UUID `json:"assistant_id"`
	RuntimeID   uuid.UUID `json:"runtime_id"`
	Token       string    `json:"token"`
}

// StoreKitGatewayToken intentionally replaces the credential when the caller
// explicitly rotates it. Never place the token in backend_metadata_json or logs.
func (s *ServiceCore) StoreKitGatewayToken(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID, token string) error {
	if token == "" || s.encryptionClient == nil {
		return errKitGatewayCredential
	}
	tx, queries, err := s.beginKitRuntimeTransaction(ctx, projectID, assistantID, runtimeID)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	plaintext, err := json.Marshal(kitGatewayTokenEnvelope{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID, Token: token})
	if err != nil {
		return errKitGatewayCredential
	}
	encrypted, err := s.encryptionClient.Encrypt(plaintext)
	if err != nil {
		return errKitGatewayCredential
	}
	count, err := queries.StoreKitGatewayCredential(ctx, assistantrepo.StoreKitGatewayCredentialParams{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID, TokenCiphertext: encrypted})
	if err != nil {
		return errKitGatewayCredential
	}
	if count != 1 {
		return ErrKitRuntimeBinding
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Kit gateway credential: %w", err)
	}
	return nil
}

// LoadKitGatewayToken is trusted internal access. Validate and lock the current
// runtime authority before reading ciphertext or attempting decryption. A
// stopped process may reconnect while its runtime row remains live; an ended,
// deleted, superseded, cross-project, or non-Kit authority cannot decrypt it.
func (s *ServiceCore) LoadKitGatewayToken(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID) (string, error) {
	tx, queries, err := s.beginKitRuntimeTransaction(ctx, projectID, assistantID, runtimeID)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ciphertext, err := queries.LoadKitGatewayCredential(ctx, assistantrepo.LoadKitGatewayCredentialParams{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKitGatewayTokenNotFound
	}
	if err != nil || s.encryptionClient == nil {
		return "", errKitGatewayCredential
	}
	plaintext, err := s.encryptionClient.Decrypt(ciphertext)
	if err != nil {
		return "", errKitGatewayCredential
	}
	var envelope kitGatewayTokenEnvelope
	if err := json.Unmarshal([]byte(plaintext), &envelope); err != nil {
		return "", errKitGatewayCredential
	}
	if envelope.ProjectID != projectID || envelope.AssistantID != assistantID || envelope.RuntimeID != runtimeID || envelope.Token == "" {
		return "", errKitGatewayCredential
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit Kit gateway credential read: %w", err)
	}
	return envelope.Token, nil
}

// EnsureKitGatewayToken is the provisioning path: concurrent startup attempts
// converge on the stored winner rather than rotating each other's gateway
// credentials. Persist and use its returned token before launching the gateway.
// StoreKitGatewayToken remains the explicit rotation operation.
func (s *ServiceCore) EnsureKitGatewayToken(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID, candidate string) (string, error) {
	tx, queries, err := s.beginKitRuntimeTransaction(ctx, projectID, assistantID, runtimeID)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if s.encryptionClient == nil {
		return "", errKitGatewayCredential
	}
	ciphertext, err := queries.LoadKitGatewayCredential(ctx, assistantrepo.LoadKitGatewayCredentialParams{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID})
	if errors.Is(err, pgx.ErrNoRows) {
		if candidate == "" {
			return "", errKitGatewayCredential
		}
		plaintext, encodeErr := json.Marshal(kitGatewayTokenEnvelope{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID, Token: candidate})
		if encodeErr != nil {
			return "", errKitGatewayCredential
		}
		ciphertext, err = s.encryptionClient.Encrypt(plaintext)
		if err != nil {
			return "", errKitGatewayCredential
		}
		count, storeErr := queries.StoreKitGatewayCredential(ctx, assistantrepo.StoreKitGatewayCredentialParams{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID, TokenCiphertext: ciphertext})
		if storeErr != nil {
			return "", errKitGatewayCredential
		}
		if count != 1 {
			return "", ErrKitRuntimeBinding
		}
	} else if err != nil {
		return "", errKitGatewayCredential
	}
	plaintext, err := s.encryptionClient.Decrypt(ciphertext)
	if err != nil {
		return "", errKitGatewayCredential
	}
	var envelope kitGatewayTokenEnvelope
	if err := json.Unmarshal([]byte(plaintext), &envelope); err != nil {
		return "", errKitGatewayCredential
	}
	if envelope.ProjectID != projectID || envelope.AssistantID != assistantID || envelope.RuntimeID != runtimeID || envelope.Token == "" {
		return "", errKitGatewayCredential
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit Kit gateway provisioning credential: %w", err)
	}
	return envelope.Token, nil
}
