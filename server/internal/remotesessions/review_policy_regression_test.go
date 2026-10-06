package remotesessions

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestReviewPreparationScopeGrammar(t *testing.T) {
	t.Parallel()
	// Exhaust every single-byte token, including control and non-ASCII bytes.
	for c := 0; c <= 255; c++ {
		t.Run(fmt.Sprintf("byte_%02x", c), func(t *testing.T) {
			t.Parallel()
			in := PreparationInput{Resource: "https://resource.example/mcp", UserSessionIssuerID: uuid.New(), RemoteSessionIssuerID: uuid.New(), Scopes: []string{string([]byte{byte(c)})}}
			_, err := normalizePreparationInput(in)
			valid := c == 0x21 || (c >= 0x23 && c <= 0x5b) || (c >= 0x5d && c <= 0x7e)
			require.Equal(t, valid, err == nil)
		})
	}
	for _, scope := range []string{"", "réad", "read\x00write", "read\vwrite", "read\fwrite", "read\x7fwrite"} {
		in := PreparationInput{Resource: "https://resource.example/mcp", UserSessionIssuerID: uuid.New(), RemoteSessionIssuerID: uuid.New(), Scopes: []string{scope}}
		_, err := normalizePreparationInput(in)
		require.Error(t, err, "%q", scope)
	}
}

func TestReviewPreparationManualSetupStage(t *testing.T) {
	t.Parallel()
	result := preparationDiagnostic(PreparationStateManualSetupRequired)
	require.Equal(t, PreparationStageRegistration, result.Stage)
	require.NotEmpty(t, result.Remediation)
	require.False(t, result.Retryable)
}

func TestReviewPreparationDCRScopePresence(t *testing.T) {
	t.Parallel()
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)
	service := &Service{policy: policy}
	for _, tc := range []struct{ name, field, want string }{
		{"omitted", "", PreparationStateReady},
		{"null", `,"scope":null`, PreparationStateIndeterminate},
		{"empty", `,"scope":""`, PreparationStateReady},
		{"narrowed", `,"scope":"read"`, PreparationStateReady},
		{"unchanged", `,"scope":"read write"`, PreparationStateReady},
		{"broadened", `,"scope":"admin"`, PreparationStateIndeterminate},
		{"tab", `,"scope":"read\twrite"`, PreparationStateIndeterminate},
		{"double space", `,"scope":"read  write"`, PreparationStateIndeterminate},
		{"leading space", `,"scope":" read"`, PreparationStateIndeterminate},
		{"trailing space", `,"scope":"read "`, PreparationStateIndeterminate},
		{"non ASCII", `,"scope":"réad"`, PreparationStateIndeterminate},
		{"wrong type", `,"scope":[]`, PreparationStateIndeterminate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprintf(w, `{"client_id":"test-client","client_secret":"test-secret","grant_types":["%s"]%s}`, oauthwire.GrantTypeJWTBearer, tc.field)
			}))
			t.Cleanup(server.Close)
			response, state := service.submitPreparationDCR(t.Context(), PreparationInput{Scopes: []string{"read", "write"}}, server.URL, oauthwire.AuthMethodClientSecretBasic)
			require.Equal(t, tc.want, state)
			if tc.name == "omitted" {
				require.Nil(t, response.Scope)
			}
			if tc.name == "empty" {
				require.NotNil(t, response.Scope)
				require.Empty(t, *response.Scope)
			}
		})
	}
}
