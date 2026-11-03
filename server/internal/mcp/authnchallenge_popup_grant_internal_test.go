package mcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type popupGrantCASCache struct {
	cache.Cache
	attempts   int
	alwaysLose bool
}

func (c *popupGrantCASCache) CompareAndSwap(ctx context.Context, key string, expected, replacement any, ttl time.Duration) (bool, error) {
	c.attempts++
	if c.alwaysLose || c.attempts == 1 {
		changed, ok := expected.(AuthnChallengeState)
		if !ok {
			return false, fmt.Errorf("unexpected challenge type %T", expected)
		}
		changed.FocusServer = "concurrent-selection"
		if err := c.Set(ctx, key, changed, ttl); err != nil {
			return false, fmt.Errorf("write concurrent consent: %w", err)
		}
		return false, nil
	}
	if err := c.Set(ctx, key, replacement, ttl); err != nil {
		return false, fmt.Errorf("write completed consent: %w", err)
	}
	return true, nil
}
func (c *popupGrantCASCache) CompareAndDelete(context.Context, string, any) (bool, error) {
	return false, nil
}

func TestConnectionPopupGrantRetriesChallengeCAS(t *testing.T) {
	t.Parallel()
	for _, alwaysLose := range []bool{false, true} {
		t.Run(fmt.Sprint(alwaysLose), func(t *testing.T) {
			t.Parallel()
			adapter := &popupGrantCASCache{Cache: testenv.NewMemoryCache(), alwaysLose: alwaysLose}
			s := &Service{authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](testenv.NewLogger(t), adapter, cache.SuffixNone)}
			subject := urn.NewUserSubject("example-user")
			parent := AuthnChallengeState{ID: uuid.NewString(), FirstParty: true, Subject: &subject, UserSessionIssuerID: uuid.New(), ReauthenticateClientID: uuid.New(), CreatedAt: time.Now()}
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
			err := s.recordConnectionPopupGrant(t.Context(), remotesessions.RemoteGrant{ConnectionPopup: true, ParentChallengeID: parent.ID, Subject: subject, UserSessionIssuerID: parent.UserSessionIssuerID, RemoteSessionClientID: parent.ReauthenticateClientID})
			current, loadErr := s.authnChallengeCache.Get(t.Context(), parent.CacheKey())
			require.NoError(t, loadErr)
			require.Equal(t, "concurrent-selection", current.FocusServer)
			if alwaysLose {
				require.Error(t, err)
				require.Equal(t, parent.ReauthenticateClientID, current.ReauthenticateClientID)
			} else {
				require.NoError(t, err)
				require.Equal(t, uuid.Nil, current.ReauthenticateClientID)
				require.Equal(t, 2, adapter.attempts)
			}
		})
	}
}
