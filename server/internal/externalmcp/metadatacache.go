package externalmcp

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
)

// Only metadata is shared, never clients, sessions, arguments, or credentials.
// The per-schema bound also bounds retained schema memory to 64 MiB.
const maxCachedToolSchemaBytes = 64 << 10

var sharedToolMetadata = newToolMetadataCache(1024, 5*time.Minute)

// metadataScope hashes the complete upstream/effective-auth/config identity.
// JSON encoding is unambiguous and sorts map keys, so header insertion order
// does not change the identity. Keeping exact header spellings conservatively
// isolates configurations with case-insensitive duplicate header names.
// Callers must supply a project-scoped identity to opt into shared metadata.
func metadataScope(remoteURL string, transportType types.TransportType, opts *ClientOptions) string {
	if opts == nil || opts.MetadataScope == "" {
		return ""
	}

	identity, err := json.Marshal(struct {
		Scope         string              `json:"scope"`
		RemoteURL     string              `json:"remote_url"`
		Transport     types.TransportType `json:"transport"`
		Authorization string              `json:"authorization"`
		Headers       map[string]string   `json:"headers"`
	}{
		Scope:         opts.MetadataScope,
		RemoteURL:     remoteURL,
		Transport:     transportType,
		Authorization: opts.Authorization,
		Headers:       opts.Headers,
	})
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(identity)
	return hex.EncodeToString(hash[:])
}

func cachedToolSchema(scope string, toolName string) (json.RawMessage, bool) {
	return sharedToolMetadata.get(scope, toolName)
}

func cacheToolSchema(scope, toolName string, schema json.RawMessage) {
	sharedToolMetadata.put(scope, toolName, schema)
}

// Hash both components to keep keys bounded even for untrusted tool names.
// Hashing separately avoids delimiter ambiguity.
type toolMetadataKey struct {
	scope    [sha256.Size]byte
	toolName [sha256.Size]byte
}

type toolMetadataEntry struct {
	key       toolMetadataKey
	schema    json.RawMessage
	expiresAt time.Time
}

type toolMetadataCache struct {
	mu       sync.Mutex
	entries  map[toolMetadataKey]*list.Element
	lru      *list.List
	capacity int
	ttl      time.Duration
	now      func() time.Time
}

func newToolMetadataCache(capacity int, ttl time.Duration) *toolMetadataCache {
	return &toolMetadataCache{
		mu:       sync.Mutex{},
		entries:  make(map[toolMetadataKey]*list.Element),
		lru:      list.New(),
		capacity: capacity,
		ttl:      ttl,
		now:      time.Now,
	}
}

func (c *toolMetadataCache) get(scope, toolName string) (json.RawMessage, bool) {
	if scope == "" {
		return nil, false
	}
	key := toolMetadataKey{scope: sha256.Sum256([]byte(scope)), toolName: sha256.Sum256([]byte(toolName))}
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry, _ := element.Value.(*toolMetadataEntry)
	if !c.now().Before(entry.expiresAt) {
		c.remove(element)
		return nil, false
	}
	c.lru.MoveToFront(element)
	return bytes.Clone(entry.schema), true
}

func (c *toolMetadataCache) put(scope, toolName string, schema json.RawMessage) {
	if scope == "" {
		return
	}
	// Empty, null, malformed, and non-object definitions are misses, but an
	// annotation-free object (including {}) is authoritative metadata.
	var object map[string]json.RawMessage
	valid := len(schema) <= maxCachedToolSchemaBytes && json.Unmarshal(schema, &object) == nil && object != nil
	key := toolMetadataKey{scope: sha256.Sum256([]byte(scope)), toolName: sha256.Sum256([]byte(toolName))}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.remove(old)
	}
	// Invalidate old metadata even when a replacement cannot be retained.
	if !valid || c.capacity <= 0 {
		return
	}
	for len(c.entries) >= c.capacity {
		c.remove(c.lru.Back())
	}
	entry := &toolMetadataEntry{key: key, schema: bytes.Clone(schema), expiresAt: c.now().Add(c.ttl)}
	c.entries[key] = c.lru.PushFront(entry)
}

// remove requires c.mu to be held.
func (c *toolMetadataCache) remove(element *list.Element) {
	entry, _ := element.Value.(*toolMetadataEntry)
	delete(c.entries, entry.key)
	c.lru.Remove(element)
}
