package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type audienceNeverRevoked struct{}

func (audienceNeverRevoked) IsTokenRevoked(context.Context, string) (bool, error) {
	return false, nil
}

func TestValidateUserSessionBearerAudiencesResourceBindingAndIssuerPortability(t *testing.T) {
	t.Parallel()

	const (
		resourceA       = "https://gram.example.test/mcp/a"
		resourceB       = "https://gram.example.test/mcp/b"
		currentAudience = "user_session_issuer:11111111-1111-1111-1111-111111111111"
	)
	signer := sessiontokens.NewSigner("audience-policy-test-secret")
	subject := urn.NewUserSubject("user-1")
	audiencesA := userSessionBearerAudiences{Resource: resourceA, Current: currentAudience}
	audiencesB := userSessionBearerAudiences{Resource: resourceB, Current: currentAudience}

	resourceToken, _, err := signer.Mint(sessiontokens.MintParams{
		Subject: subject, Audience: resourceA, Issuer: resourceA, Lifetime: time.Hour,
	})
	require.NoError(t, err)
	_, accepted, err := validateUserSessionBearerAudiences(t.Context(), signer, audienceNeverRevoked{}, resourceToken, audiencesA)
	require.NoError(t, err)
	require.Equal(t, userSessionAudienceResource, accepted)
	_, _, err = validateUserSessionBearerAudiences(t.Context(), signer, audienceNeverRevoked{}, resourceToken, audiencesB)
	require.ErrorIs(t, err, jwt.ErrTokenInvalidAudience)

	legacyToken, _, err := signer.Mint(sessiontokens.MintParams{
		Subject: subject, Audience: currentAudience, Issuer: resourceA, Lifetime: time.Hour,
	})
	require.NoError(t, err)
	for _, audiences := range []userSessionBearerAudiences{audiencesA, audiencesB} {
		_, accepted, err = validateUserSessionBearerAudiences(t.Context(), signer, audienceNeverRevoked{}, legacyToken, audiences)
		require.NoError(t, err)
		require.Equal(t, userSessionAudienceCurrent, accepted)
	}
}

func TestValidateUserSessionBearerAudiencesRejectsToolsetAudience(t *testing.T) {
	t.Parallel()

	const (
		resource        = "https://gram.example.test/mcp/a"
		currentAudience = "user_session_issuer:11111111-1111-1111-1111-111111111111"
	)
	signer := sessiontokens.NewSigner("audience-policy-test-secret")
	toolsetAudience := urn.NewToolset(uuid.MustParse("22222222-2222-2222-2222-222222222222")).String()
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject: urn.NewUserSubject("user-1"), Audience: toolsetAudience, Issuer: resource, Lifetime: time.Hour,
	})
	require.NoError(t, err)

	_, _, err = validateUserSessionBearerAudiences(t.Context(), signer, audienceNeverRevoked{}, token, userSessionBearerAudiences{
		Resource:         resource,
		Current:          currentAudience,
		IssuerMCPServers: urn.NewUserSessionIssuerMCPServers(uuid.MustParse("11111111-1111-1111-1111-111111111111")).String(),
	})
	require.ErrorIs(t, err, jwt.ErrTokenInvalidAudience)
}

func TestValidateUserSessionBearerAudiencesRejectsBroadenedResourceToken(t *testing.T) {
	t.Parallel()

	const (
		resource        = "https://gram.example.test/mcp/a"
		currentAudience = "user_session_issuer:11111111-1111-1111-1111-111111111111"
	)
	signer := sessiontokens.NewSigner("audience-policy-test-secret")
	claims := sessiontokens.SessionClaims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   urn.NewUserSubject("user-1").String(),
		Audience:  jwt.ClaimStrings{resource, currentAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		ID:        "multi-audience-jti",
	}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("audience-policy-test-secret"))
	require.NoError(t, err)

	_, _, err = validateUserSessionBearerAudiences(t.Context(), signer, audienceNeverRevoked{}, token, userSessionBearerAudiences{
		Resource: resource,
		Current:  currentAudience,
	})
	require.ErrorContains(t, err, "audience must exactly match")
}
