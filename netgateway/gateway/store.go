package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore reads ingress configuration from network_ingresses and owns
// the network_node_state KV. It follows the tunnel gateway's pattern of
// hand-written queries: the gateway is a separate deployment with its own
// database access and does not share gram-server's sqlc repos.
type PostgresStore struct {
	db  *pgxpool.Pool
	enc *Decryptor
}

func NewPostgresStore(db *pgxpool.Pool, enc *Decryptor) *PostgresStore {
	return &PostgresStore{db: db, enc: enc}
}

func (s *PostgresStore) Close() {
	if s != nil && s.db != nil {
		s.db.Close()
	}
}

// ListEnabledIngresses returns every enabled, non-deleted ingress with its
// credential decrypted.
func (s *PostgresStore) ListEnabledIngresses(ctx context.Context) ([]IngressConfig, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, organization_id, provider, hostname, tags, identity_required,
       credential_kind, COALESCE(auth_key_enc, ''), COALESCE(oauth_client_id, ''),
       COALESCE(oauth_client_secret_enc, ''), updated_at
FROM network_ingresses
WHERE enabled IS TRUE
  AND deleted IS FALSE
ORDER BY id
`)
	if err != nil {
		return nil, fmt.Errorf("list enabled network ingresses: %w", err)
	}
	defer rows.Close()

	var out []IngressConfig
	for rows.Next() {
		var (
			cfg                   IngressConfig
			authKeyEnc, secretEnc string
		)
		if err := rows.Scan(
			&cfg.ID, &cfg.OrganizationID, &cfg.Provider, &cfg.Hostname, &cfg.Tags,
			&cfg.IdentityRequired, &cfg.Credential.Kind, &authKeyEnc,
			&cfg.Credential.OAuthClientID, &secretEnc, &cfg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan network ingress row: %w", err)
		}

		if authKeyEnc != "" {
			key, err := s.enc.Decrypt(authKeyEnc)
			if err != nil {
				return nil, fmt.Errorf("decrypt auth key for ingress %s: %w", cfg.ID, err)
			}
			cfg.Credential.AuthKey = key
		}
		if secretEnc != "" {
			secret, err := s.enc.Decrypt(secretEnc)
			if err != nil {
				return nil, fmt.Errorf("decrypt oauth client secret for ingress %s: %w", cfg.ID, err)
			}
			cfg.Credential.OAuthClientSecret = secret
		}

		out = append(out, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate network ingress rows: %w", err)
	}
	return out, nil
}

// SetStatus persists a node's observed health onto its ingress row.
// connected_since is set on the transition to online and cleared when the
// node goes offline, so the dashboard can show connection age.
func (s *PostgresStore) SetStatus(ctx context.Context, ingressID uuid.UUID, status string, ns NodeStatus) error {
	_, err := s.db.Exec(ctx, `
UPDATE network_ingresses
SET status = $2,
    network_name = NULLIF($3, ''),
    dns_name = NULLIF($4, ''),
    node_id = NULLIF($5, ''),
    last_error = NULLIF($6, ''),
    last_seen_at = clock_timestamp(),
    connected_since = CASE
      WHEN $2 = 'online' AND status <> 'online' THEN clock_timestamp()
      WHEN $2 = 'online' THEN connected_since
      ELSE NULL
    END,
    updated_at = updated_at
WHERE id = $1
  AND deleted IS FALSE
`, ingressID, status, ns.NetworkName, ns.DNSName, ns.NodeID, ns.Err)
	if err != nil {
		return fmt.Errorf("set network ingress status: %w", err)
	}
	return nil
}

// NullAuthKey clears a consumed one-shot join key. Called after the first
// successful auth-key join; the node's durable identity now lives in the
// state store and the key is spent.
func (s *PostgresStore) NullAuthKey(ctx context.Context, ingressID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
UPDATE network_ingresses
SET auth_key_enc = NULL,
    updated_at = updated_at
WHERE id = $1
  AND deleted IS FALSE
`, ingressID)
	if err != nil {
		return fmt.Errorf("null consumed auth key: %w", err)
	}
	return nil
}

// NodeState returns the per-ingress durable KV store backing the provider's
// node identity.
func (s *PostgresStore) NodeState(ingressID uuid.UUID) StateStore {
	return &pgNodeState{db: s.db, ingressID: ingressID}
}

type pgNodeState struct {
	db        *pgxpool.Pool
	ingressID uuid.UUID
}

func (s *pgNodeState) Get(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRow(ctx, `
SELECT value FROM network_node_state WHERE ingress_id = $1 AND key = $2
`, s.ingressID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read node state: %w", err)
	}
	return value, nil
}

func (s *pgNodeState) Set(ctx context.Context, key string, value []byte) error {
	_, err := s.db.Exec(ctx, `
INSERT INTO network_node_state (ingress_id, key, value, updated_at)
VALUES ($1, $2, $3, clock_timestamp())
ON CONFLICT (ingress_id, key)
DO UPDATE SET value = EXCLUDED.value, updated_at = clock_timestamp()
`, s.ingressID, key, value)
	if err != nil {
		return fmt.Errorf("write node state: %w", err)
	}
	return nil
}
