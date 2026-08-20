package identity_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newURLResolver builds a Resolver wired only far enough to construct
// authorization URLs. BuildAuthorizationURL reads idpClientID and idpBaseURL
// and nothing else — no client, no cache, no database — so the rest is nil.
func newURLResolver(t *testing.T, idpBaseURL, idpClientID string) *identity.Resolver {
	t.Helper()

	return identity.NewResolver(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		nil, // redisClient
		idpBaseURL,
		idpClientID,
		nil, // idpClient
		nil, // workosClient
		nil, // systemRoleSeeder
		nil, // orgRepo
		nil, // userRepo
		nil, // pylon
		nil, // posthog
		"",  // cache suffix
	)
}

func TestBuildAuthorizationURL_WorkOS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := newURLResolver(t, "https://unused.example.com", "client_test123")

	u, err := r.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
		CallbackURL:     "https://app.example.com/rpc/auth.callback",
		Scope:           "",
		State:           "state-value",
		ScopesSupported: nil,
		LoginHint:       "someone@example.com",
		ScreenHint:      "sign-up",
	})
	require.NoError(t, err)

	require.Equal(t, "/user_management/authorize", u.Path)

	q := u.Query()
	require.Equal(t, "authkit", q.Get("provider"))
	require.Equal(t, "someone@example.com", q.Get("login_hint"))
	require.Equal(t, "sign-up", q.Get("screen_hint"))
	require.Equal(t, "state-value", q.Get("state"))
	require.Equal(t, "client_test123", q.Get("client_id"))

	// WorkOS's reference does not list `scope` for this endpoint, so it is
	// likely inert here. Asserted anyway: it is what Gram has always sent, and
	// "not documented" is not "verified ignored".
	require.Equal(t, "openid email profile", q.Get("scope"))
}

func TestBuildAuthorizationURL_WorkOSOmitsEmptyHints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := newURLResolver(t, "https://unused.example.com", "client_test123")

	u, err := r.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
		CallbackURL:     "https://app.example.com/rpc/auth.callback",
		Scope:           "",
		State:           "state-value",
		ScopesSupported: nil,
		LoginHint:       "",
		ScreenHint:      "",
	})
	require.NoError(t, err)

	q := u.Query()
	require.False(t, q.Has("login_hint"))
	require.False(t, q.Has("screen_hint"))
}

func TestBuildAuthorizationURL_DevIDP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	r := newURLResolver(t, "https://localhost:8080/oauth2", "gram-local-dev")

	u, err := r.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
		CallbackURL:     "https://localhost:5173/rpc/auth.callback",
		Scope:           "",
		State:           "state-value",
		ScopesSupported: nil,
		LoginHint:       "someone@example.com",
		ScreenHint:      "sign-up",
	})
	require.NoError(t, err)

	require.Equal(t, "/oauth2/authorize", u.Path)

	q := u.Query()
	require.Equal(t, "someone@example.com", q.Get("login_hint"))
	require.Equal(t, "sign-up", q.Get("screen_hint"))

	// dev-idp reads scope and only signs an id_token when it contains
	// "openid". Dropping it here would break local login.
	require.Equal(t, "openid email profile", q.Get("scope"))
}
