package assistants

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newMCPAuthTestService(t *testing.T, conn *pgxpool.Pool) *Service {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, nil)
	require.NoError(t, err)
	core := NewServiceCore(
		logger,
		tracerProvider,
		meterProvider,
		conn,
		guardianPolicy,
		testenv.NewEncryptionClient(t),
		testRuntimeBackend{backend: runtimeBackendFlyIO},
		nil,
		nil,
		nil,
		telemetry.NewStub(logger),
		nil,
		newTestAuditLogger(),
	)
	return &Service{
		tracer:           tracerProvider.Tracer("test"),
		logger:           logger,
		auth:             nil,
		authz:            nil,
		core:             core,
		signaler:         nil,
		bootstrapLimiter: nil,
	}
}

func TestGetOrRegisterMCPAuthClientReusesRegistration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_client_reuse")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	requests := make(chan mcpAuthClientRegistrationRequest, 2)
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registrations.Add(1)
		var request mcpAuthClientRegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "stable-client",
			ClientSecret:          "stable-secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newMCPAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	first, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI,
	)
	require.NoError(t, err)
	second, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI,
	)
	require.NoError(t, err)

	require.Equal(t, int32(1), registrations.Load())
	require.Equal(t, first, second)
	require.Equal(t, "stable-client", first.ClientID)
	secret, err := service.core.encryptionClient.Decrypt(first.ClientSecretEncrypted)
	require.NoError(t, err)
	require.Equal(t, "stable-secret", secret)
	request := <-requests
	require.Equal(t, "Gram Assistant "+assistantID.String(), request.ClientName)
	require.Equal(t, []string{redirectURI}, request.RedirectURIs)
}

func TestInvalidateMCPAuthClientForcesRegistration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_client_invalidation")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrationNumber := registrations.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "client-" + fmt.Sprint(registrationNumber),
			ClientSecret:          "secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newMCPAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	first, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI,
	)
	require.NoError(t, err)
	service.invalidateMCPAuthClient(t.Context(), projectID, assistantID, registrationServer.URL, first.ClientID)
	second, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI,
	)
	require.NoError(t, err)

	require.Equal(t, int32(2), registrations.Load())
	require.NotEqual(t, first.ClientID, second.ClientID)
}

func TestGetOrRegisterMCPAuthClientRejectsSoonExpiringRegistration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_client_expiry")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrationNumber := registrations.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "client-" + fmt.Sprint(registrationNumber),
			ClientSecret:          "secret",
			ClientSecretExpiresAt: time.Now().Add(5 * time.Minute).Unix(),
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newMCPAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	_, err = service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI,
	)
	require.ErrorContains(t, err, "client secret expires before the authorization flow")
	require.Equal(t, int32(1), registrations.Load())
}

func TestGetOrRegisterMCPAuthClientSerializesFirstRegistration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_client_concurrent")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	registrationStarted := make(chan struct{}, 1)
	finishRegistration := make(chan struct{})
	var finishRegistrationOnce sync.Once
	finish := func() { finishRegistrationOnce.Do(func() { close(finishRegistration) }) }
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrations.Add(1)
		registrationStarted <- struct{}{}
		<-finishRegistration
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "concurrent-client",
			ClientSecret:          "concurrent-secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)
	t.Cleanup(finish)

	service := newMCPAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	results := make(chan mcpAuthClientCredentials, 2)
	errs := make(chan error, 2)
	var completed atomic.Int32
	register := func() {
		result, err := service.getOrRegisterMCPAuthClient(
			t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI,
		)
		results <- result
		errs <- err
		completed.Add(1)
	}

	go register()
	require.Eventually(t, func() bool { return len(registrationStarted) > 0 }, 30*time.Second, 25*time.Millisecond,
		"first dynamic registration did not start")
	<-registrationStarted
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		register()
	}()
	<-secondStarted
	require.Never(t, func() bool { return registrations.Load() > 1 }, 500*time.Millisecond, 25*time.Millisecond,
		"a second dynamic registration started while the first claim was active")
	finish()
	require.Eventually(t, func() bool { return completed.Load() == 2 }, 30*time.Second, 25*time.Millisecond,
		"both registration callers did not complete")

	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	require.Equal(t, int32(1), registrations.Load())
	first := <-results
	second := <-results
	require.Equal(t, first, second)
}
