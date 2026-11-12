package authn

import (
	"context"
	"net/http"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// Browser tabs share the host-only proof cookie. Loading a second consent page
// rotates it; the first page must fail closed without consuming its challenge.
func TestOAuthConsentProofRotation(t *testing.T) {
	t.Parallel()
	container, clone, err := testenv.NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	db, err := clone(t, "channels_browser_rotation")
	require.NoError(t, err)
	f := newOAuthFixture(t, db)
	client := f.register(t)
	first, originalProof := f.challenge(t, client)
	second, currentProof := f.challenge(t, client)
	require.NotEqual(t, originalProof.Value, currentProof.Value)

	stale := f.request(http.MethodPost, "/channels/oauth/authorize", first.Encode(), testOrigin, currentProof)
	require.Equal(t, http.StatusForbidden, stale.Code)
	require.JSONEq(t, `{"error":"invalid_request"}`, stale.Body.String())
	// A rejected binding does not consume or weaken the challenge.
	original := f.request(http.MethodPost, "/channels/oauth/authorize", first.Encode(), testOrigin, originalProof)
	require.Equal(t, http.StatusSeeOther, original.Code, original.Body.String())
	latest := f.request(http.MethodPost, "/channels/oauth/authorize", second.Encode(), testOrigin, currentProof)
	require.Equal(t, http.StatusSeeOther, latest.Code, latest.Body.String())
	// Retrying a successfully submitted page fails even with its valid proof.
	replay := f.request(http.MethodPost, "/channels/oauth/authorize", second.Encode(), testOrigin, currentProof)
	require.Equal(t, http.StatusForbidden, replay.Code)
	require.JSONEq(t, `{"error":"invalid_request"}`, replay.Body.String())
}
