package sessiontokens_test

import (
	"context"
	"errors"
	"strings"
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
		Audience: "platform-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
		ClientID: "client-abc",
	})
	require.NoError(t, err)

	session, err := signer.ValidateBearer(t.Context(), token, "platform-mcp", neverRevoked{})
	require.NoError(t, err)
	require.Equal(t, subject, session.Subject)
	require.Equal(t, jti, session.JTI)
	require.Equal(t, "client-abc", session.ClientID)
}

func TestSigner_ExactExpirationOverridesLifetime(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	expiresAt := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:   urn.NewUserSubject("user-1"),
		Audience:  "platform-mcp",
		Issuer:    "https://example.test",
		Lifetime:  time.Hour,
		ExpiresAt: &expiresAt,
	})
	require.NoError(t, err)

	claims, err := signer.Validate(token, "platform-mcp")
	require.NoError(t, err)
	require.Equal(t, expiresAt, claims.ExpiresAt.Time)
}

func TestSigner_UsesProvidedJTI(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	jti := strings.Repeat("a", 43)
	token, mintedJTI, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: "platform-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
		ClientID: "client-abc",
		JTI:      jti,
	})
	require.NoError(t, err)
	require.Equal(t, jti, mintedJTI)

	session, err := signer.ValidateBearer(t.Context(), token, "platform-mcp", neverRevoked{})
	require.NoError(t, err)
	require.Equal(t, jti, session.JTI)
}

func TestSigner_RejectsInvalidProvidedJTI(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	_, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: "platform-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
		JTI:      "stable-jti",
	})
	require.ErrorContains(t, err, "supplied jti is invalid")
}

func TestSigner_GeneratesDistinctJTIs(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	params := sessiontokens.MintParams{Subject: urn.NewUserSubject("user-1"), Audience: "platform-mcp", Issuer: "https://example.test", Lifetime: time.Hour}
	_, firstJTI, err := signer.Mint(params)
	require.NoError(t, err)
	_, secondJTI, err := signer.Mint(params)
	require.NoError(t, err)
	require.NotEqual(t, firstJTI, secondJTI)
}

func TestSigner_VerifiedJTI(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	token, jti, err := signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject("user-1"), Audience: "platform-mcp", Issuer: "https://example.test", Lifetime: time.Hour})
	require.NoError(t, err)

	verifiedJTI, err := signer.VerifiedJTI(token)
	require.NoError(t, err)
	require.Equal(t, jti, verifiedJTI)
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

	_, err = signer.ValidateBearer(t.Context(), token, "platform-mcp", neverRevoked{})
	require.Error(t, err)
}

func TestSigner_RejectsUnexpectedAlgorithmAndMissingRequiredClaims(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	claims := sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: urn.NewUserSubject("user-1").String(), Audience: jwt.ClaimStrings{"platform-mcp"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)), ID: "jti-1"}}
	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodHS512, claims)
	wrongAlgorithmToken, err := wrongAlgorithm.SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	_, err = signer.ValidateBearer(t.Context(), wrongAlgorithmToken, "platform-mcp", neverRevoked{})
	require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)

	missingExpiry := jwt.NewWithClaims(jwt.SigningMethodHS256, sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: urn.NewUserSubject("user-1").String(), Audience: jwt.ClaimStrings{"platform-mcp"}, ID: "jti-2"}})
	missingExpiryToken, err := missingExpiry.SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	_, err = signer.ValidateBearer(t.Context(), missingExpiryToken, "platform-mcp", neverRevoked{})
	require.ErrorIs(t, err, jwt.ErrTokenRequiredClaimMissing)

	missingJTI := jwt.NewWithClaims(jwt.SigningMethodHS256, sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: urn.NewUserSubject("user-1").String(), Audience: jwt.ClaimStrings{"platform-mcp"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
	missingJTIToken, err := missingJTI.SignedString([]byte("test-jwt-secret"))
	require.NoError(t, err)
	_, err = signer.ValidateBearer(t.Context(), missingJTIToken, "platform-mcp", neverRevoked{})
	require.ErrorContains(t, err, "missing jti claim")
}

func TestSigner_FailsClosedOnRevocation(t *testing.T) {
	t.Parallel()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject:  urn.NewUserSubject("user-1"),
		Audience: "platform-mcp",
		Issuer:   "https://example.test",
		Lifetime: time.Hour,
	})
	require.NoError(t, err)

	_, err = signer.ValidateBearer(t.Context(), token, "platform-mcp", revoked{})
	require.ErrorContains(t, err, "token is revoked")

	_, err = signer.ValidateBearer(t.Context(), token, "platform-mcp", unavailableRevocationStore{})
	require.ErrorContains(t, err, "check revocation")
}
