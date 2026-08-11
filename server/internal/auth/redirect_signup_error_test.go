package auth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

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

	result, err := svc.redirectSignupError(t.Context(), errors.New("provisioning failed"))
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

	result, err := svc.redirectSignupError(t.Context(), errors.New("provisioning failed"))
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/dashboard/sign-up?signin_error=init_error", result.Location)
}
