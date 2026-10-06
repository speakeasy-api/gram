package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

var errCacheMiss = errors.New("cache miss")

// testMemoryCache is a minimal in-memory cache for unit tests. It stores
// values as JSON, like the Redis adapter. testenv.NewMemoryCache cannot be
// used here: testenv imports this package, and these tests inspect the raw
// stored entries.
type testMemoryCache struct {
	mu    sync.Mutex
	items map[string][]byte
}

var _ cache.Cache = (*testMemoryCache)(nil)

func newTestMemoryCache() *testMemoryCache {
	return &testMemoryCache{mu: sync.Mutex{}, items: make(map[string][]byte)}
}

func (c *testMemoryCache) Get(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.items[key]
	if !ok {
		return errCacheMiss
	}
	return unmarshal(raw, value)
}

func (c *testMemoryCache) GetAndDelete(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, ok := c.items[key]
	if !ok {
		return errCacheMiss
	}
	delete(c.items, key)
	return unmarshal(raw, value)
}

func unmarshal(raw []byte, value any) error {
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

func (c *testMemoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = raw
	return nil
}

func (c *testMemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	return nil
}

func (c *testMemoryCache) Add(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("not implemented")
}
func (c *testMemoryCache) Update(context.Context, string, any) error {
	return errors.New("not implemented")
}
func (c *testMemoryCache) Expire(context.Context, string, time.Duration) error { return nil }
func (c *testMemoryCache) ListAppend(context.Context, string, any, time.Duration) error {
	return nil
}
func (c *testMemoryCache) ListRange(context.Context, string, int64, int64, any) error {
	return nil
}
func (c *testMemoryCache) DeleteByPrefix(context.Context, string) error { return nil }

const (
	testSourceHost = "source.example.com"
	testTargetHost = "target.example.com"
)

func testTransferSession() Session {
	return Session{
		SessionID:             "source-session-id",
		UserID:                "test-user-id",
		ActiveOrganizationID:  "test-org-id",
		WorkOSSessionID:       "workos-session-id",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}
}

func TestTransferManager_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	memCache := newTestMemoryCache()
	manager := NewTransferManager(memCache)
	session := testTransferSession()

	code, err := manager.Create(ctx, session, testSourceHost, testTargetHost)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(code), 43, "code must carry at least 32 bytes of entropy")

	record, err := manager.Lookup(ctx, code, testTargetHost)
	require.NoError(t, err)
	require.Equal(t, TransferRecord{
		UserID:               session.UserID,
		ActiveOrganizationID: session.ActiveOrganizationID,
		WorkOSSessionID:      session.WorkOSSessionID,
		SourceHost:           testSourceHost,
		TargetHost:           testTargetHost,
	}, record)

	require.NoError(t, manager.Consume(ctx, code))
}

func TestTransferManager_StoresNoSessionIDOrCode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	memCache := newTestMemoryCache()
	manager := NewTransferManager(memCache)
	session := testTransferSession()

	code, err := manager.Create(ctx, session, testSourceHost, testTargetHost)
	require.NoError(t, err)

	require.Len(t, memCache.items, 1)
	for key, raw := range memCache.items {
		require.NotContains(t, key, code, "the cache key must not contain the raw code")
		require.NotContains(t, string(raw), session.SessionID)
		require.NotContains(t, string(raw), code)
	}
}

func TestTransferManager_SecondConsumeFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	manager := NewTransferManager(newTestMemoryCache())

	code, err := manager.Create(ctx, testTransferSession(), testSourceHost, testTargetHost)
	require.NoError(t, err)

	require.NoError(t, manager.Consume(ctx, code))
	require.ErrorIs(t, manager.Consume(ctx, code), ErrTransferCodeInvalid)

	_, err = manager.Lookup(ctx, code, testTargetHost)
	require.ErrorIs(t, err, ErrTransferCodeInvalid)
}

func TestTransferManager_UnknownCodeFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	manager := NewTransferManager(newTestMemoryCache())

	_, err := manager.Lookup(ctx, "unknown-code", testTargetHost)
	require.ErrorIs(t, err, ErrTransferCodeInvalid)
	require.ErrorIs(t, manager.Consume(ctx, "unknown-code"), ErrTransferCodeInvalid)
}

func TestTransferManager_ExpiredCodeFails(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	memCache := newTestMemoryCache()
	manager := NewTransferManager(memCache)

	code, err := manager.Create(ctx, testTransferSession(), testSourceHost, testTargetHost)
	require.NoError(t, err)

	// Simulate the cache TTL expiring the record.
	require.NoError(t, memCache.Delete(ctx, transferKey(code)))

	_, err = manager.Lookup(ctx, code, testTargetHost)
	require.ErrorIs(t, err, ErrTransferCodeInvalid)
}

func TestTransferManager_WrongTargetHostDoesNotConsume(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	manager := NewTransferManager(newTestMemoryCache())

	code, err := manager.Create(ctx, testTransferSession(), testSourceHost, testTargetHost)
	require.NoError(t, err)

	_, err = manager.Lookup(ctx, code, "wrong.example.com")
	require.ErrorIs(t, err, ErrTransferCodeInvalid)

	_, err = manager.Lookup(ctx, code, testTargetHost)
	require.NoError(t, err)
	require.NoError(t, manager.Consume(ctx, code))
}

func TestTransferManager_CodesAreUnique(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	manager := NewTransferManager(newTestMemoryCache())

	first, err := manager.Create(ctx, testTransferSession(), testSourceHost, testTargetHost)
	require.NoError(t, err)
	second, err := manager.Create(ctx, testTransferSession(), testSourceHost, testTargetHost)
	require.NoError(t, err)

	require.NotEqual(t, first, second)
	require.False(t, strings.ContainsAny(first, "+/="), "code must be base64url without padding")
}

func TestTransferManager_RejectsImpersonationAndSupportSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	memCache := newTestMemoryCache()
	manager := NewTransferManager(memCache)

	impersonation := testTransferSession()
	impersonation.ImpersonatorEmail = "support@example.com"
	_, err := manager.Create(ctx, impersonation, testSourceHost, testTargetHost)
	require.ErrorIs(t, err, ErrSessionNotTransferable)

	support := testTransferSession()
	support.SupportOrganizationID = "support-org-id"
	_, err = manager.Create(ctx, support, testSourceHost, testTargetHost)
	require.ErrorIs(t, err, ErrSessionNotTransferable)

	require.Empty(t, memCache.items)
}
