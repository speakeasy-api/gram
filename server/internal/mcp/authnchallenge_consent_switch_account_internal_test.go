package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestAccountSwitchAllowed(t *testing.T) {
	t.Parallel()

	allowed := func() AuthnChallengeState {
		subject := urn.NewUserSubject("user-1")
		signInID := uuid.New()
		no := false
		return AuthnChallengeState{Subject: &subject, AuthorizerUserID: "user-1", SignInID: &signInID, AuthorizerImpersonated: &no}
	}
	require.True(t, accountSwitchAllowed(allowed()))

	yes := true
	anonymous := urn.NewAnonymousSubject(uuid.NewString())
	other := urn.NewUserSubject("user-2")
	for name, edit := range map[string]func(*AuthnChallengeState){
		"dashboard connect":   func(s *AuthnChallengeState) { s.FirstParty = true },
		"federated login":     func(s *AuthnChallengeState) { s.FederatedBinding = &FederatedConsentBinding{} },
		"code mode reconnect": func(s *AuthnChallengeState) { s.CodeReconnect = &CodeReconnectBinding{} },
		"workload reconnect":  func(s *AuthnChallengeState) { s.WorkloadReconnect = &WorkloadReconnectBinding{} },
		"no recorded sign-in": func(s *AuthnChallengeState) { s.SignInID = nil },
		"anonymous subject":   func(s *AuthnChallengeState) { s.Subject = &anonymous },
		"another subject":     func(s *AuthnChallengeState) { s.Subject = &other },
		"support session":     func(s *AuthnChallengeState) { s.AuthorizerImpersonated = &yes },
		"unknown provenance":  func(s *AuthnChallengeState) { s.AuthorizerImpersonated = nil },
		"switches used up":    func(s *AuthnChallengeState) { s.AccountSwitches = maxAccountSwitches },
	} {
		state := allowed()
		edit(&state)
		require.False(t, accountSwitchAllowed(state), name)
	}
}

func TestRefusedConsentKeyWithoutSignInKeepsTheEarlierKey(t *testing.T) {
	t.Parallel()

	var scope mcpconnectionattempts.Scope
	attempt := mcpconnectionattempts.AttemptAt(uuid.New(), time.Now(), scope)
	clientID, signInID := uuid.New(), uuid.New()
	code := mcpconnectionattempts.CodeUnmanagedAccount
	require.Equal(t, "connectionAttemptIneligible:"+attempt.ID().String()+":"+clientID.String()+":"+code.String(), refusedConsentKey(attempt, clientID, uuid.Nil, code))
	require.Equal(t, "connectionAttemptIneligible:"+attempt.ID().String()+":"+clientID.String()+":"+signInID.String()+":"+code.String(), refusedConsentKey(attempt, clientID, signInID, code))
}
