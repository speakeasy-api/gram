package gateway

import (
	"context"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

type KeyResolver interface {
	Resolve(ctx context.Context, bearer string) (string, bool, error)
}

type ConnectionRecorder interface {
	MarkConnected(ctx context.Context, tunnelID, keyHash, agentVersion string) error
}

type ActiveTunnelChecker interface {
	IsActive(ctx context.Context, tunnelID, keyHash string) (bool, error)
}

// StaticKeyStore is test/local-only; production uses PostgresKeyResolver.
type StaticKeyStore struct {
	mu      sync.RWMutex
	byHash  map[string]string // keyHash -> tunnelID
	revoked map[string]bool   // tunnelID -> revoked
}

func NewStaticKeyStore(seed map[string]string) *StaticKeyStore {
	ks := &StaticKeyStore{byHash: make(map[string]string), revoked: make(map[string]bool)}
	for tunnelID, plaintext := range seed {
		ks.byHash[wire.HashKey(plaintext)] = tunnelID
	}
	return ks
}

func (k *StaticKeyStore) Add(tunnelID, plaintext string) string {
	h := wire.HashKey(plaintext)
	k.mu.Lock()
	k.byHash[h] = tunnelID
	delete(k.revoked, tunnelID)
	k.mu.Unlock()
	return h
}

func (k *StaticKeyStore) Revoke(tunnelID string) {
	k.mu.Lock()
	k.revoked[tunnelID] = true
	k.mu.Unlock()
}

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
