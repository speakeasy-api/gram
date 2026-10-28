package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestCodeReconnectBinding(t *testing.T) {
	t.Parallel()
	now := time.Now()
	b := &CodeReconnectBinding{UserID: "user-owner", Server: "tracker", ServerID: uuid.New(), ExpiresAt: now.Add(time.Minute)}
	require.NoError(t, b.authorize("user-owner", false, now))
	require.Error(t, b.authorize("other", false, now))
	require.Error(t, b.authorize("user-owner", true, now))
	require.Error(t, b.authorize("user-owner", false, b.ExpiresAt))
	subject := urn.NewUserSubject("user-owner")
	impersonated := false
	state := AuthnChallengeState{FirstParty: true, CodeReconnect: b, FocusServer: "tracker", Subject: &subject, AuthorizerUserID: subject.ID, AuthorizerImpersonated: &impersonated}
	require.NoError(t, validateCodeReconnectBinding(state))
	state.FocusServer = "other"
	require.Error(t, validateCodeReconnectBinding(state))
	state.FocusServer = "tracker"
	b.Ticket = true
	require.Error(t, validateCodeReconnectBinding(state))
	var absent *CodeReconnectBinding
	require.NoError(t, absent.authorize("", false, now))
}

func TestCodeReconnectBindingRejectsChangedIdentity(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"subject", "authorizer", "impersonation", "expiry"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			subject := urn.NewUserSubject("owner")
			b := &CodeReconnectBinding{UserID: "owner", Server: "tracker", ServerID: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
			state := AuthnChallengeState{FirstParty: true, CodeReconnect: b, FocusServer: "tracker", Subject: &subject, AuthorizerUserID: "owner", AuthorizerImpersonated: new(false)}
			switch scenario {
			case "subject":
				subject = urn.NewUserSubject("other")
			case "authorizer":
				state.AuthorizerUserID = "other"
			case "impersonation":
				state.AuthorizerImpersonated = new(true)
			case "expiry":
				b.ExpiresAt = time.Now().Add(-time.Minute)
			}
			require.Error(t, validateCodeReconnectBinding(state))
		})
	}
}

func TestCodeConnectionAuthenticationMetadata(t *testing.T) {
	t.Parallel()
	b := &metaCodeBackend{}
	for _, backend := range []metaMemberBackend{metaMemberBackendHosted, metaMemberBackendProxied} {
		state := b.knownConnectionState(t.Context(), &metaGateContext{}, metaMember{backend: backend})
		if backend == metaMemberBackendHosted {
			require.Equal(t, "not_required", state.Authentication)
		} else {
			require.Equal(t, "unknown", state.Authentication)
		}
		require.Empty(t, state.Instructions)
	}
}
