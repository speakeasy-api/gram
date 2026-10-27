package mcp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// isOAuthTokenRoute classifies registered route patterns, not unmatched URLs.
func isOAuthTokenRoute(method, pattern string) bool {
	return method == http.MethodPost && (strings.HasSuffix(pattern, "/token") || strings.HasSuffix(pattern, "/revoke"))
}

// oauthRouteHandler chooses the error format before endpoint resolution so
// setup failures use the same protocol as the dispatched handler.
func oauthRouteHandler(logger *slog.Logger, method, pattern string, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	if isOAuthTokenRoute(method, pattern) {
		return oops.OAuthErrHandle(logger, handler)
	}
	if method == http.MethodPost && strings.HasSuffix(pattern, "/connect/remote-session") {
		return consentActionRouteHandler(logger, handler)
	}
	if (method == http.MethodGet && (strings.HasSuffix(pattern, "/authorize") || strings.HasSuffix(pattern, "/connect/first-party"))) || ((method == http.MethodGet || method == http.MethodPost) && strings.HasSuffix(pattern, "/connect")) {
		return oops.BrowserErrHandle(logger, handler)
	}
	return oops.ErrHandle(logger, handler)
}

// consentActionRouteHandler classifies the parsed action before endpoint or
// state resolution can fail. PostForm.Get selects the first value, exactly as the
// action handler does, including duplicate values. A partially parsed form can
// still identify a fetch operation. Parse failures are deferred to the handler
// so endpoint resolution retains its error precedence.
func consentActionRouteHandler(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	browser := oops.BrowserErrHandle(logger, handler)
	machine := oops.ErrHandle(logger, handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := parseConsentActionForm(w, r); err != nil {
			r = r.WithContext(context.WithValue(r.Context(), consentActionFormErrorKey{}, err))
		}
		switch r.PostForm.Get("action") {
		case "delegate_grant", "finish_delegation", "agent_connections", "agent_attach", "agent_detach", "identity_chaining_check":
			machine.ServeHTTP(w, r)
		default:
			browser.ServeHTTP(w, r)
		}
	})
}

// tokenEndpointResolutionError preserves indistinguishable 404s for unknown,
// unserved, and wrong-host issuers. A resolved endpoint whose issuer has
// retired cannot redeem its grants and must prompt reauthorization.
func tokenEndpointResolutionError(err error) error {
	if errors.Is(err, errUserSessionIssuerRetired) {
		return oops.NewOAuthError(http.StatusBadRequest, oautherr.CodeInvalidGrant, "authorization issuer is no longer available", err)
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeNotFound {
		return oops.NewOAuthError(http.StatusNotFound, oautherr.CodeInvalidRequest, "authorization endpoint not found", err)
	}
	return err
}

// writeTokenPolicyError distinguishes a refused refresh from a client that
// cannot use an initial grant. Authentication failures have their own mapping.
func writeTokenPolicyError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, grantType, description string) error {
	code := oautherr.CodeUnauthorizedClient
	if grantType == oauthwire.GrantTypeRefreshToken {
		code = oautherr.CodeInvalidGrant
	}
	return writeTokenError(ctx, w, logger, http.StatusBadRequest, code, description)
}
