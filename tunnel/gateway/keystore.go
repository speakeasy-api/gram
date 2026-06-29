package gateway

import (
	"context"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

// KeyResolver resolves a presented tunnel API key to its tunnel ID.
type KeyResolver interface {
	Resolve(ctx context.Context, bearer string) (string, bool, error)
}

type ConnectionRecorder interface {
	MarkConnected(ctx context.Context, tunnelID, keyHash, agentVersion string) error
}

type ActiveTunnelChecker interface {
	IsActive(ctx context.Context, tunnelID, keyHash string) (bool, error)
}

// StaticKeyStore is a process-local key resolver for tests and single-process
// local harnesses. Production gateways use PostgresKeyResolver.
type StaticKeyStore struct {
	mu      sync.RWMutex
	byHash  map[string]string // keyHash -> tunnelID
	revoked map[string]bool   // tunnelID -> revoked
}

// NewStaticKeyStore builds a resolver from a tunnelID -> plaintext-key map.
func NewStaticKeyStore(seed map[string]string) *StaticKeyStore {
	ks := &StaticKeyStore{byHash: make(map[string]string), revoked: make(map[string]bool)}
	for tunnelID, plaintext := range seed {
		ks.byHash[wire.HashKey(plaintext)] = tunnelID
	}
	return ks
}

// Add registers an additional tunnelID/plaintext pair for tests that need to
// mutate a static resolver. Returns the stored hash.
func (k *StaticKeyStore) Add(tunnelID, plaintext string) string {
	h := wire.HashKey(plaintext)
	k.mu.Lock()
	k.byHash[h] = tunnelID
	delete(k.revoked, tunnelID)
	k.mu.Unlock()
	return h
}

// Revoke marks a tunnel revoked; subsequent Resolve calls reject it.
func (k *StaticKeyStore) Revoke(tunnelID string) {
	k.mu.Lock()
	k.revoked[tunnelID] = true
	k.mu.Unlock()
}

// Resolve validates a bearer value and returns the bound tunnel ID. The cheap
// prefix check happens before the map hit.
func (k *StaticKeyStore) Resolve(_ context.Context, bearer string) (string, bool, error) {
	key := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	if !wire.HasKeyPrefix(key) {
		return "", false, nil
	}
	h := wire.HashKey(key)
	k.mu.RLock()
	defer k.mu.RUnlock()
	tunnelID, ok := k.byHash[h]
	if !ok || k.revoked[tunnelID] {
		return "", false, nil
	}
	return tunnelID, true, nil
}

var _ KeyResolver = (*StaticKeyStore)(nil)
