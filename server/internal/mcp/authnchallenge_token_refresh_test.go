package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agents_repo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestHandleToken_ConcurrentRefreshReplayReturnsWinnerResponse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)

	performRefresh := func(mcpSlug, clientID, token string) refreshResult {
		return performRefreshRequest(ctx, ti, mcpSlug, clientID, token)
	}

	const requestCount = 5
	start := make(chan struct{})
	results := make(chan refreshResult, requestCount)
	var requests sync.WaitGroup
	for range requestCount {
		requests.Go(func() {
			<-start
			results <- performRefresh(toolset.McpSlug.String, client.ClientID, refreshToken)
		})
	}
	close(start)
	requests.Wait()
	close(results)

	var winnerBody string
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.code, result.body)
		if winnerBody == "" {
			winnerBody = result.body
		}
		assertSameTokenPair(t, winnerBody, result.body)
	}

	var response map[string]any
	require.NoError(t, json.Unmarshal([]byte(winnerBody), &response))
	require.NotEmpty(t, response["access_token"])
	require.NotEmpty(t, response["refresh_token"])

	otherSlug := "refresh-replay-other-" + uuid.NewString()[:8]
	otherToolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         toolset.OrganizationID,
		ProjectID:              toolset.ProjectID,
		Name:                   "Refresh replay alternate endpoint",
		Slug:                   otherSlug,
		Description:            conv.ToPGText("Alternate OAuth endpoint surface"),
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                conv.ToPGText(otherSlug),
		McpEnabled:             true,
	})
	require.NoError(t, err)
	otherToolset, err = toolsets_repo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                otherToolset.Slug,
		ProjectID:           otherToolset.ProjectID,
	})
	require.NoError(t, err)

	otherEndpoint := performRefresh(otherSlug, client.ClientID, refreshToken)
	require.NoError(t, otherEndpoint.err)
	require.Equal(t, http.StatusOK, otherEndpoint.code, otherEndpoint.body)
	var otherResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(otherEndpoint.body), &otherResponse))
	require.Equal(t, response["refresh_token"], otherResponse.RefreshToken)
	claims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(otherResponse.AccessToken, urn.NewToolset(otherToolset.ID).String())
	require.NoError(t, err)
	require.Equal(t, ti.serverURL.JoinPath("mcp", otherSlug).String(), claims.Issuer)

	activeSessions, err := usersessions_repo.New(ti.conn).ListUserSessionsByProjectID(ctx, usersessions_repo.ListUserSessionsByProjectIDParams{
		ProjectID:           issuer.ProjectID.UUID,
		Status:              pgtype.Text{String: "active", Valid: true},
		SubjectUrn:          pgtype.Text{},
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		ClientID:            uuid.NullUUID{UUID: client.ID, Valid: true},
		ID:                  uuid.NullUUID{},
		Cursor:              uuid.NullUUID{},
		LimitValue:          10,
	})
	require.NoError(t, err)
	require.Len(t, activeSessions, 1)
	require.Equal(t, activeSessions[0].Jti, claims.ID)

	otherClient, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "other-client-" + uuid.NewString(),
		ClientName:              "other client",
		RedirectUris:            []string{"http://localhost:3001/callback"},
		TokenEndpointAuthMethod: "none",
	})
	require.NoError(t, err)

	wrongClient := performRefresh(toolset.McpSlug.String, otherClient.ClientID, refreshToken)
	require.NoError(t, wrongClient.err)
	require.Equal(t, http.StatusBadRequest, wrongClient.code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token was issued to a different client"}`, wrongClient.body)

	unknownToken := "unknown-" + uuid.NewString()
	firstUnknown := performRefresh(toolset.McpSlug.String, client.ClientID, unknownToken)
	require.NoError(t, firstUnknown.err)
	require.Equal(t, http.StatusBadRequest, firstUnknown.code)
	started := time.Now()
	secondUnknown := performRefresh(toolset.McpSlug.String, client.ClientID, unknownToken)
	require.NoError(t, secondUnknown.err)
	require.Equal(t, http.StatusBadRequest, secondUnknown.code)
	require.Less(t, time.Since(started), 3*time.Second, "cached terminal refresh failures must not wait for the replay grace period")
}

func TestApplyIssuerGate_AgentSessionAdmitsLiveParent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx, agent, _, session := seedAgentRefreshSession(t, ctx, ti)
	issuer := ti.serverURL.JoinPath("mcp", fx.toolset.McpSlug.String).String()
	accessToken, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:   session.SubjectUrn,
		Audience:  urn.NewToolset(fx.toolset.ID).String(),
		Issuer:    issuer,
		ExpiresAt: &session.ExpiresAt.Time,
		ClientID:  fx.client.ClientID,
		JTI:       session.Jti,
	})
	require.NoError(t, err)
	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewToolset(fx.toolset.ID).String(),
		OrganizationID:      fx.orgID,
		ProjectID:           fx.target.ProjectID,
		RouteBase:           "mcp",
		Slug:                fx.toolset.McpSlug.String,
		ToolsetID:           uuid.NullUUID{UUID: fx.toolset.ID, Valid: true},
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
	}

	w := httptest.NewRecorder()
	admittedCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, accessToken, ti.serverURL.String(), endpoint)
	require.NoError(t, err)
	actor, ok := contextvalues.AuthenticatedActor(admittedCtx)
	require.True(t, ok)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()).String(), actor.String())
	credential, ok := contextvalues.PrincipalCredentialAuthorization(admittedCtx)
	require.True(t, ok)
	require.Equal(t, fx.userID, credential.AuthorizerUserID)

	_, err = agents_repo.New(ti.conn).SuspendAgent(ctx, agents_repo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, accessToken, ti.serverURL.String(), endpoint)
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleToken_AgentRefreshPreservesCredentialProfile(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx, _, refreshToken, oldSession := seedAgentRefreshSession(t, ctx, ti)

	result := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.code, result.body)
	var response tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(result.body), &response))
	claims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(response.AccessToken, urn.NewToolset(fx.toolset.ID).String())
	require.NoError(t, err)

	rotated, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	require.Equal(t, oldSession.SubjectUrn.String(), rotated.SubjectUrn.String())
	require.Equal(t, oldSession.AuthorizerUserID, rotated.AuthorizerUserID)
	require.JSONEq(t, string(oldSession.DelegatedGrants), string(rotated.DelegatedGrants))
	require.Equal(t, oldSession.DelegatedGrantsVersion, rotated.DelegatedGrantsVersion)
}

func TestHandleToken_AgentRefreshReplayDeniesDirectlyRevokedSuccessor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx, _, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)
	winner := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, winner.err)
	require.Equal(t, http.StatusOK, winner.code, winner.body)
	var response tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(winner.body), &response))
	claims, err := sessiontokens.NewSigner("test-jwt-secret").Validate(response.AccessToken, urn.NewToolset(fx.toolset.ID).String())
	require.NoError(t, err)
	successor, err := usersessions_repo.New(ti.conn).GetUserSessionByJTI(ctx, usersessions_repo.GetUserSessionByJTIParams{
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
		Jti:                 claims.ID,
	})
	require.NoError(t, err)
	_, err = usersessions_repo.New(ti.conn).RevokeUserSession(ctx, usersessions_repo.RevokeUserSessionParams{
		ID:             successor.ID,
		ProjectID:      fx.target.ProjectID,
		OrganizationID: fx.orgID,
	})
	require.NoError(t, err)

	replay := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, replay.err)
	require.Equal(t, http.StatusBadRequest, replay.code, replay.body)
	require.Contains(t, replay.body, "refreshed session is no longer active")
}

func TestHandleToken_AgentRefreshDeniesSuspendedParent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx, agent, refreshToken, _ := seedAgentRefreshSession(t, ctx, ti)
	_, err := agents_repo.New(ti.conn).SuspendAgent(ctx, agents_repo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)

	result := performRefreshRequest(ctx, ti, fx.toolset.McpSlug.String, fx.client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusBadRequest, result.code, result.body)
	require.Contains(t, result.body, "agent authorization is no longer valid")
}

func TestHandleToken_UnpublishedRollbackReleasesLease(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return failingConditionalCache{Cache: delegate}
	})
	toolset, issuer, client, _ := seedRefreshReplaySession(t, ctx, ti)
	unknownToken := "unknown-" + uuid.NewString()
	_, lockKey := refreshReplayKeys(issuer.ID, unknownToken)

	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, unknownToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusBadRequest, result.code, result.body)

	claimed, err := ti.cacheAdapter.Add(ctx, lockKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, claimed, "an unpublished outcome with no database change must release its lease")
}

func TestHandleToken_RefreshReplayCacheErrorIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return failingReplayGetCache{Cache: delegate}
	})
	toolset, issuer, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)
	_, lockKey := refreshReplayKeys(issuer.ID, refreshToken)
	claimed, err := ti.cacheAdapter.Add(ctx, lockKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)

	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusServiceUnavailable, result.code, result.body)
	require.JSONEq(t, `{"error":"temporarily_unavailable","error_description":"refresh token rotation is still in progress"}`, result.body)
}

func TestHandleToken_RefreshReplayRequestDeadlineIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)
	_, lockKey := refreshReplayKeys(issuer.ID, refreshToken)
	claimed, err := ti.cacheAdapter.Add(ctx, lockKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)

	deadlineCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	result := performRefreshRequest(deadlineCtx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusServiceUnavailable, result.code, result.body)
	require.JSONEq(t, `{"error":"temporarily_unavailable","error_description":"refresh token rotation is still in progress"}`, result.body)
}

func TestHandleToken_RefreshReplayWaitTimeoutIsRetryable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return contendedLeaseCache{Cache: delegate}
	})
	toolset, _, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)

	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusServiceUnavailable, result.code, result.body)
	require.JSONEq(t, `{"error":"temporarily_unavailable","error_description":"refresh token rotation is still in progress"}`, result.body)
}

func TestHandleToken_RefreshReplayWaiterBecomesLeaderAfterLockRelease(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)
	_, lockKey := refreshReplayKeys(issuer.ID, refreshToken)

	claimed, err := ti.cacheAdapter.Add(ctx, lockKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, claimed)

	results := make(chan refreshResult, 1)
	go func() {
		results <- performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	}()

	leaderWindow := time.NewTimer(100 * time.Millisecond)
	defer leaderWindow.Stop()
	select {
	case result := <-results:
		require.Failf(t, "request completed while failed leader lock was held", "status=%d body=%s err=%v", result.code, result.body, result.err)
	case <-leaderWindow.C:
	}

	require.NoError(t, ti.cacheAdapter.Delete(ctx, lockKey))
	completion := time.NewTimer(3 * time.Second)
	defer completion.Stop()
	select {
	case result := <-results:
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.code, result.body)
	case <-completion.C:
		require.Fail(t, "waiting refresh request did not take over after leader lock release")
	}
}

func TestHandleToken_RefreshRotationPublishesAfterRequestCancellation(t *testing.T) {
	t.Parallel()

	var cancel context.CancelFunc
	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return cancelOnReplaySetCache{Cache: delegate, cancel: func() {
			if cancel != nil {
				cancel()
			}
		}}
	})
	toolset, _, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)
	requestCtx, requestCancel := context.WithCancel(ctx)
	cancel = requestCancel
	first := performRefreshRequest(requestCtx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)

	replayed := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, replayed.err)
	require.Equal(t, http.StatusOK, replayed.code, replayed.body)
	assertSameTokenPair(t, first.body, replayed.body)
}

func TestHandleToken_RefreshRotationFallsBackWhenRedisFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithCacheWrapper(t, func(delegate cache.Cache) cache.Cache {
		return failingAddCache{Cache: delegate}
	})
	toolset, _, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)

	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.code, result.body)

	var response tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(result.body), &response))
	require.NotEmpty(t, response.AccessToken)
	require.NotEmpty(t, response.RefreshToken)
}

func TestHandleToken_RefreshRotationRevokesOldAccessToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, refreshToken, oldJTI := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.code, result.body)
	revoked, err := ti.chatSessionsManager.IsTokenRevoked(ctx, oldJTI)
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestHandleToken_ExpiredRefreshRevokesOldAccessToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, _, client, refreshToken, oldJTI := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(-time.Minute))
	result := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusBadRequest, result.code, result.body)
	revoked, err := ti.chatSessionsManager.IsTokenRevoked(ctx, oldJTI)
	require.NoError(t, err)
	require.True(t, revoked)
}

func TestHandleToken_RefreshReplayExpiresBackToInvalidGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)

	replayKey, lockKey := refreshReplayKeys(issuer.ID, refreshToken)
	// Both keys expire at the grace boundary. Removing them models the state
	// after that TTL without making the test wait for wall-clock expiration.
	require.NoError(t, ti.cacheAdapter.Delete(ctx, lockKey))
	require.NoError(t, ti.cacheAdapter.Delete(ctx, replayKey+":"))
	expired := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, expired.err)
	require.Equal(t, http.StatusBadRequest, expired.code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, expired.body)
}

func TestHandleToken_RefreshRotationAdoptsCachedWinnerAfterLockLoss(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client, refreshToken := seedRefreshReplaySession(t, ctx, ti)
	first := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, first.err)
	require.Equal(t, http.StatusOK, first.code, first.body)

	_, lockKey := refreshReplayKeys(issuer.ID, refreshToken)
	require.NoError(t, ti.cacheAdapter.Delete(ctx, lockKey))

	adopted := performRefreshRequest(ctx, ti, toolset.McpSlug.String, client.ClientID, refreshToken)
	require.NoError(t, adopted.err)
	require.Equal(t, http.StatusOK, adopted.code, adopted.body)
	assertSameTokenPair(t, first.body, adopted.body)

	claimed, err := ti.cacheAdapter.Add(ctx, lockKey, 30*time.Second)
	require.NoError(t, err)
	require.True(t, claimed, "adopting a cached result must release the newly acquired lock")
}

type refreshResult struct {
	body string
	code int
	err  error
}

type tokenResponseFixture struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type failingAddCache struct {
	cache.Cache
}

type failingConditionalCache struct {
	cache.Cache
}

func (c failingConditionalCache) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return acquireLease(ctx, c.Cache, key, owner, ttl)
}

func (c failingConditionalCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, c.Cache, key, owner)
}

func (failingConditionalCache) SetIfAbsent(context.Context, string, any, time.Duration) (bool, error) {
	return false, errors.New("refresh replay publication unavailable")
}

type contendedLeaseCache struct {
	cache.Cache
}

func (contendedLeaseCache) AcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func (c contendedLeaseCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, c.Cache, key, owner)
}

func (c contendedLeaseCache) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return setIfAbsent(ctx, c.Cache, key, value, ttl)
}

func (failingAddCache) Add(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("refresh replay coordination unavailable")
}

func (failingAddCache) AcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("refresh replay coordination unavailable")
}

func (f failingAddCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, f.Cache, key, owner)
}

func (f failingAddCache) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return setIfAbsent(ctx, f.Cache, key, value, ttl)
}

type cancelOnReplaySetCache struct {
	cache.Cache
	cancel func()
}

func (c cancelOnReplaySetCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if strings.HasPrefix(key, "userSessionRefreshReplay:") {
		c.cancel()
	}
	if err := c.Cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("set cache value: %w", err)
	}
	return nil
}

func (c cancelOnReplaySetCache) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return acquireLease(ctx, c.Cache, key, owner, ttl)
}

func (c cancelOnReplaySetCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, c.Cache, key, owner)
}

func (c cancelOnReplaySetCache) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return setIfAbsent(ctx, c.Cache, key, value, ttl)
}

type failingReplayGetCache struct {
	cache.Cache
}

func (c failingReplayGetCache) Get(ctx context.Context, key string, value any) error {
	if strings.HasPrefix(key, "userSessionRefreshReplay:") {
		return errors.New("refresh replay cache unavailable")
	}
	if err := c.Cache.Get(ctx, key, value); err != nil {
		return fmt.Errorf("get cache value: %w", err)
	}
	return nil
}

func (c failingReplayGetCache) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	return acquireLease(ctx, c.Cache, key, owner, ttl)
}

func (c failingReplayGetCache) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	return releaseLease(ctx, c.Cache, key, owner)
}

func (c failingReplayGetCache) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	return setIfAbsent(ctx, c.Cache, key, value, ttl)
}

func acquireLease(ctx context.Context, delegate cache.Cache, key, owner string, ttl time.Duration) (bool, error) {
	leases, ok := delegate.(cache.LeaseCache)
	if !ok {
		return false, errors.New("cache does not support leases")
	}
	acquired, err := leases.AcquireLease(ctx, key, owner, ttl)
	if err != nil {
		return false, fmt.Errorf("acquire cache lease: %w", err)
	}
	return acquired, nil
}

func releaseLease(ctx context.Context, delegate cache.Cache, key, owner string) (bool, error) {
	leases, ok := delegate.(cache.LeaseCache)
	if !ok {
		return false, errors.New("cache does not support leases")
	}
	released, err := leases.ReleaseLeaseIfOwner(ctx, key, owner)
	if err != nil {
		return false, fmt.Errorf("release cache lease: %w", err)
	}
	return released, nil
}

func setIfAbsent(ctx context.Context, delegate cache.Cache, key string, value any, ttl time.Duration) (bool, error) {
	conditional, ok := delegate.(cache.ConditionalCache)
	if !ok {
		return false, errors.New("cache does not support conditional writes")
	}
	stored, err := conditional.SetIfAbsent(ctx, key, value, ttl)
	if err != nil {
		return false, fmt.Errorf("conditionally set cache value: %w", err)
	}
	return stored, nil
}

func assertSameTokenPair(t *testing.T, expectedBody, actualBody string) {
	t.Helper()
	var expected, actual tokenResponseFixture
	require.NoError(t, json.Unmarshal([]byte(expectedBody), &expected))
	require.NoError(t, json.Unmarshal([]byte(actualBody), &actual))
	require.Equal(t, expected.AccessToken, actual.AccessToken)
	require.Equal(t, expected.RefreshToken, actual.RefreshToken)
}

func seedAgentRefreshSession(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
) (agentConsentFixture, agents_repo.Agent, string, usersessions_repo.UserSession) {
	t.Helper()

	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Refresh subject agent")
	seedUserMCPConnectGrant(t, ctx, ti.conn, fx.orgID, fx.userID, fx.target.MCPResourceID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	policy, err := authz.NewDelegatedPolicyV1([]authz.Grant{{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   fx.target.MCPResourceID.String(),
			authz.SelectorKeyProjectID:    fx.target.ProjectID.String(),
		},
	}})
	require.NoError(t, err)
	delegatedGrants, err := authz.EncodeDelegatedPolicy(authz.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	refreshToken := "agent-refresh-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	oldJTIHash := sha256.Sum256([]byte("agent-jti-" + uuid.NewString()))
	session, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID:    fx.target.UserSessionIssuerID,
		UserSessionClientID:    uuid.NullUUID{UUID: fx.client.ID, Valid: true},
		SubjectUrn:             urn.NewAgentSubject(agent.ID),
		AuthorizerUserID:       pgtype.Text{String: fx.userID, Valid: true},
		DelegatedGrants:        delegatedGrants,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(authz.CurrentDelegatedPolicyVersion), Valid: true},
		Jti:                    base64.RawURLEncoding.EncodeToString(oldJTIHash[:]),
		RefreshTokenHash:       base64.RawURLEncoding.EncodeToString(refreshHash[:]),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
	})
	require.NoError(t, err)
	return fx, agent, refreshToken, session
}

func seedRefreshReplaySession(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
) (toolsets_repo.Toolset, usersessions_repo.UserSessionIssuer, usersessions_repo.UserSessionClient, string) {
	t.Helper()
	toolset, issuer, client, refreshToken, _ := seedRefreshReplaySessionDetails(t, ctx, ti, time.Now().Add(time.Hour))
	return toolset, issuer, client, refreshToken
}

func seedRefreshReplaySessionDetails(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	refreshExpiresAt time.Time,
) (toolsets_repo.Toolset, usersessions_repo.UserSessionIssuer, usersessions_repo.UserSessionClient, string, string) {
	t.Helper()

	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	refreshToken := "refresh-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	oldJTI := uuid.NewString()
	_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		SubjectUrn:          urn.NewUserSubject("refresh-replay-user"),
		Jti:                 oldJTI,
		RefreshTokenHash:    base64.RawURLEncoding.EncodeToString(refreshHash[:]),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: refreshExpiresAt, Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
	})
	require.NoError(t, err)

	return toolset, issuer, client, refreshToken, oldJTI
}

func performRefreshRequest(ctx context.Context, ti *testInstance, mcpSlug, clientID, token string) refreshResult {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {token},
		"client_id":     {clientID},
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp/"+mcpSlug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("mcpSlug", mcpSlug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeCtx))

	w := httptest.NewRecorder()
	requestErr := ti.service.HandleToken(w, req)
	return refreshResult{body: w.Body.String(), code: w.Code, err: requestErr}
}

func refreshReplayKeys(issuerID uuid.UUID, refreshToken string) (replayKey, lockKey string) {
	refreshHash := sha256.Sum256([]byte(refreshToken))
	replayKey = "userSessionRefreshReplay:" + issuerID.String() + ":" + base64.RawURLEncoding.EncodeToString(refreshHash[:])
	return replayKey, "lock:" + replayKey
}
