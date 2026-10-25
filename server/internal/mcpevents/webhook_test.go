package mcpevents

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWebhookVerificationRequiresSignedEchoAndRejectsRedirects(t *testing.T) {
	t.Parallel()
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	signer, err := standardwebhooks.NewWebhook(secret)
	require.NoError(t, err)
	for _, behavior := range []string{"echo", "wrong", "redirect", "large"} {
		t.Run(behavior, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil || signer.Verify(body, r.Header) != nil {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Header.Get("X-MCP-Subscription-Id") != "sub_test" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var challenge struct {
					Challenge string `json:"challenge"`
				}
				if json.Unmarshal(body, &challenge) != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				switch behavior {
				case "echo":
					_ = json.NewEncoder(w).Encode(challenge)
				case "wrong":
					_, _ = io.WriteString(w, `{"challenge":"wrong"}`)
				case "redirect":
					http.Redirect(w, r, "https://127.0.0.1/should-not-follow", http.StatusTemporaryRedirect)
				case "large":
					_, _ = io.WriteString(w, strings.Repeat("x", maxCallbackBytes+1))
				}
			}))
			t.Cleanup(server.Close)
			pool := x509.NewCertPool()
			pool.AddCert(server.Certificate())
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithTLSRootCAs(pool))
			require.NoError(t, err)
			err = NewWebhook(policy).Verify(t.Context(), "sub_test", Destination{Mode: "webhook", URL: server.URL, Secret: secret}, time.Now())
			switch behavior {
			case "echo":
				require.NoError(t, err)
			case "redirect":
				var failure *CallbackError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, "challenge_failed", failure.Reason)
				require.Equal(t, int32(1), calls.Load())
			default:
				require.Error(t, err)
			}
		})
	}
}

func TestWebhookBlocksPrivateEndpointsAndInvalidSecrets(t *testing.T) {
	t.Parallel()
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
	for _, address := range []string{"https://127.0.0.1/", "https://[::1]/", "https://169.254.169.254/"} {
		err := NewWebhook(guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))).Verify(t.Context(), "sub_private", Destination{Mode: "webhook", URL: address, Secret: secret}, time.Now())
		require.Error(t, err)
		require.NotContains(t, err.Error(), address)
	}
	for _, invalid := range []string{"", "whsec_not-base64", "whsec_" + base64.StdEncoding.EncodeToString([]byte("short")), "whsec_" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 65)))} {
		require.Error(t, ValidateDestination(Destination{Mode: "webhook", URL: "https://receiver.example/callback", Secret: invalid}, true))
	}
}

func TestSubscriptionIdentityAndArgumentsAreStrict(t *testing.T) {
	t.Parallel()
	owner := Owner{OrganizationID: "org-test", UserID: "user-test", ConnectionID: uuid.New(), Generation: uuid.New(), ClientID: "client-test"}
	projectID := uuid.New()
	input := SubscriptionInput{Name: ReviewDecided, Arguments: Arguments{ProjectID: projectID}, Delivery: Destination{Mode: "webhook", URL: "https://receiver.example/callback", Secret: "first"}}
	id := SubscriptionID(owner, input)
	input.Delivery.Secret = "second"
	input.TTLMs = new(int64(1000))
	require.Equal(t, id, SubscriptionID(owner, input))
	owner.Generation = uuid.New()
	require.NotEqual(t, id, SubscriptionID(owner, input))
	var arguments Arguments
	require.Error(t, json.Unmarshal([]byte(`{"project_id":"`+projectID.String()+`","ignored_filter":"private"}`), &arguments))
	require.NoError(t, json.Unmarshal([]byte(`{"project_id":"`+strings.ToUpper(projectID.String())+`"}`), &arguments))
	require.Equal(t, projectID, arguments.ProjectID)
}
