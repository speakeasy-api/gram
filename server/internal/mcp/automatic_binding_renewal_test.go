package mcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAutomaticBindingRenewalRejectsUntrustedParent(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing parent", "missing parent reference", "missing subject", "agent subject", "missing parent subject", "different subject", "different authorizer", "impersonated", "unknown impersonation", "different issuer"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			s := &Service{authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](testenv.NewLogger(t), testenv.NewMemoryCache(), cache.SuffixNone)}
			subject := urn.NewUserSubject("example-user")
			parent := AuthnChallengeState{ID: uuid.NewString(), Subject: &subject, AuthorizerUserID: subject.ID, AuthorizerImpersonated: new(false), UserSessionIssuerID: uuid.New(), CreatedAt: time.Now()}
			state := remotesessions.RemoteLoginState{ParentChallengeID: parent.ID, Subject: &subject, UserSessionIssuerID: parent.UserSessionIssuerID}
			switch scenario {
			case "missing parent":
				state.ParentChallengeID = uuid.NewString()
			case "missing parent reference":
				state.ParentChallengeID = ""
			case "missing subject":
				state.Subject = nil
			case "agent subject":
				agent := urn.NewAgentSubject(uuid.New())
				state.Subject = &agent
				parent.Subject = &agent
				parent.AuthorizerUserID = agent.ID
			case "missing parent subject":
				parent.Subject = nil
			case "different subject":
				other := urn.NewUserSubject("other-example-user")
				parent.Subject = &other
			case "different authorizer":
				parent.AuthorizerUserID = "other-example-user"
			case "impersonated":
				parent.AuthorizerImpersonated = new(true)
			case "unknown impersonation":
				parent.AuthorizerImpersonated = nil
			case "different issuer":
				parent.UserSessionIssuerID = uuid.New()
			}
			require.NoError(t, s.authnChallengeCache.Store(t.Context(), parent))
			// No database is configured: these checks must fail closed before resolving
			// an endpoint or obtaining any authority to change agent assignments.
			allowed, err := s.authorizeAutomaticBindingRenewal(t.Context(), state)
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
}
