package llmanalyzer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultVerdictCacheTTL is how long a cached verdict stays valid. It is
	// long enough to collapse the per-policy fan-out of one batch run and a
	// sync scan followed by its async re-scan of the same content, and short
	// enough that a retrained model served under the same name is picked up
	// within minutes.
	DefaultVerdictCacheTTL = 10 * time.Minute

	// verdictCacheKeyPrefix namespaces verdict entries in the shared Redis.
	verdictCacheKeyPrefix = "risk:llm:verdict:"

	// CacheResultHit, CacheResultMiss and CacheResultError are the
	// gram.risk.llm.cache_result values recorded on risk.llm.cache. A lookup
	// that errored or returned an unparsable entry is counted as an error and
	// then handled as a miss.
	CacheResultHit   = "hit"
	CacheResultMiss  = "miss"
	CacheResultError = "error"
)

// CachedVerdict is the model reply stored per prompt. Raw is the verbatim
// completion text, so ParseVerdict reproduces the verdict on a hit without
// the cache having to know the verdict schema.
type CachedVerdict struct {
	// Raw is the completion content exactly as the model returned it.
	Raw string `json:"raw"`

	// Model is the served model name that produced Raw.
	Model string `json:"model"`

	// PromptTokens is the prompt token count the upstream reported for the
	// original call. Informational: hits report zero tokens on the Analysis.
	PromptTokens int `json:"prompt_tokens"`

	// CompletionTokens is the completion token count the upstream reported
	// for the original call. Informational, like PromptTokens.
	CompletionTokens int `json:"completion_tokens"`
}

// VerdictCache stores model replies keyed by VerdictCacheKey. Implementations
// must be safe for concurrent use. Errors from either method are surfaced to
// the analyzer, which treats them as misses and never fails an analysis on
// them.
type VerdictCache interface {
	// Get returns the verdict stored under key and whether one was found.
	Get(ctx context.Context, key string) (CachedVerdict, bool, error)

	// Set stores v under key, replacing any existing entry.
	Set(ctx context.Context, key string, v CachedVerdict) error
}

// VerdictCacheKey derives the cache key of one model call. The served model
// name is part of the hash so that a redeploy under a new name never serves
// verdicts produced by its predecessor. The prompts are hashed rather than
// stored, so message content never lands in Redis key space.
func VerdictCacheKey(model, systemPrompt, userPrompt string) string {
	h := sha256.New()
	h.Write([]byte(model))
	h.Write([]byte{0})
	h.Write([]byte(systemPrompt))
	h.Write([]byte{0})
	h.Write([]byte(userPrompt))
	return verdictCacheKeyPrefix + hex.EncodeToString(h.Sum(nil))
}

// RedisVerdictCache is the production VerdictCache: one JSON string per key
// with a fixed expiry. Writes are plain SET with EX, so concurrent analyses
// of the same prompt race benignly and the last writer wins.
type RedisVerdictCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

var _ VerdictCache = (*RedisVerdictCache)(nil)

// NewRedisVerdictCache builds a cache over client whose entries expire after
// ttl. A non-positive ttl falls back to DefaultVerdictCacheTTL.
func NewRedisVerdictCache(client redis.Cmdable, ttl time.Duration) *RedisVerdictCache {
	if ttl <= 0 {
		ttl = DefaultVerdictCacheTTL
	}
	return &RedisVerdictCache{client: client, ttl: ttl}
}

// Get reads the entry under key. A missing key is a miss, not an error.
func (c *RedisVerdictCache) Get(ctx context.Context, key string) (CachedVerdict, bool, error) {
	var v CachedVerdict
	payload, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return v, false, nil
	}
	if err != nil {
		return v, false, fmt.Errorf("get risk llm verdict: %w", err)
	}
	if err := json.Unmarshal(payload, &v); err != nil {
		return CachedVerdict{Raw: "", Model: "", PromptTokens: 0, CompletionTokens: 0}, false, fmt.Errorf("decode risk llm verdict: %w", err)
	}
	return v, true, nil
}

// Set stores v under key with the cache's expiry.
func (c *RedisVerdictCache) Set(ctx context.Context, key string, v CachedVerdict) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode risk llm verdict: %w", err)
	}
	if err := c.client.Set(ctx, key, payload, c.ttl).Err(); err != nil {
		return fmt.Errorf("set risk llm verdict: %w", err)
	}
	return nil
}

// NoopVerdictCache never stores anything: every lookup misses. It is the
// analyzer's default so callers without Redis need no cache wiring.
type NoopVerdictCache struct{}

var _ VerdictCache = NoopVerdictCache{}

// Get always reports a miss.
func (NoopVerdictCache) Get(context.Context, string) (CachedVerdict, bool, error) {
	return CachedVerdict{Raw: "", Model: "", PromptTokens: 0, CompletionTokens: 0}, false, nil
}

// Set discards v.
func (NoopVerdictCache) Set(context.Context, string, CachedVerdict) error {
	return nil
}

// MemoryVerdictCache is a process-local VerdictCache without expiry, for
// tests and local development. The zero value is not usable; construct it
// with NewMemoryVerdictCache.
type MemoryVerdictCache struct {
	mu      sync.Mutex
	entries map[string]CachedVerdict
}

var _ VerdictCache = (*MemoryVerdictCache)(nil)

// NewMemoryVerdictCache returns an empty in-memory cache.
func NewMemoryVerdictCache() *MemoryVerdictCache {
	return &MemoryVerdictCache{mu: sync.Mutex{}, entries: make(map[string]CachedVerdict)}
}

// Get returns the entry under key, if any.
func (c *MemoryVerdictCache) Get(_ context.Context, key string) (CachedVerdict, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok, nil
}

// Set stores v under key.
func (c *MemoryVerdictCache) Set(_ context.Context, key string, v CachedVerdict) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = v
	return nil
}

// Len reports how many entries the cache holds.
func (c *MemoryVerdictCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
