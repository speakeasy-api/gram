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
		{name: "agent", subject: urn.NewAgentSubject(uuid.MustParse("22222222-2222-2222-2222-222222222222")), want: mcpidentity.KindAgent},
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

// No provenance kind describes a workload session, so the request stays
// unattributed.
func TestValidatorBoundaryLeavesWorkloadSessionUnattributed(t *testing.T) {
	t.Parallel()

	subject := urn.NewWorkloadSubject(uuid.MustParse("33333333-3333-3333-3333-333333333333"), "repo:acme/payments-api:ref:refs/heads/main")
	_, ok := mcpidentity.FromContext(mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), validatedSession(t, subject)))
	require.False(t, ok)
}

// Provenance already on the context belongs to another credential, so a
// workload session must not inherit it.
func TestValidatorBoundaryWorkloadSessionClearsEarlierProvenance(t *testing.T) {
	t.Parallel()

	boundary := mcpidentity.NewValidatorBoundary()
	stamped := boundary.StampValidatedSession(t.Context(), validatedSession(t, urn.NewUserSubject("user_01J8EXAMPLE")))
	_, ok := mcpidentity.FromContext(stamped)
	require.True(t, ok, "the earlier session must be stamped, or the test proves nothing")

	subject := urn.NewWorkloadSubject(uuid.MustParse("33333333-3333-3333-3333-333333333333"), "repo:acme/payments-api:ref:refs/heads/main")
	identity, ok := mcpidentity.FromContext(boundary.StampValidatedSession(stamped, validatedSession(t, subject)))
	require.False(t, ok, "a workload session must not carry an earlier credential's provenance")
	require.Empty(t, identity.UserID())
}

// A zero boundary cannot stamp provenance, so it cannot clear it either.
func TestZeroValidatorBoundaryDoesNotClearProvenanceForWorkload(t *testing.T) {
	t.Parallel()

	stamped := mcpidentity.NewValidatorBoundary().StampAPIKey(t.Context())

	var inert mcpidentity.ValidatorBoundary
	subject := urn.NewWorkloadSubject(uuid.MustParse("33333333-3333-3333-3333-333333333333"), "repo:acme/payments-api:ref:refs/heads/main")
	identity, ok := mcpidentity.FromContext(inert.StampValidatedSession(stamped, validatedSession(t, subject)))
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAPIKey, identity.Kind())
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
