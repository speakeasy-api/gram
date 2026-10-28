package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestConnectionPopupGrantOffersAdditionalDelegation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"dashboard", "agent refresh", "explicit reconnect", "third party"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			s := &Service{authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](testenv.NewLogger(t), &popupGrantCASCache{Cache: testenv.NewMemoryCache(), attempts: 1}, cache.SuffixNone)}
			subject := urn.NewUserSubject("example-user")
			parent := AuthnChallengeState{ID: uuid.NewString(), FirstParty: scenario != "third party", AuthorizerUserID: subject.ID, AuthorizerImpersonated: new(false), Subject: &subject, UserSessionIssuerID: uuid.New(), CreatedAt: time.Now()}
			client := uuid.New()
			if scenario == "agent refresh" {
				parent.BindingRefreshAttemptID = uuid.NewString()
			}
			if scenario == "explicit reconnect" {
				parent.ReauthenticateClientID = client
			}
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
			grant := remotesessions.RemoteGrant{ConnectionPopup: scenario != "third party", ParentChallengeID: parent.ID, Subject: subject, UserSessionIssuerID: parent.UserSessionIssuerID, RemoteSessionClientID: client, RemoteSessionID: uuid.New(), GrantGeneration: 7}
			require.NoError(t, s.recordConnectionPopupGrant(t.Context(), grant))
			current, err := s.authnChallengeCache.Get(t.Context(), parent.CacheKey())
			require.NoError(t, err)
			if scenario == "dashboard" || scenario == "third party" {
				require.Equal(t, &grant, current.PendingDelegation)
			} else {
				require.Nil(t, current.PendingDelegation)
			}
		})
	}
}

func TestConnectionPopupGrantPreservesPendingGrant(t *testing.T) {
	t.Parallel()
	s := &Service{authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](testenv.NewLogger(t), &popupGrantCASCache{Cache: testenv.NewMemoryCache(), attempts: 1}, cache.SuffixNone)}
	subject := urn.NewUserSubject("example-user")
	parent := AuthnChallengeState{ID: uuid.NewString(), AuthorizerUserID: subject.ID, AuthorizerImpersonated: new(false), Subject: &subject, UserSessionIssuerID: uuid.New(), CreatedAt: time.Now()}
	grant := remotesessions.RemoteGrant{ParentChallengeID: parent.ID, Subject: subject, UserSessionIssuerID: parent.UserSessionIssuerID, RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New(), GrantGeneration: 7}
	parent.PendingDelegation = &grant
	require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
	require.NoError(t, s.recordConnectionPopupGrant(t.Context(), grant))
	later := grant
	later.GrantGeneration++
	require.Error(t, s.recordConnectionPopupGrant(t.Context(), later))
	current, err := s.authnChallengeCache.Get(t.Context(), parent.CacheKey())
	require.NoError(t, err)
	require.Equal(t, &grant, current.PendingDelegation)
}

func TestUnavailableDelegationReloadsBeforeContinuing(t *testing.T) {
	t.Parallel()
	for _, concurrentChange := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrentChange), func(t *testing.T) {
			t.Parallel()
			adapter := &popupGrantCASCache{Cache: testenv.NewMemoryCache(), attempts: 1, alwaysLose: concurrentChange}
			s := &Service{authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](testenv.NewLogger(t), adapter, cache.SuffixNone)}
			subject := urn.NewUserSubject("example-user")
			state := AuthnChallengeState{ID: uuid.NewString(), Subject: &subject, AuthorizerUserID: subject.ID, AuthorizerImpersonated: new(false), UserSessionIssuerID: uuid.New(), CreatedAt: time.Now(), PendingDelegation: &remotesessions.RemoteGrant{Subject: subject, RemoteSessionID: uuid.New(), GrantGeneration: 7}}
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), state))
			response := httptest.NewRecorder()
			// No delegation provider means no choices can be rendered. The same
			// branch handles an empty eligible-agent result from the provider.
			handled, err := s.servePostLoginDelegation(response, httptest.NewRequest(http.MethodGet, "/connect", nil).WithContext(t.Context()), &ResolvedMcpEndpoint{}, state)
			require.NoError(t, err)
			require.True(t, handled)
			require.Equal(t, http.StatusSeeOther, response.Code)
			require.Contains(t, response.Header().Get("Location"), "?state="+state.ID)
			current, err := s.authnChallengeCache.Get(t.Context(), state.CacheKey())
			require.NoError(t, err)
			if concurrentChange {
				require.Equal(t, "concurrent-selection", current.FocusServer)
				require.NotNil(t, current.PendingDelegation)
			} else {
				require.Nil(t, current.PendingDelegation)
				next := remotesessions.RemoteGrant{ParentChallengeID: state.ID, Subject: subject, UserSessionIssuerID: state.UserSessionIssuerID, RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New(), GrantGeneration: 8}
				require.NoError(t, s.recordConnectionPopupGrant(t.Context(), next))
				current, err = s.authnChallengeCache.Get(t.Context(), state.CacheKey())
				require.NoError(t, err)
				require.Equal(t, &next, current.PendingDelegation)
			}
		})
	}
}
