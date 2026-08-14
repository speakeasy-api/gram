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
	performRefresh := func(clientID string) refreshResult {
		form := url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {refreshToken},
			"client_id":     {clientID},
		}
		req := httptest.NewRequest(http.MethodPost, "/mcp/"+toolset.McpSlug.String+"/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("mcpSlug", toolset.McpSlug.String)
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
			results <- performRefresh(client.ClientID)
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

	otherClient, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID: issuer.ID,
		ClientID:            "other-client-" + uuid.NewString(),
		ClientName:          "other client",
		RedirectUris:        []string{"http://localhost:3001/callback"},
	})
	require.NoError(t, err)

	wrongClient := performRefresh(otherClient.ClientID)
	require.NoError(t, wrongClient.err)
	require.Equal(t, http.StatusBadRequest, wrongClient.code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token was issued to a different client"}`, wrongClient.body)
}
