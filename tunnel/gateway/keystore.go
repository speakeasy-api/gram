package gateway

import (
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

// KeyStore resolves a presented tunnel API key to its tunnel ID. In prod this
// is a project-scoped lookup against the `tunnels` table (key_hash column); the
// POC seeds an in-memory map (keyHash -> tunnelID) from config/env, so there is
// no DB dependency. Org/project binding still comes from the stored row, never
// from the token itself.
type KeyStore struct {
	mu      sync.RWMutex
	byHash  map[string]string // keyHash -> tunnelID
	revoked map[string]bool   // tunnelID -> revoked
}

// NewKeyStore builds a store from a tunnelID -> plaintext-key map (POC seeding).
func NewKeyStore(seed map[string]string) *KeyStore {
	ks := &KeyStore{byHash: make(map[string]string), revoked: make(map[string]bool)}
	for tunnelID, plaintext := range seed {
		ks.byHash[wire.HashKey(plaintext)] = tunnelID
	}
	return ks
}

// Add registers an additional tunnelID/plaintext pair at runtime (e.g. from a
// create endpoint). Returns the stored hash.
func (k *KeyStore) Add(tunnelID, plaintext string) string {
	h := wire.HashKey(plaintext)
	k.mu.Lock()
	k.byHash[h] = tunnelID
	delete(k.revoked, tunnelID)
	k.mu.Unlock()
	return h
}

// Revoke marks a tunnel revoked; subsequent Resolve calls reject it.
func (k *KeyStore) Revoke(tunnelID string) {
	k.mu.Lock()
	k.revoked[tunnelID] = true
	k.mu.Unlock()
}

// Resolve validates a bearer value and returns the bound tunnel ID. The cheap
// prefix check happens before the map hit.
func (k *KeyStore) Resolve(bearer string) (string, bool) {
	key := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
	if !wire.HasKeyPrefix(key) {
		return "", false
	}
	h := wire.HashKey(key)
	k.mu.RLock()
	defer k.mu.RUnlock()
	tunnelID, ok := k.byHash[h]
	if !ok || k.revoked[tunnelID] {
		return "", false
	}
	return tunnelID, true
}
