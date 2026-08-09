package remotesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// proxyRegisterMaxBodyBytes caps both the inbound request body and the upstream
// DCR response read at 10 MiB.
const proxyRegisterMaxBodyBytes int64 = 10 * 1024 * 1024

type ProxyRegisterRequest struct {
	RegistrationEndpoint    string  `json:"registration_endpoint"`
	Scope                   *string `json:"scope,omitempty"`
	TokenEndpointAuthMethod *string `json:"token_endpoint_auth_method,omitempty"`
}

type ProxyRegisterResponse struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

// DCRRequest is the RFC 7591 Dynamic Client Registration request Gram sends to
// an upstream provider on the caller's behalf.
type DCRRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
}

// DCRResponse is the subset of the RFC 7591 registration response Gram reads.
type DCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
}

// handleProxyRegister performs Dynamic Client Registration against an upstream
// OAuth provider on behalf of the dashboard user so the dashboard can wire up
// remote_session_clients without hitting the upstream's registration_endpoint
// from the browser (CORS). The handler forwards the caller's `scope` and
// `token_endpoint_auth_method` verbatim when supplied and omits them otherwise
// — interpreting RFC 7591 spec defaults is the upstream's job, not Gram's. SSRF
// is gated by the guardian policy's HTTP client.
func (s *Service) handleProxyRegister(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	// The session cookie is SameSite=Lax so it does not flow on cross-origin
	// fetch from the dashboard in dev (where the dashboard and API run on
	// different origins). Goa-generated endpoints accept the Gram-Session
	// header, so this raw handler falls back to the header when context has
	// no token, keeping the dashboard surfaces that hit /oauth/proxy-register
	// functional in dev.
	sessionToken, _ := contextvalues.GetSessionTokenFromContext(ctx)
	if sessionToken == "" {
		sessionToken = r.Header.Get(constants.SessionHeader)
	}
	if _, err := s.sessions.Authenticate(ctx, sessionToken); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authentication required").LogError(ctx, s.logger)
	}

	if s.policy == nil {
		return oops.E(oops.CodeUnexpected, nil, "proxy register handler is not configured").LogError(ctx, s.logger)
	}

	r.Body = http.MaxBytesReader(w, r.Body, proxyRegisterMaxBodyBytes)
	var req ProxyRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid JSON in request body").LogError(ctx, s.logger)
	}

	endpoint, err := url.Parse(req.RegistrationEndpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return oops.E(oops.CodeBadRequest, err, "invalid registration_endpoint").LogError(ctx, s.logger)
	}

	serverURL := s.serverURL.String()
	redirectURIs := []string{
		fmt.Sprintf("%s/oauth/callback", serverURL),
		fmt.Sprintf("%s/mcp/remote_login_callback", serverURL),
		fmt.Sprintf("%s/x/mcp/remote_login_callback", serverURL),
	}

	dcrReq := DCRRequest{
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: conv.PtrValOr(req.TokenEndpointAuthMethod, ""),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Speakeasy",
		ClientURI:               serverURL,
		Scope:                   conv.PtrValOr(req.Scope, ""),
	}

	body, err := json.Marshal(dcrReq)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal DCR request").LogError(ctx, s.logger)
	}

	upstreamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create DCR request").LogError(ctx, s.logger)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.policy.Client().Do(httpReq)
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to reach registration endpoint").LogError(ctx, s.logger)
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		if closeErr := resp.Body.Close(); closeErr != nil {
			return fmt.Errorf("close DCR response body: %w", closeErr)
		}
		return nil
	})

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, proxyRegisterMaxBodyBytes))
	if err != nil {
		return oops.E(oops.CodeGatewayError, err, "failed to read DCR response").LogError(ctx, s.logger)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logAttrs := []slog.Attr{
			attr.SlogHTTPResponseStatusCode(resp.StatusCode),
			attr.SlogHTTPResponseBody(string(respBody)),
		}

		// A 4xx means the upstream refused this particular registration —
		// e.g. an unsupported scope, a restricted set of redirect URIs, or a
		// token-endpoint auth method it does not offer. That's a configuration
		// mismatch the operator can act on, not a gateway fault, so surface the
		// upstream error/error_description and classify it as a bad request
		// rather than flattening it into an opaque 502.
		if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
			return oops.E(oops.CodeBadRequest, nil,
				"identity provider rejected the client registration: %s",
				dcrErrorDetail(respBody, resp.StatusCode),
			).LogWarn(ctx, s.logger, logAttrs...)
		}

		// A 5xx (or any other unexpected status) is a genuine upstream failure.
		return oops.E(oops.CodeGatewayError, nil,
			"registration endpoint returned %d", resp.StatusCode,
		).LogError(ctx, s.logger, logAttrs...)
	}

	var dcrResp DCRResponse
	if err := json.Unmarshal(respBody, &dcrResp); err != nil {
		return oops.E(oops.CodeGatewayError, err, "invalid DCR response").LogError(ctx, s.logger)
	}
	if dcrResp.ClientID == "" {
		return oops.E(oops.CodeGatewayError, nil, "DCR response missing client_id").LogError(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ProxyRegisterResponse{
		ClientID:                dcrResp.ClientID,
		ClientSecret:            dcrResp.ClientSecret,
		TokenEndpointAuthMethod: dcrResp.TokenEndpointAuthMethod,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode proxyRegister response", attr.SlogError(err))
	}
	return nil
}

// dcrErrorDetail extracts a human-readable reason from an RFC 7591 error
// response body, preferring the machine-readable error/error_description fields
// and falling back to the status code when the body carries neither.
func dcrErrorDetail(body []byte, statusCode int) string {
	var e struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &e); err == nil {
		switch {
		case e.Error != "" && e.ErrorDescription != "":
			return fmt.Sprintf("%s: %s", e.Error, e.ErrorDescription)
		case e.ErrorDescription != "":
			return e.ErrorDescription
		case e.Error != "":
			return e.Error
		}
	}
	return fmt.Sprintf("HTTP %d", statusCode)
}
