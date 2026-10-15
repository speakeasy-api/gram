package mockworkos_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"
	workosmode "github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"
	"github.com/speakeasy-api/gram/plog"
)

func TestSessionsAreListedWithTheirCreationTimeUntilRevoked(t *testing.T) {
	t.Parallel()

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	emulator := mockworkos.NewHandler(plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
	surface, err := workosmode.NewHandler(workosmode.Config{
		Backend:      workosmode.BackendLocal,
		ClientSecret: "local-test-key",
		UpstreamURL:  "",
		APIKey:       "",
	}, emulator.Handler(), nil, plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle(workosmode.Prefix+"/", http.StripPrefix(workosmode.Prefix, surface.Handler()))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := usermanagement.NewClient("local-test-key")
	client.Endpoint = server.URL + workosmode.Prefix
	client.HTTPClient = server.Client()

	challenge, err := client.CreateMagicAuth(t.Context(), usermanagement.CreateMagicAuthOpts{Email: "sessions@example.com", InvitationToken: ""})
	require.NoError(t, err)
	before := time.Now().Add(-time.Second)
	result, err := client.AuthenticateWithMagicAuth(t.Context(), usermanagement.AuthenticateWithMagicAuthOpts{
		ClientID:              "gram-local-dev",
		Email:                 challenge.Email,
		Code:                  challenge.Code,
		LinkAuthorizationCode: "",
		IPAddress:             "",
		UserAgent:             "",
	})
	require.NoError(t, err)
	sessionID := sessionIDFromAccessToken(t, result.AccessToken)

	listed, err := client.ListSessions(t.Context(), result.User.ID, usermanagement.ListSessionsOpts{Limit: 10, Before: "", After: "", Order: usermanagement.Desc})
	require.NoError(t, err)
	require.Len(t, listed.Data, 1)
	require.Equal(t, sessionID, listed.Data[0].ID)
	created, err := time.Parse(time.RFC3339Nano, listed.Data[0].CreatedAt)
	require.NoError(t, err)
	require.True(t, created.After(before), "created at %s", created)

	err = client.RevokeSession(t.Context(), usermanagement.RevokeSessionOpts{SessionID: sessionID})
	require.NoError(t, err)
	listed, err = client.ListSessions(t.Context(), result.User.ID, usermanagement.ListSessionsOpts{Limit: 10, Before: "", After: "", Order: usermanagement.Desc})
	require.NoError(t, err)
	require.Empty(t, listed.Data, "a revoked session is no longer listed")
}

func sessionIDFromAccessToken(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims struct {
		SID string `json:"sid"`
	}
	require.NoError(t, json.Unmarshal(payload, &claims))
	require.NotEmpty(t, claims.SID)
	return claims.SID
}
