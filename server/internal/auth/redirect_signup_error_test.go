package auth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestService_redirectSignupError exercises redirectSignupError directly
// rather than through Callback: forcing the org-provisioning call it guards
// to fail without also failing an earlier call in the same request (e.g.
// UpsertUserFromIDP) is not practical from outside the package, since both
// paths write through the same database pool.
func TestService_redirectSignupError(t *testing.T) {
	t.Parallel()

	svc := &Service{
		logger: testenv.NewLogger(t),
		cfg: AuthConfigurations{
			SignInRedirectURL: "http://localhost:3000/dashboard",
		},
	}

	result, err := svc.redirectSignupError(t.Context(), nil, errors.New("provisioning failed"))
	require.NoError(t, err, "signup failures are reported by redirect, not by error")
	require.Equal(t, "http://localhost:3000/dashboard/sign-up?signin_error=init_error", result.Location)
	require.Empty(t, result.SessionToken)
	require.Empty(t, result.SessionCookie)
}

// TestService_redirectSignupError_TrimsTrailingSlash confirms the redirect
// does not end up with a double slash when SignInRedirectURL is configured
// with a trailing one.
func TestService_redirectSignupError_TrimsTrailingSlash(t *testing.T) {
	t.Parallel()

	svc := &Service{
		logger: testenv.NewLogger(t),
		cfg: AuthConfigurations{
			SignInRedirectURL: "http://localhost:3000/dashboard/",
		},
	}

	result, err := svc.redirectSignupError(t.Context(), nil, errors.New("provisioning failed"))
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/dashboard/sign-up?signin_error=init_error", result.Location)
}

// TestService_redirectSignupError_PreservesDestination confirms the retry page
// keeps the post-login destination that the failed attempt carried through the
// IDP round trip: /sign-up threads ?redirect= into the next login attempt, so
// dropping it here would strand a deep-linked signup on the dashboard root.
func TestService_redirectSignupError_PreservesDestination(t *testing.T) {
	t.Parallel()

	svc := &Service{
		logger: testenv.NewLogger(t),
		cfg: AuthConfigurations{
			SignInRedirectURL: "http://localhost:3000",
		},
	}

	state := encodeStateParam("/~/toolsets?tab=all", "nonce")
	payload := &gen.CallbackPayload{Code: "code", State: &state}

	result, err := svc.redirectSignupError(t.Context(), payload, errors.New("provisioning failed"))
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/sign-up?signin_error=init_error&redirect=%2F~%2Ftoolsets%3Ftab%3Dall", result.Location)
}

// TestService_redirectSignupError_DropsForeignDestination confirms an
// off-origin destination smuggled into the unsigned state param is discarded
// rather than echoed back into the retry URL.
func TestService_redirectSignupError_DropsForeignDestination(t *testing.T) {
	t.Parallel()

	svc := &Service{
		logger: testenv.NewLogger(t),
		cfg: AuthConfigurations{
			SignInRedirectURL: "http://localhost:3000",
		},
		siteOrigin: parseSiteOrigin("http://localhost:3000"),
	}

	state := encodeStateParam("https://evil.example/phish", "nonce")
	payload := &gen.CallbackPayload{Code: "code", State: &state}

	result, err := svc.redirectSignupError(t.Context(), payload, errors.New("provisioning failed"))
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/sign-up?signin_error=init_error", result.Location)
}
