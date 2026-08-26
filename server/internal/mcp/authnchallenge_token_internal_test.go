package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestWriteRefreshTokenReplayRejectsDifferentCacheKey(t *testing.T) {
	t.Parallel()

	service, endpoint, clientRow, replay := newRefreshTokenReplayTestFixture(t, time.Now().Add(time.Hour))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", nil)
	err := service.writeRefreshTokenReplay(
		t.Context(),
		w,
		r,
		endpoint,
		clientRow,
		"https://gram.example",
		"none",
		"userSessionRefreshReplay:different",
		replay,
		testenv.NewLogger(t),
	)
	require.ErrorContains(t, err, "refresh token replay response key mismatch")
}

func TestWriteRefreshTokenReplayRejectsExpiredCredentials(t *testing.T) {
	t.Parallel()

	service, endpoint, clientRow, replay := newRefreshTokenReplayTestFixture(t, time.Now().Add(-time.Second))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", nil)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	err := service.writeRefreshTokenReplay(
		t.Context(),
		w,
		r,
		endpoint,
		clientRow,
		"https://gram.example",
		"none",
		"userSessionRefreshReplay:expected",
		replay,
		logger,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token has expired"}`, w.Body.String())
	require.Contains(t, logs.String(), `"gram.oauth.failure_reason":"refresh_token_expired"`)
}

func TestWriteRefreshTokenReplayRecomputesRemainingLifetimes(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(10 * time.Minute)
	service, endpoint, clientRow, replay := newRefreshTokenReplayTestFixture(t, expiresAt)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", nil)
	err := service.writeRefreshTokenReplay(
		t.Context(), w, r, endpoint, clientRow, "https://gram.example", "none",
		"userSessionRefreshReplay:expected", replay, testenv.NewLogger(t),
	)
	require.NoError(t, err)

	var response tokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.InDelta(t, 600, response.ExpiresIn, 2)
	require.InDelta(t, 600, response.AuthorizationExpiresIn, 2)
}

func TestWriteRefreshTokenReplayFailurePrecedesClientBinding(t *testing.T) {
	t.Parallel()

	service, endpoint, clientRow, replay := newRefreshTokenReplayTestFixture(t, time.Now().Add(time.Hour))
	plaintext, err := service.enc.Decrypt(replay.Ciphertext)
	require.NoError(t, err)
	var payload userSessionRefreshReplayPayload
	require.NoError(t, json.Unmarshal([]byte(plaintext), &payload))
	payload.ClientID = uuid.New()
	payload.ErrorDescription = "refresh_token is unknown or already used"
	payload.FailureReason = "refresh_token_unknown_or_already_used"
	payload.Subject = nil
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	replay.Ciphertext, err = service.enc.Encrypt(encoded)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", nil)
	err = service.writeRefreshTokenReplay(
		t.Context(), w, r, endpoint, clientRow, "https://gram.example", "none",
		"userSessionRefreshReplay:expected", replay, testenv.NewLogger(t),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token is unknown or already used"}`, w.Body.String())
}

func TestStoreRefreshTokenReplayFailureDoesNotOverwriteSuccess(t *testing.T) {
	t.Parallel()

	service, _, clientRow, replay := newRefreshTokenReplayTestFixture(t, time.Now().Add(time.Hour))
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	service.userSessionRefreshReplayCache = cache.NewTypedObjectCache[userSessionRefreshReplay](
		testenv.NewLogger(t), cache.NewRedisCacheAdapter(client), cache.SuffixNone,
	)
	require.NoError(t, service.userSessionRefreshReplayCache.Store(t.Context(), replay))

	stored := service.storeRefreshTokenReplayFailure(
		t.Context(), replay.Key, clientRow.ID,
		"refresh_token_unknown_or_already_used",
		"refresh_token is unknown or already used", testenv.NewLogger(t),
	)
	require.False(t, stored)

	got, err := service.userSessionRefreshReplayCache.Get(t.Context(), replay.Key)
	require.NoError(t, err)
	plaintext, err := service.enc.Decrypt(got.Ciphertext)
	require.NoError(t, err)
	var payload userSessionRefreshReplayPayload
	require.NoError(t, json.Unmarshal([]byte(plaintext), &payload))
	require.Empty(t, payload.ErrorDescription)
	require.Equal(t, "refresh-token", payload.Response.RefreshToken)
}

func TestWriteRefreshTokenReplayRecordsServedEvent(t *testing.T) {
	t.Parallel()

	service, endpoint, clientRow, replay := newRefreshTokenReplayTestFixture(t, time.Now().Add(time.Hour))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/mcp/test/token", nil)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	err := service.writeRefreshTokenReplay(
		t.Context(),
		w,
		r,
		endpoint,
		clientRow,
		"https://gram.example",
		"none",
		"userSessionRefreshReplay:expected",
		replay,
		logger,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, logs.String(), `"msg":"oauth refresh_token replay served"`)
}

func newRefreshTokenReplayTestFixture(
	t *testing.T,
	expiresAt time.Time,
) (*Service, *ResolvedMcpEndpoint, *usersessions_repo.UserSessionClient, userSessionRefreshReplay) {
	t.Helper()

	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	clientID := uuid.New()
	var clientRow usersessions_repo.UserSessionClient
	clientRow.ID = clientID
	clientRow.ClientID = "client-id"
	endpoint := &ResolvedMcpEndpoint{
		AudienceURN:          "toolset:test",
		CIMDAdmissionModeRaw: pgtype.Text{},
		CustomDomainID:       uuid.NullUUID{},
		IsPublic:             false,
		McpServerID:          uuid.NullUUID{},
		OrganizationID:       "org_test",
		ProjectID:            uuid.New(),
		RouteBase:            "mcp",
		Slug:                 "test",
		ToolsetID:            uuid.NullUUID{},
		UpstreamResource:     "",
		UserSessionIssuerID:  uuid.New(),
	}
	subject := urn.NewUserSubject("user-id")
	payload := userSessionRefreshReplayPayload{
		AccessExpiresAt:        expiresAt,
		AudienceURN:            endpoint.AudienceURN,
		AuthorizationExpiresAt: expiresAt,
		ClientID:               clientID,
		EndpointIssuer:         "https://gram.example/mcp/test",
		ErrorDescription:       "",
		FailureReason:          "",
		JTI:                    strings.Repeat("a", 43),
		ReplayKey:              "userSessionRefreshReplay:expected",
		Response: tokenResponse{
			AccessToken:            "access-token",
			TokenType:              "Bearer",
			ExpiresIn:              3600,
			RefreshToken:           "refresh-token",
			AuthorizationExpiresIn: 3600,
		},
		Subject: &subject,
	}
	plaintext, err := json.Marshal(payload)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	service := new(Service)
	service.enc = enc
	service.metrics = &mcpmetrics.Metrics{}
	service.userSessionSigner = sessiontokens.NewSigner("test-jwt-secret")
	return service, endpoint, &clientRow, userSessionRefreshReplay{
		Key:        payload.ReplayKey,
		Ciphertext: ciphertext,
	}
}
