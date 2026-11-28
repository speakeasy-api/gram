package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

var (
	ErrKitRuntimeBinding         = errors.New("kit runtime authority or session binding is unavailable")
	ErrKitRuntimeSessionConflict = errors.New("kit session is already bound to another thread")
	ErrKitGatewayTokenNotFound   = errors.New("kit ACP root credential is not stored")
	errKitGatewayCredential      = errors.New("kit ACP root credential is unavailable")
	errKitRuntimeUnrecoverable   = errors.New("kit runtime cannot recover")
	errKitRetiredBackend         = errors.New("kit runtime references a retired backend")
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
	if _, err := queries.GetLiveKitRuntimeAuthority(ctx, assistantrepo.GetLiveKitRuntimeAuthorityParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return kitRuntimeSession{}, ErrKitRuntimeBinding
		}
		return kitRuntimeSession{}, fmt.Errorf("validate Kit callback authority: %w", err)
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
	if _, err := queries.GetLiveKitRuntimeAuthority(ctx, assistantrepo.GetLiveKitRuntimeAuthorityParams{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtimeID}); err != nil {
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
