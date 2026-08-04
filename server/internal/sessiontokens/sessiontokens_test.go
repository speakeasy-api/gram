package sessiontokens_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

func TestSigner_RejectsUnexpectedAlgorithmAndMissingRequiredClaims(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	claims := sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: urn.NewUserSubject("user-1").String(), Audience: jwt.ClaimStrings{"admin-mcp"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), ID: "jti-1"}}
	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	wrongAlgorithmToken, err := wrongAlgorithm.SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	_, err = signer.ValidateBearer(t.Context(), wrongAlgorithmToken, "admin-mcp", neverRevoked{})
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)

	missingExpiry := jwt.NewWithClaims(jwt.SigningMethodHS256, sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: urn.NewUserSubject("user-1").String(), Audience: jwt.ClaimStrings{"admin-mcp"}, ID: "jti-2"}})
	missingExpiryToken, err := missingExpiry.SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	_, err = signer.ValidateBearer(t.Context(), missingExpiryToken, "admin-mcp", neverRevoked{})
	require.ErrorIs(t, err, jwt.ErrTokenRequiredClaimMissing)

	missingJTI := jwt.NewWithClaims(jwt.SigningMethodHS256, sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: urn.NewUserSubject("user-1").String(), Audience: jwt.ClaimStrings{"admin-mcp"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
	missingJTIToken, err := missingJTI.SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	_, err = signer.ValidateBearer(t.Context(), missingJTIToken, "admin-mcp", neverRevoked{})
	require.ErrorContains(t, err, "missing jti claim")
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
