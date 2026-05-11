package usersessions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateAfterDefaults runs the production order — SetDefaults then
// Validate — on the supplied request and returns the validation error.
func validateAfterDefaults(req *RegistrationRequest) error {
	req.SetDefaults()
	return req.Validate()
}

func TestRegistrationRequest_Validate(t *testing.T) {
	t.Parallel()

	t.Run("accepts a fully populated request", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "Acme MCP Client",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "client_secret_basic",
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("accepts a minimal request with optional fields absent", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "minimal",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects missing client_name", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_client_metadata", "client_name")
	})

	t.Run("rejects missing redirect_uris", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{ClientName: "named"}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_redirect_uri", "redirect_uris")
	})

	t.Run("rejects empty redirect_uris slice", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{ClientName: "named", RedirectURIs: []string{}}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_redirect_uri", "redirect_uris")
	})

	t.Run("rejects relative redirect URI", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"/callback"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects URI missing scheme", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"app.acme.test/callback"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects URI missing host", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https:///callback"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects unsupported grant_type", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
			GrantTypes:   []string{"password"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported grant_type "password"`)
	})

	t.Run("rejects unsupported response_type", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:    "named",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			ResponseTypes: []string{"token"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported response_type "token"`)
	})

	t.Run("rejects unsupported token_endpoint_auth_method", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "named",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "client_secret_jwt",
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported token_endpoint_auth_method "client_secret_jwt"`)
	})

	t.Run("accepts public client (none) auth method", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "public",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "none",
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects refresh_token alone (no authorization_code)", func(t *testing.T) {
		t.Parallel()
		// bflad's RFC 7591 §2.1 example
		req := &RegistrationRequest{
			ClientName:    "drift",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			GrantTypes:    []string{"refresh_token"},
			ResponseTypes: []string{"code"},
		}
		assertRegistrationError(t, validateAfterDefaults(req), "invalid_client_metadata", `response_type "code" requires grant_type "authorization_code"`)
	})

	t.Run("accepts authorization_code with refresh_token", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:    "paired",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			GrantTypes:    []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})
}

func TestRegistrationRequest_SetDefaults(t *testing.T) {
	t.Parallel()

	t.Run("populates all defaults when absent", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		req.SetDefaults()
		assert.Equal(t, []string{"authorization_code"}, req.GrantTypes)
		assert.Equal(t, []string{"code"}, req.ResponseTypes)
		assert.Equal(t, "client_secret_basic", req.TokenEndpointAuthMethod)
	})

	t.Run("does not overwrite supplied values", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:              "named",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			GrantTypes:              []string{"refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
		}
		req.SetDefaults()
		assert.Equal(t, []string{"refresh_token"}, req.GrantTypes)
		assert.Equal(t, []string{"code"}, req.ResponseTypes)
		assert.Equal(t, "none", req.TokenEndpointAuthMethod)
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()
		req := &RegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		req.SetDefaults()
		first := *req
		req.SetDefaults()
		assert.Equal(t, first, *req)
	})
}

// assertRegistrationError fails the test unless err unwraps to a
// *RegistrationError with the expected code and a description containing
// the expected substring.
func assertRegistrationError(t *testing.T, err error, wantCode, wantDescriptionSubstr string) {
	t.Helper()
	require.Error(t, err)
	var regErr *RegistrationError
	require.ErrorAs(t, err, &regErr, "expected *RegistrationError, got %T (%v)", err, err)
	assert.Equal(t, wantCode, regErr.Code)
	assert.Contains(t, regErr.Description, wantDescriptionSubstr)
}
