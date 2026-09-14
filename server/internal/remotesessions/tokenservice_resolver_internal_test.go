package remotesessions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRemoteSessionCallerPrincipal(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, id.String())
	auth := &contextvalues.AuthContext{UserID: "credential-authorizer", APIKeyID: uuid.NewString()}
	keyContext := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), auth, agent, contextvalues.PrincipalCredential{AuthorizerUserID: "credential-authorizer"})
	sessionContext := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{}, agent)
	cases := []struct {
		name     string
		ctx      func() context.Context
		subject  urn.SessionSubject
		attached bool
		invalid  bool
	}{
		{"direct user", func() context.Context { return t.Context() }, urn.NewUserSubject("user-subject"), false, false},
		{"agent key retains caller", func() context.Context { return keyContext }, urn.NewAPIKeySubject(uuid.MustParse(auth.APIKeyID)), true, false},
		{"agent key never uses authorizer", func() context.Context { return keyContext }, urn.NewUserSubject(auth.UserID), true, false},
		{"agent session", func() context.Context { return sessionContext }, urn.NewAgentSubject(id), true, false},
		{"explicit agent session subject", func() context.Context { return t.Context() }, urn.NewAgentSubject(id), true, false},
		{"mismatched agent", func() context.Context { return keyContext }, urn.NewAgentSubject(uuid.New()), true, true},
		{"invalid agent", func() context.Context { return t.Context() }, urn.SessionSubject{Kind: urn.SessionSubjectKindAgent, ID: "invalid"}, true, true},
		{"user actor cannot impersonate agent", func() context.Context {
			return contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{}, urn.NewPrincipal(urn.PrincipalTypeUser, "user-subject"))
		}, urn.NewAgentSubject(id), true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			principal, attached, err := remoteSessionCallerPrincipal(tc.ctx(), tc.subject)
			require.Equal(t, tc.attached, attached)
			if tc.invalid {
				require.ErrorIs(t, err, ErrInvalidAuthorizationRequest)
				return
			}
			require.NoError(t, err)
			if attached {
				require.Equal(t, id, principal)
			} else {
				require.Equal(t, uuid.Nil, principal)
			}
		})
	}
	actor, ok := contextvalues.AuthenticatedActor(keyContext)
	require.True(t, ok)
	require.Equal(t, agent, actor)
}

func TestResolveAccessToken_AgentNeedsScopedAttachment(t *testing.T) {
	t.Parallel()
	// A nil database proves the client-only primitive cannot fall back to a
	// direct credential lookup for an agent.
	manager := &ChallengeManager{}
	token, err := manager.ResolveAccessToken(t.Context(), uuid.New(), urn.NewAgentSubject(uuid.New()), "")
	require.NoError(t, err)
	require.Empty(t, token)
}
