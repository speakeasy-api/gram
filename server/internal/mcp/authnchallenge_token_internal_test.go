package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestWriteRefreshTokenReplayRejectsDifferentCacheKey(t *testing.T) {
	t.Parallel()

	service, endpoint, clientRow, replay := newRefreshTokenReplayTestFixture(t, time.Now().Add(time.Hour))
	w := httptest.NewRecorder()
	err := service.writeRefreshTokenReplay(
		t.Context(),
		w,
		endpoint,
		clientRow,
		"https://gram.example",
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
	err := service.writeRefreshTokenReplay(
		t.Context(),
		w,
		endpoint,
		clientRow,
		"https://gram.example",
		"userSessionRefreshReplay:expected",
		replay,
		testenv.NewLogger(t),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"invalid_grant","error_description":"refresh_token has expired"}`, w.Body.String())
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
	payload := userSessionRefreshReplayPayload{
		AccessExpiresAt:        expiresAt,
		AudienceURN:            endpoint.AudienceURN,
		AuthorizationExpiresAt: expiresAt,
		ClientID:               clientID,
		EndpointIssuer:         "https://gram.example/mcp/test",
		ErrorDescription:       "",
		JTI:                    strings.Repeat("a", 43),
		ReplayKey:              "userSessionRefreshReplay:expected",
		Response: tokenResponse{
			AccessToken:            "access-token",
			TokenType:              "Bearer",
			ExpiresIn:              3600,
			RefreshToken:           "refresh-token",
			AuthorizationExpiresIn: 3600,
		},
		Subject: urn.NewUserSubject("user-id"),
	}
	plaintext, err := json.Marshal(payload)
	require.NoError(t, err)
	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	service := new(Service)
	service.enc = enc
	service.userSessionSigner = sessiontokens.NewSigner("test-jwt-secret")
	return service, endpoint, &clientRow, userSessionRefreshReplay{
		Key:        payload.ReplayKey,
		Ciphertext: ciphertext,
	}
}
