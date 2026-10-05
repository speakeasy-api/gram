package sessions

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// errCacheMiss is a local error for test cache methods that are part of the
// interface but never actually called by TransferManager.
var errCacheMiss = errors.New("cache miss")

// testMemoryCache is a minimal in-memory cache for unit tests.
type testMemoryCache struct {
	mu    sync.RWMutex
	items map[string]any
}

var _ cache.Cache = (*testMemoryCache)(nil)

func newTestMemoryCache() *testMemoryCache {
	return &testMemoryCache{items: make(map[string]any)}
}

func (c *testMemoryCache) Get(_ context.Context, key string, value any) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.items[key]; ok {
		ptr, ok := value.(*any)
		if !ok {
			return errCacheMiss
		}
		*ptr = v
		return nil
	}
	return errCacheMiss
}

func (c *testMemoryCache) GetAndDelete(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.items[key]; ok {
		ptr, ok := value.(*any)
		if !ok {
			return errCacheMiss
		}
		*ptr = v
		delete(c.items, key)
		return nil
	}
	return errCacheMiss
}

func (c *testMemoryCache) Set(_ context.Context, key string, value any, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = value
	return nil
}

func (c *testMemoryCache) Add(_ context.Context, key string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; exists {
		return false, nil
	}
	c.items[key] = struct{}{}
	return true, nil
}

func (c *testMemoryCache) Update(_ context.Context, key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = value
	return nil
}

func (c *testMemoryCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	return nil
}

func (c *testMemoryCache) Expire(_ context.Context, _ string, _ time.Duration) error { return nil }
func (c *testMemoryCache) ListAppend(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}
func (c *testMemoryCache) ListRange(_ context.Context, _ string, _, _ int64, _ any) error {
	return nil
}
func (c *testMemoryCache) DeleteByPrefix(_ context.Context, _ string) error { return nil }

func TestTransferManager_CreateAndValidateToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	secret := []byte("test-secret-key-for-transfer")
	memCache := newTestMemoryCache()
	manager := NewTransferManager(secret, memCache)

	session := Session{
		SessionID:             "test-session-id",
		UserID:                "test-user-id",
		ActiveOrganizationID:  "test-org-id",
		WorkOSSessionID:       "workos-session-id",
		ImpersonatorEmail:     "",
		SupportOrganizationID: "",
		SupportExpiresAt:      time.Time{},
	}

	t.Run("successful transfer token flow", func(t *testing.T) {
		t.Parallel()
		token, err := manager.CreateTransferToken(ctx, session, "source.example.com", "target.example.com")
		require.NoError(t, err)
		require.NotEmpty(t, token)

		claims, err := manager.ValidateTransferToken(ctx, token, "target.example.com")
		require.NoError(t, err)
		require.Equal(t, session.SessionID, claims.SessionID)
		require.Equal(t, session.UserID, claims.UserID)
		require.Equal(t, session.ActiveOrganizationID, claims.ActiveOrganizationID)
		require.Equal(t, session.WorkOSSessionID, claims.WorkOSSessionID)
		require.Equal(t, "source.example.com", claims.SourceHost)
		require.Equal(t, "target.example.com", claims.TargetHost)
	})

	t.Run("token is one-time use", func(t *testing.T) {
		t.Parallel()
		token, err := manager.CreateTransferToken(ctx, session, "source.example.com", "target.example.com")
		require.NoError(t, err)

		// First validation should succeed
		_, err = manager.ValidateTransferToken(ctx, token, "target.example.com")
		require.NoError(t, err)

		// Second validation should fail (token already used)
		_, err = manager.ValidateTransferToken(ctx, token, "target.example.com")
		require.Error(t, err)
		require.Contains(t, err.Error(), "already used")
	})

	t.Run("wrong target host fails", func(t *testing.T) {
		t.Parallel()
		token, err := manager.CreateTransferToken(ctx, session, "source.example.com", "target.example.com")
		require.NoError(t, err)

		_, err = manager.ValidateTransferToken(ctx, token, "wrong.example.com")
		require.Error(t, err)
		require.Contains(t, err.Error(), "target host mismatch")
	})

	t.Run("invalid token fails", func(t *testing.T) {
		t.Parallel()
		_, err := manager.ValidateTransferToken(ctx, "invalid-token", "target.example.com")
		require.Error(t, err)
	})

	t.Run("different secret fails", func(t *testing.T) {
		t.Parallel()
		token, err := manager.CreateTransferToken(ctx, session, "source.example.com", "target.example.com")
		require.NoError(t, err)

		wrongManager := NewTransferManager([]byte("wrong-secret"), memCache)
		_, err = wrongManager.ValidateTransferToken(ctx, token, "target.example.com")
		require.Error(t, err)
	})

	t.Run("impersonator email is preserved", func(t *testing.T) {
		t.Parallel()
		sessionWithImpersonator := Session{
			SessionID:             "test-session-2",
			UserID:                "test-user-id",
			ActiveOrganizationID:  "test-org-id",
			ImpersonatorEmail:     "admin@example.com",
			WorkOSSessionID:       "",
			SupportOrganizationID: "",
			SupportExpiresAt:      time.Time{},
		}

		token, err := manager.CreateTransferToken(ctx, sessionWithImpersonator, "source.example.com", "target.example.com")
		require.NoError(t, err)

		claims, err := manager.ValidateTransferToken(ctx, token, "target.example.com")
		require.NoError(t, err)
		require.Equal(t, "admin@example.com", claims.ImpersonatorEmail)
	})
}
