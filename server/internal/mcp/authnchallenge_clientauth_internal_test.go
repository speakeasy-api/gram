package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/privatekeyjwt"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Without Redis there is no shared replay store, and without that there is no
// single-use guarantee, so the verifier is absent and an assertion client is
// refused with a label that says why. This is the worker's configuration in
// production.
func TestVerifyClientAssertion_NoVerifierRefuses(t *testing.T) {
	t.Parallel()

	require.Nil(t, newClientAssertionVerifier(nil, nil, testenv.NewMeterProvider(t), testenv.NewLogger(t)))

	svc := &Service{clientAssertionVerifier: nil}
	row := &usersessions_repo.UserSessionClient{
		ClientID:                "https://client.example.com/oauth/client.json",
		TokenEndpointAuthMethod: pgtype.Text{String: "private_key_jwt", Valid: true},
		ClientJwks:              []byte(`{"keys":[]}`),
	}
	urls := AuthorizationServerURLs{Issuer: "https://gram.example.com/mcp/example", Token: "https://gram.example.com/mcp/example/token"}
	reason := svc.verifyClientAssertion(t.Context(), testenv.NewLogger(t), uuid.New(), urls, clientAssertionAtToken, row, privatekeyjwt.Assertion{Value: "x", Type: privatekeyjwt.AssertionType})
	require.Equal(t, "assertion_verifier_unavailable", reason)
}

func TestAuthenticateOAuthClientRejectsObsoleteRuntimeClients(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		row      usersessions_repo.UserSessionClient
		rejected bool
	}{
		{name: "ordinary", row: usersessions_repo.UserSessionClient{ClientID: "ordinary-client"}},
		{name: "runtime binding", row: usersessions_repo.UserSessionClient{AssistantRuntimeID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}, rejected: true},
		{name: "trigger binding", row: usersessions_repo.UserSessionClient{AssistantTriggerWorkloadBindingID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}, rejected: true},
		{name: "retired binding with cleared markers", row: usersessions_repo.UserSessionClient{ClientID: "assistant-runtime-" + uuid.NewString()}, rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, endpoint := range []clientAssertionEndpoint{clientAssertionAtToken, clientAssertionAtRevoke} {
				reason := new(Service).authenticateOAuthClient(t.Context(), testenv.NewLogger(t), uuid.New(), AuthorizationServerURLs{}, endpoint, &tc.row, presentedClientCredentials{method: oauthwire.AuthMethodNone})
				if tc.rejected {
					require.Equal(t, "obsolete_runtime_client", reason)
				} else {
					require.Empty(t, reason)
				}
			}
		})
	}
}
