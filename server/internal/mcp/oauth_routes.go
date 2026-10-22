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
	return oops.ErrHandle(logger, handler)
}

// tokenEndpointResolutionError preserves indistinguishable 404s for unknown,
// unserved, and wrong-host issuers. Only a failed grant lookup can establish
// invalid_grant; endpoint discovery failures must retain client credentials.
func tokenEndpointResolutionError(err error) error {
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
