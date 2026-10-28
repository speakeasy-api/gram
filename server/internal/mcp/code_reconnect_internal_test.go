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
