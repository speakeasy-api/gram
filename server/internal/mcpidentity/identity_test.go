package mcpidentity_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestFromContext_AbsentMeansUnattributed(t *testing.T) {
	t.Parallel()

	identity, ok := mcpidentity.FromContext(t.Context())
	require.False(t, ok)
	require.Empty(t, identity.Kind())
	require.Empty(t, identity.UserID())
}

type neverRevoked struct{}

func (neverRevoked) IsTokenRevoked(context.Context, string) (bool, error) { return false, nil }

func validatedSession(t *testing.T, subject urn.SessionSubject) sessiontokens.ValidatedSession {
	t.Helper()
	signer := sessiontokens.NewSigner("mcpidentity-test-secret")
	token, _, err := signer.Mint(sessiontokens.MintParams{
		Subject: subject, Audience: "mcpidentity-test", Issuer: "mcpidentity-test", Lifetime: time.Hour,
	})
	require.NoError(t, err)
	session, err := signer.ValidateBearer(t.Context(), token, "mcpidentity-test", neverRevoked{})
	require.NoError(t, err)
	return session
}

func TestValidatorBoundaryValidatedSessions(t *testing.T) {
	t.Parallel()

	boundary := mcpidentity.NewValidatorBoundary()
	tests := []struct {
		name    string
		subject urn.SessionSubject
		want    mcpidentity.Kind
		userID  string
	}{
		{name: "user", subject: urn.NewUserSubject("user_01J8EXAMPLE"), want: mcpidentity.KindUserSession, userID: "user_01J8EXAMPLE"},
		{name: "api key", subject: urn.NewAPIKeySubject(uuid.MustParse("11111111-1111-1111-1111-111111111111")), want: mcpidentity.KindAPIKey},
		{name: "anonymous", subject: urn.NewAnonymousSubject("session"), want: mcpidentity.KindAnonymous},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identity, ok := mcpidentity.FromContext(boundary.StampValidatedSession(t.Context(), validatedSession(t, test.subject)))
			require.True(t, ok)
			require.Equal(t, test.want, identity.Kind())
			require.Equal(t, test.userID, identity.UserID())
		})
	}
}

func TestValidatorBoundaryRejectsZeroValidatedSession(t *testing.T) {
	t.Parallel()

	var proof sessiontokens.ValidatedSession
	_, ok := mcpidentity.FromContext(mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), proof))
	require.False(t, ok)
}

func TestValidatorBoundaryNonUserStrategiesCannotCarryUser(t *testing.T) {
	t.Parallel()

	boundary := mcpidentity.NewValidatorBoundary()
	tests := []struct {
		want mcpidentity.Kind
		ctx  func() context.Context
	}{
		{want: mcpidentity.KindAssistant, ctx: func() context.Context { return boundary.StampAssistant(t.Context()) }},
		{want: mcpidentity.KindAPIKey, ctx: func() context.Context { return boundary.StampAPIKey(t.Context()) }},
		{want: mcpidentity.KindChatSession, ctx: func() context.Context { return boundary.StampChatSession(t.Context()) }},
	}
	for _, tt := range tests {
		identity, ok := mcpidentity.FromContext(tt.ctx())
		require.True(t, ok)
		require.Equal(t, tt.want, identity.Kind())
		require.Empty(t, identity.UserID())
	}
}

func TestZeroValidatorBoundaryIsInert(t *testing.T) {
	t.Parallel()

	var boundary mcpidentity.ValidatorBoundary
	_, ok := mcpidentity.FromContext(boundary.StampValidatedSession(t.Context(), validatedSession(t, urn.NewUserSubject("user_01J8EXAMPLE"))))
	require.False(t, ok)
}
