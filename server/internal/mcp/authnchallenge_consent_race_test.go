package mcp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/stretchr/testify/require"
)

type callbackBeforeConsentClaim struct {
	cache.Cache
	cas     cache.CompareAndSwapCache
	publish func(context.Context) error
}

func (c *callbackBeforeConsentClaim) CompareAndSwap(ctx context.Context, key string, expected, replacement any, ttl time.Duration) (bool, error) {
	swapped, err := c.cas.CompareAndSwap(ctx, key, expected, replacement, ttl)
	if err != nil {
		return false, fmt.Errorf("swap test challenge: %w", err)
	}
	return swapped, nil
}
func (c *callbackBeforeConsentClaim) CompareAndDelete(ctx context.Context, key string, expected any) (bool, error) {
	if err := c.publish(ctx); err != nil {
		return false, err
	}
	deleted, err := c.cas.CompareAndDelete(ctx, key, expected)
	if err != nil {
		return false, fmt.Errorf("claim test challenge: %w", err)
	}
	return deleted, nil
}
func (c *callbackBeforeConsentClaim) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.publish(ctx); err != nil {
		return err
	}
	if err := c.Cache.GetAndDelete(ctx, key, value); err != nil {
		return fmt.Errorf("consume test challenge: %w", err)
	}
	return nil
}

func TestConsentApprovalPreservesConcurrentCallback(t *testing.T) {
	t.Parallel()
	var adapter *callbackBeforeConsentClaim
	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		cas, ok := delegate.(cache.CompareAndSwapCache)
		require.True(t, ok)
		adapter = &callbackBeforeConsentClaim{Cache: delegate, cas: cas}
		return adapter
	})
	fx := newAgentConsentFixture(t, ctx, ti)
	state, err := ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	grant := remotesessions.RemoteGrant{RemoteSessionID: uuid.New(), RemoteSessionClientID: uuid.New(), GrantGeneration: 2, Subject: *state.Subject}
	adapter.publish = func(ctx context.Context) error {
		updated := state
		updated.PendingDelegation = &grant
		return ti.authnChallengeCache.Store(ctx, updated)
	}
	form := url.Values{"state": {fx.stateID}, "csrf_token": {fx.csrf}, "action": {"approve"}}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.toolset.McpSlug.String+"/connect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", fx.toolset.McpSlug.String)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	err = ti.service.HandleConsent(response, req)
	require.ErrorContains(t, err, "consent changed")
	require.Empty(t, response.Header().Get("Location"), "must not redirect a newly minted client grant")
	current, err := ti.authnChallengeCache.Get(ctx, state.CacheKey())
	require.NoError(t, err)
	require.NotNil(t, current.PendingDelegation)
	require.Equal(t, grant.RemoteSessionID, current.PendingDelegation.RemoteSessionID)
}
