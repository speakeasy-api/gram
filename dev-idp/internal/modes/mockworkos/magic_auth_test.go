package mockworkos_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/usermanagement"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"
	workosmode "github.com/speakeasy-api/gram/dev-idp/internal/modes/workos"
	"github.com/speakeasy-api/gram/plog"
)

func TestMagicAuthWorksThroughWorkOSSDK(t *testing.T) {
	t.Parallel()

	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	emulator := mockworkos.NewHandler(mockworkos.Config{}, plog.NewLogger(io.Discard), tracenoop.NewTracerProvider(), db)
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

	challenge, err := client.CreateMagicAuth(t.Context(), usermanagement.CreateMagicAuthOpts{
		Email:           "invitee@example.com",
		InvitationToken: "",
	})
	require.NoError(t, err)
	require.Equal(t, "invitee@example.com", challenge.Email)
	require.NotEmpty(t, challenge.Code)
	require.NotEmpty(t, challenge.UserId)

	result, err := client.AuthenticateWithMagicAuth(t.Context(), usermanagement.AuthenticateWithMagicAuthOpts{
		ClientID:              "gram-local-dev",
		Email:                 challenge.Email,
		Code:                  challenge.Code,
		LinkAuthorizationCode: "",
		IPAddress:             "",
		UserAgent:             "",
	})
	require.NoError(t, err)
	require.Equal(t, challenge.UserId, result.User.ID)
	require.Equal(t, challenge.Email, result.User.Email)
	require.True(t, result.User.EmailVerified)
	require.NotEmpty(t, result.AccessToken)
	require.NotEmpty(t, result.RefreshToken)

	_, err = client.AuthenticateWithMagicAuth(t.Context(), usermanagement.AuthenticateWithMagicAuthOpts{
		ClientID:              "gram-local-dev",
		Email:                 challenge.Email,
		Code:                  challenge.Code,
		LinkAuthorizationCode: "",
		IPAddress:             "",
		UserAgent:             "",
	})
	require.Error(t, err, "Magic Auth codes must be single-use")

	concurrentChallenge, err := client.CreateMagicAuth(t.Context(), usermanagement.CreateMagicAuthOpts{
		Email:           "concurrent@example.com",
		InvitationToken: "",
	})
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, authErr := client.AuthenticateWithMagicAuth(t.Context(), usermanagement.AuthenticateWithMagicAuthOpts{
				ClientID:              "gram-local-dev",
				Email:                 concurrentChallenge.Email,
				Code:                  concurrentChallenge.Code,
				LinkAuthorizationCode: "",
				IPAddress:             "",
				UserAgent:             "",
			})
			errs <- authErr
		}()
	}
	close(start)

	successes := 0
	for range 2 {
		if <-errs == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes, "concurrent redemption must succeed exactly once")
}
