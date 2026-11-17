package tailnetgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tailscale.com/ipn"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/tailnetgateway/repo"
)

const (
	// stateStoreTimeout bounds each state read or write. tsnet calls the store
	// without a context, so a hung query must not stall the node forever.
	stateStoreTimeout = 10 * time.Second

	// acmeAccountKeyStateKey is the key tsnet stores its ACME account key
	// under when certificates live in the state store.
	acmeAccountKeyStateKey = "acme-account.key.pem"
)

// sharedAcrossSlots reports whether a state key belongs to the whole
// connection. Every slot serves the same Tailscale Service name, so they
// share one TLS certificate and key (stored as "<domain>.crt" and
// "<domain>.key") and the ACME account that issues and renews it. Without
// this each slot requested its own certificate for the same name, spending
// the tailnet's Let's Encrypt allowance once per slot, and again on every
// renewal.
func sharedAcrossSlots(id ipn.StateKey) bool {
	key := string(id)
	return key == acmeAccountKeyStateKey || strings.HasSuffix(key, ".crt") || strings.HasSuffix(key, ".key")
}

// PostgresStateStore persists one node's tsnet state in Postgres, encrypted
// under the organization's key. It implements ipn.StateStore for a single
// (ingress, slot) pair; the certificate state the connection's slots share
// is kept once for the connection in network_ingress_certificate_states.
type PostgresStateStore struct {
	db        *pgxpool.Pool
	enc       *orgencryption.Encryptor
	ingressID uuid.UUID
	orgID     string
	slot      int32
}

var (
	_ ipn.StateStore = (*PostgresStateStore)(nil)

	// tsnet stores certificates through these when the store offers them, so
	// a certificate and its key are always read and written as a pair.
	_ interface {
		ReadTLSCertAndKey(domain string) ([]byte, []byte, error)
		WriteTLSCertAndKey(domain string, cert, key []byte) error
	} = (*PostgresStateStore)(nil)
)

// NewPostgresStateStore builds the store for one node identity.
func NewPostgresStateStore(db *pgxpool.Pool, enc *orgencryption.Service, ingressID uuid.UUID, orgID string, slot int32) (*PostgresStateStore, error) {
	if db == nil || enc == nil {
		return nil, errors.New("tailnet state store requires a database and organization encryption")
	}
	if ingressID == uuid.Nil || orgID == "" || slot < 0 {
		return nil, errors.New("tailnet state store requires an ingress, organization and slot")
	}
	encryptor, err := enc.For(orgID, EncryptionUseCase)
	if err != nil {
		return nil, fmt.Errorf("tailnet state store encryption: %w", err)
	}
	return &PostgresStateStore{db: db, enc: encryptor, ingressID: ingressID, orgID: orgID, slot: slot}, nil
}

// ReadState returns the decrypted bytes for id, or ipn.ErrStateNotExist.
func (s *PostgresStateStore) ReadState(id ipn.StateKey) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
	defer cancel()

	queries := repo.New(s.db)
	var ciphertext string
	var err error
	if sharedAcrossSlots(id) {
		ciphertext, err = queries.GetNetworkIngressCertificateState(ctx, repo.GetNetworkIngressCertificateStateParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: string(id),
		})
	} else {
		ciphertext, err = queries.GetNetworkIngressNodeState(ctx, repo.GetNetworkIngressNodeStateParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(id),
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ipn.ErrStateNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("read tailnet node state: %w", err)
	}

	value, err := s.enc.Decrypt(ctx, nodeStateColumn, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt tailnet node state: %w", err)
	}
	return value, nil
}

// WriteState encrypts and upserts bs under id. A nil value deletes the key.
// The connection's ACME account key is written once: when slots start
// together and each generates one, the first stored wins and every later
// read returns it, so renewals come from the account that issued the
// certificate.
func (s *PostgresStateStore) WriteState(id ipn.StateKey, bs []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
	defer cancel()

	queries := repo.New(s.db)
	shared := sharedAcrossSlots(id)
	if bs == nil {
		var err error
		if shared {
			err = queries.DeleteNetworkIngressCertificateState(ctx, repo.DeleteNetworkIngressCertificateStateParams{
				NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: string(id),
			})
		} else {
			err = queries.DeleteNetworkIngressNodeState(ctx, repo.DeleteNetworkIngressNodeStateParams{
				NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(id),
			})
		}
		if err != nil {
			return fmt.Errorf("delete tailnet node state: %w", err)
		}
		return nil
	}

	ciphertext, err := s.enc.Encrypt(ctx, nodeStateColumn, bs)
	if err != nil {
		return fmt.Errorf("encrypt tailnet node state: %w", err)
	}
	switch {
	case id == acmeAccountKeyStateKey:
		err = queries.InsertNetworkIngressCertificateStateIfAbsent(ctx, repo.InsertNetworkIngressCertificateStateIfAbsentParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: string(id), ValueEncrypted: ciphertext,
		})
	case shared:
		err = queries.UpsertNetworkIngressCertificateState(ctx, repo.UpsertNetworkIngressCertificateStateParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: string(id), ValueEncrypted: ciphertext,
		})
	default:
		err = queries.UpsertNetworkIngressNodeState(ctx, repo.UpsertNetworkIngressNodeStateParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, Slot: s.slot, StateKey: string(id), ValueEncrypted: ciphertext,
		})
	}
	if err != nil {
		return fmt.Errorf("write tailnet node state: %w", err)
	}
	return nil
}

// ReadTLSCertAndKey returns the connection's certificate and key for domain
// from one statement, or ipn.ErrStateNotExist when either is missing.
// Reading them separately could pair a key with the certificate another slot
// replaced in between, which tsnet would treat as invalid and re-issue.
func (s *PostgresStateStore) ReadTLSCertAndKey(domain string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
	defer cancel()

	certKey, keyKey := domain+".crt", domain+".key"
	rows, err := repo.New(s.db).ListNetworkIngressCertificateStates(ctx, repo.ListNetworkIngressCertificateStatesParams{
		NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKeys: []string{certKey, keyKey},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read tailnet TLS certificate: %w", err)
	}

	var cert, key []byte
	for _, row := range rows {
		value, err := s.enc.Decrypt(ctx, nodeStateColumn, row.ValueEncrypted)
		if err != nil {
			return nil, nil, fmt.Errorf("decrypt tailnet TLS certificate: %w", err)
		}
		switch row.StateKey {
		case certKey:
			cert = value
		case keyKey:
			key = value
		}
	}
	if cert == nil || key == nil {
		return nil, nil, ipn.ErrStateNotExist
	}
	return cert, key, nil
}

// WriteTLSCertAndKey stores the connection's certificate and key for domain
// in one transaction, so every slot sees either the old pair or the new one.
func (s *PostgresStateStore) WriteTLSCertAndKey(domain string, cert, key []byte) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
	defer cancel()

	certCiphertext, err := s.enc.Encrypt(ctx, nodeStateColumn, cert)
	if err != nil {
		return fmt.Errorf("encrypt tailnet TLS certificate: %w", err)
	}
	keyCiphertext, err := s.enc.Encrypt(ctx, nodeStateColumn, key)
	if err != nil {
		return fmt.Errorf("encrypt tailnet TLS key: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tailnet TLS certificate write: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	// A fixed order keeps two slots writing at once from taking the two row
	// locks in opposite orders and deadlocking.
	queries := repo.New(tx)
	for _, entry := range []struct{ stateKey, ciphertext string }{
		{stateKey: domain + ".crt", ciphertext: certCiphertext},
		{stateKey: domain + ".key", ciphertext: keyCiphertext},
	} {
		if err := queries.UpsertNetworkIngressCertificateState(ctx, repo.UpsertNetworkIngressCertificateStateParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: entry.stateKey, ValueEncrypted: entry.ciphertext,
		}); err != nil {
			return fmt.Errorf("write tailnet TLS certificate: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tailnet TLS certificate: %w", err)
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
	raw, err := s.enc.Decrypt(ctx, nodeStateColumn, ciphertext)
	if err != nil {
		return false, fmt.Errorf("decrypt tailnet login profiles: %w", err)
	}
	return knownProfilesPresent(raw)
}

// knownProfilesPresent reports whether the persisted known-profiles document
// names at least one login. tailscaled stores it as a map keyed by profile
// id; older builds wrote a list, so both shapes are accepted.
func knownProfilesPresent(raw []byte) (bool, error) {
	var byID map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byID); err == nil {
		return len(byID) > 0, nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return false, fmt.Errorf("parse tailnet login profiles: %w", err)
	}
	return len(list) > 0, nil
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

const (
	// certificateLeaseStateKey stores the lease a slot holds while it
	// requests the connection's certificate.
	certificateLeaseStateKey = "issuance-lease"

	// certificateLeaseDuration is how long a lease lasts if its holder never
	// releases it, as when its pod dies mid-request. It outlasts
	// certificateFetchTimeout plus the release's stateStoreTimeout, so a live
	// request never loses its lease and a release never deletes another
	// slot's; TestCertificateLeaseOutlastsALiveRequest holds that.
	certificateLeaseDuration = 3 * time.Minute
)

// ClaimCertificateIssuance claims the connection's certificate lease, so its
// slots request the certificate one at a time: tsnet reads the stored
// certificate before asking Let's Encrypt, so a slot that found the lease
// taken and retried later finds the certificate the holder stored instead of
// issuing another. Claiming holds no database connection. It reports false
// when another slot holds a live lease; otherwise release ends the lease.
func (s *PostgresStateStore) ClaimCertificateIssuance(ctx context.Context) (func(), bool, error) {
	// The marker identifies this claim, so release deletes only its own.
	marker, err := s.enc.Encrypt(ctx, nodeStateColumn, []byte("issuing:"+uuid.NewString()))
	if err != nil {
		return nil, false, fmt.Errorf("encrypt tailnet certificate lease: %w", err)
	}
	claimed, err := repo.New(s.db).ClaimNetworkIngressCertificateIssuance(ctx, repo.ClaimNetworkIngressCertificateIssuanceParams{
		NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: certificateLeaseStateKey,
		ValueEncrypted: marker, LeaseSeconds: certificateLeaseDuration.Seconds(),
	})
	if err != nil {
		return nil, false, fmt.Errorf("claim tailnet certificate lease: %w", err)
	}
	if claimed == 0 {
		return nil, false, nil
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), stateStoreTimeout)
		defer cancel()
		// A lease left behind lapses after certificateLeaseDuration.
		_ = repo.New(s.db).ReleaseNetworkIngressCertificateIssuance(releaseCtx, repo.ReleaseNetworkIngressCertificateIssuanceParams{
			NetworkIngressID: s.ingressID, OrganizationID: s.orgID, StateKey: certificateLeaseStateKey, ValueEncrypted: marker,
		})
	}, true, nil
}
