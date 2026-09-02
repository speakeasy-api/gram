package chatsessions

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat_sessions"
	authchatsessions "github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestCreateSignsOrdinarySessionProvenanceOnly(t *testing.T) {
	t.Parallel()

	const secret = "chat-session-signing-secret"
	logger := testenv.NewLogger(t)
	manager := authchatsessions.NewManager(logger, nil, secret)
	service := &Service{
		tracer:              testenv.NewTracerProvider(t).Tracer("test"),
		logger:              logger,
		chatSessionsManager: manager,
	}
	projectID := uuid.New()
	projectSlug := "project"
	sessionID := "session"
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID,
		ProjectID: &projectID, ProjectSlug: &projectSlug,
	}
	payload := &gen.CreatePayload{EmbedOrigin: "https://example.com", ExpiresAfter: 3600}

	decode := func(tokenString string) *authchatsessions.ChatSessionClaims {
		t.Helper()
		token, err := jwt.ParseWithClaims(tokenString, &authchatsessions.ChatSessionClaims{}, func(*jwt.Token) (any, error) {
			return []byte(secret), nil
		})
		require.NoError(t, err)
		claims, ok := token.Claims.(*authchatsessions.ChatSessionClaims)
		require.True(t, ok)
		return claims
	}

	ordinaryCtx := contextvalues.WithValidatedGramSession(t.Context(), authCtx, false)
	result, err := service.Create(ordinaryCtx, payload)
	require.NoError(t, err)
	claim := decode(result.ClientToken).GramSessionActingUser
	require.NotNil(t, claim)
	require.Equal(t, "org", claim.OrgID)
	require.Equal(t, "user", claim.UserID)
	require.Equal(t, "session", claim.SessionID)

	apiKeyCtx := *authCtx
	apiKeyCtx.APIKeyID = "key"
	result, err = service.Create(contextvalues.SetAuthContext(t.Context(), &apiKeyCtx), payload)
	require.NoError(t, err)
	require.Nil(t, decode(result.ClientToken).GramSessionActingUser)
}

func TestValidatedGramSessionClaimMinting(t *testing.T) {
	t.Parallel()

	sessionID := "session"
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID}
	ctx := contextvalues.WithValidatedGramSession(t.Context(), authCtx, false)

	claim := validatedGramSessionClaim(ctx, authCtx)
	require.NotNil(t, claim)
	require.Equal(t, "org", claim.OrgID)
	require.Equal(t, "user", claim.UserID)
	require.Equal(t, "session", claim.SessionID)

	for name, testCtx := range map[string]func() (*contextvalues.AuthContext, bool){
		"API key creator fields": func() (*contextvalues.AuthContext, bool) {
			return &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "owner", SessionID: &sessionID, APIKeyID: "key"}, false
		},
		"legacy impersonation": func() (*contextvalues.AuthContext, bool) {
			return authCtx, true
		},
		"shared demo": func() (*contextvalues.AuthContext, bool) {
			demo := *authCtx
			demo.ActiveOrganizationID = constants.DemoOrganizationID
			return &demo, false
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate, legacy := testCtx()
			candidateCtx := contextvalues.SetAuthContext(t.Context(), candidate)
			if candidate.APIKeyID == "" {
				candidateCtx = contextvalues.WithValidatedGramSession(t.Context(), candidate, legacy)
			}
			require.Nil(t, validatedGramSessionClaim(candidateCtx, candidate))
		})
	}
}

func TestValidatedGramSessionClaimRejectsPostValidationIdentityMutation(t *testing.T) {
	t.Parallel()

	sessionID := "session"
	authCtx := &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "user", SessionID: &sessionID}
	ctx := contextvalues.WithValidatedGramSession(t.Context(), authCtx, false)
	carried, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	carried.UserID = "substitute-owner"

	require.Nil(t, validatedGramSessionClaim(ctx, carried))
}
