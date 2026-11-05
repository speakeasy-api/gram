package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type recoveryReadFailureCache struct {
	cache.Cache
	err error
}

func (c recoveryReadFailureCache) Get(context.Context, string, any) error {
	return c.err
}

func TestConnectionRecoveryCompletionCacheReadFailure(t *testing.T) {
	t.Parallel()
	unavailable := errors.New("cache unavailable")
	for _, tt := range []struct {
		name    string
		cause   error
		expired bool
	}{
		{name: "missing", cause: redisCache.ErrCacheMiss, expired: true},
		{name: "wrapped missing", cause: fmt.Errorf("read: %w", redisCache.ErrCacheMiss), expired: true},
		{name: "unavailable", cause: unavailable},
		{name: "canceled", cause: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Service{connectionRecoveryCache: cache.NewTypedObjectCache[connectionRecovery](testenv.NewLogger(t), recoveryReadFailureCache{err: tt.cause}, cache.SuffixNone)}
			req := httptest.NewRequest("GET", connectionRecoveryDonePath, nil)
			handled, err := s.verifyConnectionRecoveryCompletion(req, remotesessions.RemoteLoginState{FinalRedirectURI: connectionRecoveryDonePath, ParentChallengeID: "login"})
			require.True(t, handled)
			if tt.expired {
				require.ErrorIs(t, err, errRecoveryLoginExpired)
			} else {
				require.ErrorIs(t, err, tt.cause)
				require.NotErrorIs(t, err, errRecoveryLoginExpired)
				require.Equal(t, mcpconnectionattempts.CodeInternalError, remoteLoginRefusalHappened(err))
			}
		})
	}
}

func TestConnectionRecoveryCompletionInvalidStoredLogin(t *testing.T) {
	t.Parallel()
	for _, login := range []connectionRecovery{
		{ID: "login", CreatedAt: time.Now().Add(-time.Hour)},
		{ID: "other", CreatedAt: time.Now()},
	} {
		t.Run(login.ID, func(t *testing.T) {
			t.Parallel()
			backend := testenv.NewMemoryCache()
			s := &Service{connectionRecoveryCache: cache.NewTypedObjectCache[connectionRecovery](testenv.NewLogger(t), backend, cache.SuffixNone)}
			require.NoError(t, backend.Set(t.Context(), "connectionRecovery:login:", login, time.Minute))
			handled, err := s.verifyConnectionRecoveryCompletion(httptest.NewRequest("GET", connectionRecoveryDonePath, nil), remotesessions.RemoteLoginState{FinalRedirectURI: connectionRecoveryDonePath, ParentChallengeID: "login"})
			require.True(t, handled)
			require.ErrorIs(t, err, errRecoveryLoginExpired)
		})
	}
}
