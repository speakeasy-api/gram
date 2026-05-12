package usersessions

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthCodeTokenRequestFromForm(t *testing.T) {
	t.Parallel()
	form := url.Values{}
	form.Set("code", "auth_code_123")
	form.Set("redirect_uri", "https://app.acme.test/callback")
	form.Set("code_verifier", "verifier_xyz")

	req := AuthCodeTokenRequestFromForm(form)
	require.Equal(t, "auth_code_123", req.Code)
	require.Equal(t, "https://app.acme.test/callback", req.RedirectURI)
	require.Equal(t, "verifier_xyz", req.CodeVerifier)
}

func TestAuthCodeTokenRequest_Validate(t *testing.T) {
	t.Parallel()

	valid := func() *AuthCodeTokenRequest {
		return &AuthCodeTokenRequest{
			Code:         "auth_code_123",
			RedirectURI:  "https://app.acme.test/callback",
			CodeVerifier: "verifier_xyz",
		}
	}

	t.Run("accepts a fully populated request", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, valid().Validate())
	})

	t.Run("rejects missing code", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.Code = ""
		assertOAuthError(t, req.Validate(), "invalid_request", "code is required")
	})

	t.Run("rejects missing redirect_uri", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.RedirectURI = ""
		assertOAuthError(t, req.Validate(), "invalid_request", "redirect_uri")
	})

	t.Run("rejects missing code_verifier", func(t *testing.T) {
		t.Parallel()
		req := valid()
		req.CodeVerifier = ""
		assertOAuthError(t, req.Validate(), "invalid_request", "code_verifier")
	})
}
