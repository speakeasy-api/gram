package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestHandleToken_ConcurrentRefreshReplayReturnsWinnerResponse(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)

	refreshToken := "refresh-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	_, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID: issuer.ID,
		UserSessionClientID: uuid.NullUUID{UUID: client.ID, Valid: true},
		SubjectUrn:          urn.NewUserSubject("refresh-replay-user"),
		Jti:                 uuid.NewString(),
		RefreshTokenHash:    base64.RawURLEncoding.EncodeToString(refreshHash[:]),
		RefreshExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:           pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
	})
	require.NoError(t, err)

	type refreshResult struct {
		body string
		code int
		err  error
	}
	performRefresh := func(mcpSlug, clientID, token string) refreshResult {
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
		require.JSONEq(t, winnerBody, result.body)
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
		ProjectID:           issuer.ProjectID,
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
		UserSessionIssuerID: issuer.ID,
		ClientID:            "other-client-" + uuid.NewString(),
		ClientName:          "other client",
		RedirectUris:        []string{"http://localhost:3001/callback"},
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
