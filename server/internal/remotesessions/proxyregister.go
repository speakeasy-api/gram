//nolint:exhaustruct // Dynamic registration responses intentionally omit optional expiration values.
package remotesessions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/registration"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// proxyRegisterMaxBodyBytes caps both the inbound request body and the upstream
// DCR response read at 10 MiB.
const proxyRegisterMaxBodyBytes int64 = 10 * 1024 * 1024

var ErrInvalidDynamicClientRegistrationEndpoint = errors.New("invalid dynamic client registration endpoint")

type ProxyRegisterRequest struct {
	RegistrationEndpoint    string  `json:"registration_endpoint"`
	Scope                   *string `json:"scope,omitempty"`
	TokenEndpointAuthMethod *string `json:"token_endpoint_auth_method,omitempty"`
}

type ProxyRegisterResponse struct {
	ClientID                string             `json:"client_id"`
	ClientSecret            string             `json:"client_secret,omitempty"`
	ClientSecretExpiresAt   pgtype.Timestamptz `json:"-"`
	TokenEndpointAuthMethod string             `json:"token_endpoint_auth_method,omitempty"`
}

// DynamicClientRegistrationError is retained as the remotesessions-facing name
// for the shared automatic registration HTTP error.
type DynamicClientRegistrationError = registration.HTTPError

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

// RegisterDynamicClient performs Dynamic Client Registration against an
// upstream provider. Trusted server-side callers use it when the provider and
// registration endpoint were discovered from a persisted resource, never from
// an MCP or browser input. The returned secret is transient and callers must
// encrypt it before persistence without returning or logging it.
func RegisterDynamicClient(ctx context.Context, policy *guardian.Policy, serverURL *url.URL, request ProxyRegisterRequest, telemetry registration.Recorder) (ProxyRegisterResponse, error) {
	if policy == nil || serverURL == nil {
		return ProxyRegisterResponse{}, fmt.Errorf("dynamic client registration is not configured")
	}

	endpoint, err := url.Parse(request.RegistrationEndpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return ProxyRegisterResponse{}, ErrInvalidDynamicClientRegistrationEndpoint
	}
	recordFailure := func(err error) {
		if telemetry != nil && !errors.Is(err, context.Canceled) {
			telemetry.RecordFailure(ctx, registration.MethodDCR, registration.ClassifyDCR(err))
		}
	}

	origin := serverURL.String()
	redirectURIs := []string{
		fmt.Sprintf("%s/oauth/callback", origin),
		fmt.Sprintf("%s/mcp/remote_login_callback", origin),
		fmt.Sprintf("%s/x/mcp/remote_login_callback", origin),
	}

	dcrReq := DCRRequest{
		RedirectURIs:            redirectURIs,
		TokenEndpointAuthMethod: conv.PtrValOr(request.TokenEndpointAuthMethod, ""),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              "Speakeasy",
		ClientURI:               origin,
		Scope:                   conv.PtrValOr(request.Scope, ""),
	}

	body, err := json.Marshal(dcrReq)
	if err != nil {
		return ProxyRegisterResponse{}, fmt.Errorf("marshal DCR request: %w", err)
	}

	upstreamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return ProxyRegisterResponse{}, fmt.Errorf("create DCR request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Dynamic registration may return a client secret. Never follow a redirect:
	// a provider-controlled redirect could resend the registration request to a
	// different origin or downgrade transport security before the secret can be
	// encrypted by the caller.
	httpClient := policy.Client()
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		var exhausted *guardian.RetriesExhaustedError
		if errors.As(err, &exhausted) && exhausted.StatusCode != 0 {
			httpErr := &registration.HTTPError{
				StatusCode:      exhausted.StatusCode,
				ProviderMessage: registration.SanitizeProviderMessage(dcrErrorDetail([]byte(exhausted.Body))),
			}
			recordFailure(httpErr)
			return ProxyRegisterResponse{}, httpErr
		}
		reachErr := fmt.Errorf("reach registration endpoint: %w", err)
		recordFailure(reachErr)
		return ProxyRegisterResponse{}, reachErr
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, proxyRegisterMaxBodyBytes))
	if err != nil {
		readErr := fmt.Errorf("read DCR response: %w", err)
		recordFailure(readErr)
		return ProxyRegisterResponse{}, readErr
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		invalidErr := &registration.InvalidSuccessResponseError{Err: fmt.Errorf("unsupported DCR success status %d", resp.StatusCode), StatusCode: resp.StatusCode}
		recordFailure(invalidErr)
		return ProxyRegisterResponse{}, invalidErr
	}
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		httpErr := &registration.HTTPError{StatusCode: resp.StatusCode, ProviderMessage: registration.SanitizeProviderMessage(dcrErrorDetail(respBody))}
		recordFailure(httpErr)
		return ProxyRegisterResponse{}, httpErr
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		httpErr := &registration.HTTPError{StatusCode: resp.StatusCode, ProviderMessage: registration.SanitizeProviderMessage(dcrErrorDetail(respBody))}
		recordFailure(httpErr)
		return ProxyRegisterResponse{}, httpErr
	}

	var dcrResp DCRResponse
	if err := json.Unmarshal(respBody, &dcrResp); err != nil {
		invalidErr := &registration.InvalidSuccessResponseError{Err: fmt.Errorf("decode DCR response: %w", err), StatusCode: resp.StatusCode}
		recordFailure(invalidErr)
		return ProxyRegisterResponse{}, invalidErr
	}
	if dcrResp.ClientID == "" {
		invalidErr := &registration.InvalidSuccessResponseError{Err: errors.New("DCR response missing client_id"), StatusCode: resp.StatusCode}
		recordFailure(invalidErr)
		return ProxyRegisterResponse{}, invalidErr
	}
	clientSecretExpiresAt := pgtype.Timestamptz{}
	if dcrResp.ClientSecretExpiresAt > 0 {
		clientSecretExpiresAt = conv.ToPGTimestamptz(time.Unix(dcrResp.ClientSecretExpiresAt, 0).UTC())
	}
	return ProxyRegisterResponse{
		ClientID:                dcrResp.ClientID,
		ClientSecret:            dcrResp.ClientSecret,
		ClientSecretExpiresAt:   clientSecretExpiresAt,
		TokenEndpointAuthMethod: dcrResp.TokenEndpointAuthMethod,
	}, nil
}

// handleProxyRegister performs Dynamic Client Registration against an upstream
// OAuth provider on behalf of the dashboard user so the dashboard can wire up
// remote_session_clients without hitting the upstream's registration_endpoint
// from the browser (CORS). SSRF is gated by the guardian policy's HTTP client.
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

	registered, err := RegisterDynamicClient(ctx, s.policy, s.serverURL, req, s.registrationTelemetry)
	if err != nil {
		mapped := proxyRegistrationError(err)
		if mapped.Code == oops.CodeBadRequest {
			return mapped.LogWarn(ctx, s.logger)
		}
		return mapped.LogError(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(registered); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode proxyRegister response", attr.SlogError(err))
	}
	return nil
}

func proxyRegistrationError(err error) *oops.ShareableError {
	if errors.Is(err, ErrInvalidDynamicClientRegistrationEndpoint) {
		return oops.E(oops.CodeBadRequest, err, "invalid identity provider registration endpoint")
	}
	var registrationErr *DynamicClientRegistrationError
	if errors.As(err, &registrationErr) && registrationErr.StatusCode >= http.StatusBadRequest && registrationErr.StatusCode < http.StatusInternalServerError && registration.ClassifyDCR(err).Outcome == registration.OutcomeRefused {
		if registrationErr.ProviderMessage != "" {
			return oops.E(oops.CodeBadRequest, err, "identity provider rejected the client registration: %s", registrationErr.ProviderMessage)
		}
		return oops.E(oops.CodeBadRequest, err, "identity provider rejected the client registration")
	}
	return oops.E(oops.CodeGatewayError, err, "failed to register client with identity provider")
}

// dcrErrorDetail extracts a human-readable reason from an RFC 7591 error
// response body, preferring the machine-readable error/error_description fields.
// An unstructured body is not surfaced as a provider message.
func dcrErrorDetail(body []byte) string {
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
	return ""
}
