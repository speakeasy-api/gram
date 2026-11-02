package tailnetgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
)

// stateStoreTimeout bounds each state read or write. tsnet calls the store
// without a context, so a hung query must not stall the node forever.
const stateStoreTimeout = 10 * time.Second

// PostgresStateStore persists one node's tsnet state in Postgres, encrypted
// with the application key. It implements ipn.StateStore for a single
// (ingress, slot) pair.
type PostgresStateStore struct {
	db        repo.DBTX
	enc       *encryption.Client
	ingressID uuid.UUID
	orgID     string
	slot      int32
}

var _ ipn.StateStore = (*PostgresStateStore)(nil)

// NewPostgresStateStore builds the store for one node identity.
func NewPostgresStateStore(db repo.DBTX, enc *encryption.Client, ingressID uuid.UUID, orgID string, slot int32) (*PostgresStateStore, error) {
	if db == nil || enc == nil {
		return nil, errors.New("tailnet state store requires a database and an encryption client")
	}
	if ingressID == uuid.Nil || orgID == "" || slot < 0 {
		return nil, errors.New("tailnet state store requires an ingress, organization and slot")
	}
	return &PostgresStateStore{db: db, enc: enc, ingressID: ingressID, orgID: orgID, slot: slot}, nil
}

// ReadState returns the decrypted bytes for id, or ipn.ErrStateNotExist.
func (s *PostgresStateStore) ReadState(id ipn.StateKey) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
	defer cancel()

	ciphertext, err := repo.New(s.db).GetNetworkIngressNodeState(ctx, repo.GetNetworkIngressNodeStateParams{
		NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(id),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ipn.ErrStateNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("read tailnet node state: %w", err)
	}

	value, err := s.enc.Decrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt tailnet node state: %w", err)
	}
	return []byte(value), nil
}

// WriteState encrypts and upserts bs under id. A nil value deletes the key.
func (s *PostgresStateStore) WriteState(id ipn.StateKey, bs []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
	defer cancel()

	queries := repo.New(s.db)
	if bs == nil {
		if err := queries.DeleteNetworkIngressNodeState(ctx, repo.DeleteNetworkIngressNodeStateParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(id),
		}); err != nil {
			return fmt.Errorf("delete tailnet node state: %w", err)
		}
		return nil
	}

	ciphertext, err := s.enc.Encrypt(bs)
	if err != nil {
		return fmt.Errorf("encrypt tailnet node state: %w", err)
	}
	if err := queries.UpsertNetworkIngressNodeState(ctx, repo.UpsertNetworkIngressNodeStateParams{
		NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(id), ValueEncrypted: ciphertext,
	}); err != nil {
		return fmt.Errorf("write tailnet node state: %w", err)
	}
	return nil
}

// Registered reports whether this node completed a tailnet login. tsnet
// writes the machine key before registering, so a failed first login leaves
// state behind; only a persisted login profile proves the node has a device
// identity and no longer needs a one-shot auth key.
func (s *PostgresStateStore) Registered(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, stateStoreTimeout)
	defer cancel()
	ciphertext, err := repo.New(s.db).GetNetworkIngressNodeState(ctx, repo.GetNetworkIngressNodeStateParams{
		NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(ipn.KnownProfilesStateKey),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read tailnet login profiles: %w", err)
	}
	raw, err := s.enc.Decrypt(ciphertext)
	if err != nil {
		return false, fmt.Errorf("decrypt tailnet login profiles: %w", err)
	}
	var profiles []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
		return false, fmt.Errorf("parse tailnet login profiles: %w", err)
	}
	return len(profiles) > 0, nil
}

// Reset deletes every persisted key so the next start registers afresh. It is
// used after a failed first login left partial state behind.
func (s *PostgresStateStore) Reset(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, stateStoreTimeout)
	defer cancel()
	if err := repo.New(s.db).DeleteNetworkIngressNodeSlotStates(ctx, repo.DeleteNetworkIngressNodeSlotStatesParams{
		NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot,
	}); err != nil {
		return fmt.Errorf("reset tailnet node state: %w", err)
	}
	return nil
}
