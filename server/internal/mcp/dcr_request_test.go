package mcp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validateAfterDefaults runs the production order — SetDefaults then
// Validate — on the supplied request and returns the validation error.
func validateAfterDefaults(req *dcrRegistrationRequest) error {
	req.SetDefaults()
	return req.Validate()
}

func TestDCRRegistrationRequest_Validate(t *testing.T) {
	t.Parallel()

	t.Run("accepts a fully populated request", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:              "Acme MCP Client",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "client_secret_basic",
			Scope:                   "openid",
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("accepts a minimal request with optional fields absent", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:   "minimal",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects missing client_name", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_client_metadata", "client_name")
	})

	t.Run("rejects missing redirect_uris", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{ClientName: "named"}
		assertDCRError(t, validateAfterDefaults(req), "invalid_redirect_uri", "redirect_uris")
	})

	t.Run("rejects empty redirect_uris slice", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{ClientName: "named", RedirectURIs: []string{}}
		assertDCRError(t, validateAfterDefaults(req), "invalid_redirect_uri", "redirect_uris")
	})

	t.Run("rejects relative redirect URI", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"/callback"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects URI missing scheme", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"app.acme.test/callback"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects URI missing host", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https:///callback"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_redirect_uri", "absolute URL")
	})

	t.Run("rejects unsupported grant_type", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
			GrantTypes:   []string{"password"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported grant_type "password"`)
	})

	t.Run("rejects unsupported response_type", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:    "named",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			ResponseTypes: []string{"token"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported response_type "token"`)
	})

	t.Run("rejects unsupported token_endpoint_auth_method", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:              "named",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "client_secret_jwt",
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_client_metadata", `unsupported token_endpoint_auth_method "client_secret_jwt"`)
	})

	t.Run("accepts public client (none) auth method", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:              "public",
			RedirectURIs:            []string{"https://app.acme.test/callback"},
			TokenEndpointAuthMethod: "none",
		}
		require.NoError(t, validateAfterDefaults(req))
	})

	t.Run("rejects refresh_token alone (no authorization_code)", func(t *testing.T) {
		t.Parallel()
		// bflad's RFC 7591 §2.1 example
		req := &dcrRegistrationRequest{
			ClientName:    "drift",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			GrantTypes:    []string{"refresh_token"},
			ResponseTypes: []string{"code"},
		}
		assertDCRError(t, validateAfterDefaults(req), "invalid_client_metadata", `response_type "code" requires grant_type "authorization_code"`)
	})

	t.Run("accepts authorization_code with refresh_token", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
			ClientName:    "paired",
			RedirectURIs:  []string{"https://app.acme.test/callback"},
			GrantTypes:    []string{"authorization_code", "refresh_token"},
			ResponseTypes: []string{"code"},
		}
		require.NoError(t, validateAfterDefaults(req))
	})
}

func TestDCRRegistrationRequest_SetDefaults(t *testing.T) {
	t.Parallel()

	t.Run("populates all defaults when absent", func(t *testing.T) {
		t.Parallel()
		req := &dcrRegistrationRequest{
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
		req := &dcrRegistrationRequest{
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
		req := &dcrRegistrationRequest{
			ClientName:   "named",
			RedirectURIs: []string{"https://app.acme.test/callback"},
		}
		req.SetDefaults()
		first := *req
		req.SetDefaults()
		assert.Equal(t, first, *req)
	})
}

// assertDCRError fails the test unless err unwraps to a *dcrError with the
// expected code and a description containing the expected substring.
func assertDCRError(t *testing.T, err error, wantCode, wantDescriptionSubstr string) {
	t.Helper()
	require.Error(t, err)
	var dcrErr *dcrError
	require.True(t, errors.As(err, &dcrErr), "expected *dcrError, got %T (%v)", err, err)
	assert.Equal(t, wantCode, dcrErr.Code)
	assert.Contains(t, dcrErr.Description, wantDescriptionSubstr)
}
