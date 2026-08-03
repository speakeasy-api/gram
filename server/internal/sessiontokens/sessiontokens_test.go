package sessiontokens_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type neverRevoked struct{}

func (neverRevoked) IsTokenRevoked(context.Context, string) (bool, error) { return false, nil }

type revoked struct{}

func (revoked) IsTokenRevoked(context.Context, string) (bool, error) { return true, nil }

type unavailableRevocationStore struct{}

func (unavailableRevocationStore) IsTokenRevoked(context.Context, string) (bool, error) {
	return false, errors.New("unavailable")
}

func TestSigner_MintAndValidateBearer(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	subject := urn.NewUserSubject("user-1")
	token, jti, err := signer.Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: "admin-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
		ClientID: "client-abc",
	})
	require.NoError(t, err)

	session, err := signer.ValidateBearer(t.Context(), token, "admin-mcp", neverRevoked{})
	require.NoError(t, err)
	require.Equal(t, subject, session.Subject)
	require.Equal(t, jti, session.JTI)
	require.Equal(t, "client-abc", session.ClientID)
}

func TestSigner_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: "hosted-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
	})
	require.NoError(t, err)

	_, err = signer.ValidateBearer(t.Context(), token, "admin-mcp", neverRevoked{})
	require.Error(t, err)
}

func TestSigner_FailsClosedOnRevocation(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: "admin-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
	})
	require.NoError(t, err)

	_, err = signer.ValidateBearer(t.Context(), token, "admin-mcp", revoked{})
	require.ErrorContains(t, err, "token is revoked")

	_, err = signer.ValidateBearer(t.Context(), token, "admin-mcp", unavailableRevocationStore{})
	require.ErrorContains(t, err, "check revocation")
}
