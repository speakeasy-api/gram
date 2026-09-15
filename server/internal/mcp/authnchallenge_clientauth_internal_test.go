package mcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
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
	reason := svc.verifyClientAssertion(t.Context(), testenv.NewLogger(t), &ResolvedMcpEndpoint{}, clientAssertionAtToken, row, clientauth.Assertion{Value: "x", Type: clientauth.AssertionType}, "https://gram.example.com")
	require.Equal(t, "assertion_verifier_unavailable", reason)
}
