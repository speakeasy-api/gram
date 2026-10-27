package mcp

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestWorkloadReconnectOwnerBinding(t *testing.T) {
	t.Parallel()
	now := time.Now()
	owner := urn.NewUserSubject("test-owner")
	other := urn.NewUserSubject("other-owner")
	no := false
	base := AuthnChallengeState{Subject: &owner, AuthorizerUserID: owner.ID, AuthorizerImpersonated: &no, WorkloadReconnect: &WorkloadReconnectBinding{OwnerUserID: owner.ID, ClientID: uuid.New(), ExpiresAt: now.Add(time.Minute)}}
	require.NoError(t, validateChallengeBrowser(httptest.NewRequest("GET", "/", nil), base, false))
	require.NoError(t, base.WorkloadReconnect.authorize(owner.ID, false, now))
	require.Error(t, base.WorkloadReconnect.authorize(other.ID, false, now))
	require.Error(t, base.WorkloadReconnect.authorize(owner.ID, true, now))
	require.Error(t, base.WorkloadReconnect.authorize(owner.ID, false, now.Add(time.Minute)))
	changed := base
	changed.Subject = &other
	require.Error(t, validateChallengeBrowser(httptest.NewRequest("GET", "/", nil), changed, false))
	changed = base
	changed.AuthorizerUserID = other.ID
	require.Error(t, validateChallengeBrowser(httptest.NewRequest("GET", "/", nil), changed, true))
	// Ordinary consent and OAuth flows remain unaffected by this binding.
	var absent *WorkloadReconnectBinding
	require.NoError(t, absent.authorize(other.ID, true, now))
}
