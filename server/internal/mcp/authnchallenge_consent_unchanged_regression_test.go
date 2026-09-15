// First-party pages still auto-close unless a grant could gain an identity on reconnect.

package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp"
)

// firstPartyConsent re-mints the fixture's consent challenge as a first-party
// one, the shape ServeFirstPartyConnect stores: no client, no redirect.
func firstPartyConsent(t *testing.T, ctx context.Context, fx consentActionFixture) consentActionFixture {
	t.Helper()
	stateID := uuid.NewString()
	subject := fx.subject
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: fx.shared,
		Endpoint: mcp.EndpointRef{
			McpSlug:        fx.endpoint.Slug,
			RouteBase:      fx.endpoint.RouteBase,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            "",
		RedirectURI:         "",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		FirstParty:          true,
		CreatedAt:           time.Now(),
	}))
	fx.stateID = stateID
	return fx
}

func expectFirstPartyAutoClose(t *testing.T, fx consentActionFixture) {
	t.Helper()
	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "1 of 1 connected")
	require.Contains(t, html, "data-auto-close", "a fully connected first-party page closes itself")
	require.Contains(t, html, "Connection complete. This tab will close automatically.")
	require.NotContains(t, html, consentIdentityCopy)
	require.NotContains(t, html, "> Reconnect </button>")
}

func TestServeConsent_Unchanged_FirstPartyAutoClosesWhenGrantAlreadyCarriesOpenID(t *testing.T) {
	t.Parallel()
	ctx, fx, _, clientID, issuerID := metaConsent(t, "unchanged-fp-openid")
	fx = firstPartyConsent(t, ctx, fx)

	advertise(t, ctx, fx, issuerID, []string{"read", "openid"})
	grantScoped(t, ctx, fx, clientID, []string{"read", "openid"})
	expectFirstPartyAutoClose(t, fx)
}

func TestServeConsent_Unchanged_FirstPartyAutoClosesWhenIssuerDoesNotAdvertiseOpenID(t *testing.T) {
	t.Parallel()
	ctx, fx, _, clientID, issuerID := metaConsent(t, "unchanged-fp-no-openid")
	fx = firstPartyConsent(t, ctx, fx)

	advertise(t, ctx, fx, issuerID, []string{"read"})
	grantScoped(t, ctx, fx, clientID, []string{"read"})
	expectFirstPartyAutoClose(t, fx)
}
