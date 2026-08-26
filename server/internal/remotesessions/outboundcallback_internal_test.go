package remotesessions

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Upstream OAuth registrations and vendor allowlists hold Gram's redirect URI
// and CIMD client_id, and we cannot migrate them. Moving the canonical host
// must therefore leave both untouched.
func TestOutboundCallbackURLIsPinnedAgainstCanonicalHost(t *testing.T) {
	t.Parallel()

	pinned, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)

	// The deployment's canonical host has moved elsewhere.
	canonical, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	mgr := NewChallengeManager(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		nil,
		nil,
		policy,
		cache.NoopCache,
		canonical,
	).WithOutboundCallbackURL(pinned)

	require.Equal(t, "https://app.getgram.ai/mcp/remote_login_callback", mgr.callbackURL(""))
	require.Equal(t, "https://app.getgram.ai/mcp/remote_login_callback", mgr.callbackURL("mcp"))
	require.Equal(t, "https://app.getgram.ai/x/mcp/remote_login_callback", mgr.callbackURL("x/mcp"))
	require.Equal(t, "https://app.getgram.ai/oauth/callback", mgr.legacyCallbackURL())

	clientID := uuid.MustParse("00000000-0000-0000-0000-0000000000ff")
	require.Equal(t,
		"https://app.getgram.ai/.well-known/oauth-client/"+clientID.String(),
		ClientMetadataDocumentURL(pinned, clientID))
}

// A deployment that configures nothing extra keeps today's behaviour: the
// outbound callback is the server URL.
func TestOutboundCallbackURLDefaultsToServerURL(t *testing.T) {
	t.Parallel()

	serverURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	mgr := NewChallengeManager(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		nil,
		nil,
		policy,
		cache.NoopCache,
		serverURL,
	)

	require.Equal(t, "https://app.getgram.ai/mcp/remote_login_callback", mgr.callbackURL(""))

	// A nil override is ignored rather than clearing the default.
	require.Equal(t, "https://app.getgram.ai/mcp/remote_login_callback", mgr.WithOutboundCallbackURL(nil).callbackURL(""))
}
