package anthropicinference

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
)

// Bump this when enforcement input extraction or checkpoint semantics change.
const checkpointVersion = 2

var errCheckpointConflict = errors.New("inference checkpoint changed during evaluation")

type acceptedCheckpoint struct {
	// A fresh token makes every acceptance observable, even for identical frames.
	ID             string   `json:"id"`
	UserID         string   `json:"user_id"`
	Version        int      `json:"version"`
	PolicyRevision string   `json:"policy_revision"`
	Hashes         [][]byte `json:"hashes"`
}

type checkpointSession interface {
	Load(context.Context) ([][]byte, error)
	Accept(context.Context, [][]byte) error
}

type postgresCheckpoint struct {
	userID   string
	db       *pgxpool.Pool
	expected []byte
	config   Config
	chatID   uuid.UUID
	revision string
}

// Begin creates request-local checkpoint state; it acquires no connection or lock.
// The marker lives on the conversation the delivery was bound to, which is the
// owning lane's chat when this transcript is archived elsewhere.
func (s *postgresStore) Begin(_ context.Context, config Config, binding conversation, userID string) (checkpointSession, error) {
	return &postgresCheckpoint{userID: userID, db: s.db, expected: nil, config: config, chatID: binding.chatID, revision: ""}, nil
}

func (s *postgresCheckpoint) Load(ctx context.Context) ([][]byte, error) {
	q := chatrepo.New(s.db)
	revision, err := s.enforcementRevision(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("read inference policy revision: %w", err)
	}
	s.revision = revision
	data, err := q.GetInferenceAcceptedCheckpoint(ctx, chatrepo.GetInferenceAcceptedCheckpointParams{
		ProjectID: s.config.ProjectID, ChatID: s.chatID,
	})
	s.expected = data
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read accepted checkpoint: %w", err)
	}
	var checkpoint acceptedCheckpoint
	if len(data) == 0 {
		return nil, nil
	}
	// Unknown or damaged checkpoints are not proof of acceptance.
	if json.Unmarshal(data, &checkpoint) != nil || checkpoint.Version != checkpointVersion || checkpoint.PolicyRevision != revision || checkpoint.UserID != s.userID {
		return nil, nil
	}
	return checkpoint.Hashes, nil
}

func (s *postgresCheckpoint) Accept(ctx context.Context, hashes [][]byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checkpoint context: %w", err)
	}
	// Hold a connection only for the final revision check and CAS, never across
	// archival or scanning. A failed CAS rolls back without replacing the winning marker.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin checkpoint acceptance: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	q := chatrepo.New(tx)
	revision, err := s.enforcementRevision(ctx, tx)
	if err != nil {
		return fmt.Errorf("recheck inference policy revision: %w", err)
	}
	if revision != s.revision {
		return errors.New("inference enforcement context changed during evaluation")
	}
	data, err := json.Marshal(acceptedCheckpoint{ID: uuid.NewString(), UserID: s.userID, Version: checkpointVersion, PolicyRevision: revision, Hashes: hashes})
	if err != nil {
		return fmt.Errorf("encode accepted checkpoint: %w", err)
	}
	// Empty frames have no conversation row and no content to accept.
	if len(hashes) != 0 {
		count, err := q.SetInferenceAcceptedCheckpoint(ctx, chatrepo.SetInferenceAcceptedCheckpointParams{
			ProjectID: s.config.ProjectID, ChatID: s.chatID, Checkpoint: data, ExpectedCheckpoint: s.expected,
		})
		if err != nil {
			return fmt.Errorf("persist accepted checkpoint: %w", err)
		}
		if count != 1 {
			return errCheckpointConflict
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checkpoint commit context: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit accepted checkpoint: %w", err)
	}
	return nil
}

// Resolve the same principals/grants as the scanner, including targeted
// audiences and bypass grants. Sorting removes query-order instability.
func (s *postgresCheckpoint) enforcementRevision(ctx context.Context, db chatrepo.DBTX) (string, error) {
	revision, err := chatrepo.New(db).InferencePolicyRevision(ctx, s.config.ProjectID)
	if err != nil {
		return "", fmt.Errorf("load enforcement configuration: %w", err)
	}
	principals, err := authz.ResolveUserPrincipals(ctx, db, s.config.OrganizationID, s.userID)
	if err != nil {
		return "", fmt.Errorf("resolve enforcement principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, db, s.config.OrganizationID, principals)
	if err != nil {
		return "", fmt.Errorf("load enforcement grants: %w", err)
	}
	encoded := make([]string, 0, len(grants))
	for _, grant := range grants {
		data, err := json.Marshal(map[string]any{
			"principal": grant.PrincipalUrn, "scope": grant.Scope, "selector": grant.Selector,
		})
		if err != nil {
			return "", fmt.Errorf("encode enforcement grant: %w", err)
		}
		encoded = append(encoded, string(data))
	}
	sort.Strings(encoded)
	data, err := json.Marshal(encoded)
	if err != nil {
		return "", fmt.Errorf("encode enforcement grants: %w", err)
	}
	hash := sha256.Sum256(append([]byte(revision), data...))
	return hex.EncodeToString(hash[:]), nil
}

func transcriptHashes(messages []Message) [][]byte {
	hashes := make([][]byte, len(messages))
	for i, msg := range messages {
		hashes[i] = contentHash(msg)
	}
	return hashes
}

// acceptedPrefix never jumps over uncertain content to a later matching
// anchor. A retained tail can start at a unique position in the accepted
// frame. Repeated/ambiguous anchors and rewritten summaries rescan instead.
// Keeping the entire accepted frame avoids bounded-window repetition bugs.
func acceptedPrefix(accepted, frame [][]byte) int {
	if len(accepted) == 0 || len(frame) == 0 {
		return 0
	}
	start := 0
	if !bytes.Equal(accepted[0], frame[0]) {
		start = -1
		for i, hash := range accepted {
			if bytes.Equal(hash, frame[0]) {
				if start != -1 {
					return 0
				}
				start = i
			}
		}
		if start == -1 {
			return 0
		}
	}
	matched := 0
	for matched < len(frame) && start+matched < len(accepted) && bytes.Equal(frame[matched], accepted[start+matched]) {
		matched++
	}
	return matched
}
