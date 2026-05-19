package usersessions

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthorizationRequestFromQuery(t *testing.T) {
	t.Parallel()
	q := url.Values{}
	q.Set("client_id", "client_abc")
	q.Set("redirect_uri", "https://app.acme.test/callback")
	q.Set("response_type", "code")
	q.Set("state", "xyz")
	q.Set("code_challenge", "abc123")
	q.Set("code_challenge_method", "S256")

	req := AuthorizationRequestFromQuery(q)
	require.Equal(t, "client_abc", req.ClientID)
	require.Equal(t, "https://app.acme.test/callback", req.RedirectURI)
	require.Equal(t, "code", req.ResponseType)
	require.Equal(t, "xyz", req.State)
	require.Equal(t, "abc123", req.CodeChallenge)
	require.Equal(t, "S256", req.CodeChallengeMethod)
}

func TestAuthorizationRequest_ValidateRedirectableFields(t *testing.T) {
	t.Parallel()

	t.Run("accepts a fully populated request", func(t *testing.T) {
		t.Parallel()
		req := &AuthorizationRequest{
			ClientID:    "client_abc",
			RedirectURI: "https://app.acme.test/callback",
		}
		require.NoError(t, req.ValidateRedirectableFields())
	})

	t.Run("rejects missing client_id", func(t *testing.T) {
		t.Parallel()
		req := &AuthorizationRequest{RedirectURI: "https://app.acme.test/callback"}
		assertOAuthError(t, req.ValidateRedirectableFields(), "invalid_request", "client_id")
	})

	t.Run("rejects missing redirect_uri", func(t *testing.T) {
		t.Parallel()
		req := &AuthorizationRequest{ClientID: "client_abc"}
		assertOAuthError(t, req.ValidateRedirectableFields(), "invalid_request", "redirect_uri")
	})
}

func TestAuthorizationRequest_ValidatePostRedirect(t *testing.T) {
	t.Parallel()

	valid := func() *AuthorizationRequest {
		return &AuthorizationRequest{
			ClientID:            "client_abc",
			RedirectURI:         "https://app.acme.test/callback",
			ResponseType:        "code",
			CodeChallenge:       "abc123",
			CodeChallengeMethod: "S256",
		}
	}

	t.Run("accepts a fully populated request", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, valid().ValidatePostRedirect())
	})

	t.Run("rejects unsupported response_type", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.ResponseType = "token"
		assertOAuthError(t, req.ValidatePostRedirect(), "unsupported_response_type", "response_type")
	})

	t.Run("rejects missing response_type", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.ResponseType = ""
		assertOAuthError(t, req.ValidatePostRedirect(), "unsupported_response_type", "response_type")
	})

	t.Run("rejects missing code_challenge (PKCE mandatory)", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.CodeChallenge = ""
		assertOAuthError(t, req.ValidatePostRedirect(), "invalid_request", "code_challenge")
	})

	t.Run("rejects unsupported code_challenge_method", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.CodeChallengeMethod = "plain"
		assertOAuthError(t, req.ValidatePostRedirect(), "invalid_request", `unsupported code_challenge_method "plain"`)
	})

	t.Run("rejects empty code_challenge_method", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.CodeChallengeMethod = ""
		assertOAuthError(t, req.ValidatePostRedirect(), "invalid_request", "unsupported code_challenge_method")
	})
}
