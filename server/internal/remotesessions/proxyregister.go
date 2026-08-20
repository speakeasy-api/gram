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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// proxyRegisterMaxBodyBytes caps both the inbound request body and the upstream
// DCR response read at 10 MiB.
const proxyRegisterMaxBodyBytes int64 = 10 * 1024 * 1024

var ErrInvalidDynamicClientRegistrationEndpoint = errors.New("invalid dynamic client registration endpoint")

type ProxyRegisterRequest struct {
	RegistrationEndpoint    string  `json:"registration_endpoint"`
	Scope                   *string `json:"scope,omitempty"`
	TokenEndpointAuthMethod *string `json:"token_endpoint_auth_method,omitempty"`

	// TunneledMcpServerID routes the registration POST through an MCP tunnel
	// instead of dialing the registration endpoint from cloud egress, for
	// authorization servers inside a customer network. Platform admins only;
	// the handler rejects it for everyone else and audit-logs its use. Callers
	// use the tunnel bound to the remote session issuer they are registering.
	TunneledMcpServerID *string `json:"tunneled_mcp_server_id,omitempty"`
}

type ProxyRegisterResponse struct {
	ClientID                string             `json:"client_id"`
	ClientSecret            string             `json:"client_secret,omitempty"`
	ClientSecretExpiresAt   pgtype.Timestamptz `json:"-"`
	TokenEndpointAuthMethod string             `json:"token_endpoint_auth_method,omitempty"`
}

// DynamicClientRegistrationError classifies a refusal from the upstream
// registration endpoint without retaining the response body. Callers can keep
// the existing dashboard distinction between non-retryable 4xx rejections and
// retryable upstream failures without exposing provider details.
type DynamicClientRegistrationError struct {
	StatusCode int
	Detail     string
}

func (e *DynamicClientRegistrationError) Error() string {
	return fmt.Sprintf("registration endpoint returned %d: %s", e.StatusCode, e.Detail)
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

// RegisterDynamicClient performs Dynamic Client Registration against an
// upstream provider. Trusted server-side callers use it when the provider and
// registration endpoint were discovered from a persisted resource, never from
// an MCP or browser input. The returned secret is transient and callers must
// encrypt it before persistence without returning or logging it.
//
// When request carries a TunneledMcpServerID the POST rides that MCP tunnel
// (tunnels must be non-nil); authorization of the parameter is the caller's
// responsibility. Callers that never set it may pass a nil tunnels.
func RegisterDynamicClient(ctx context.Context, policy *guardian.Policy, tunnels *tunnelrouting.HTTPClient, serverURL *url.URL, request ProxyRegisterRequest) (ProxyRegisterResponse, error) {
	if policy == nil || serverURL == nil {
		return ProxyRegisterResponse{}, fmt.Errorf("dynamic client registration is not configured")
	}

	endpoint, err := url.Parse(request.RegistrationEndpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return ProxyRegisterResponse{}, ErrInvalidDynamicClientRegistrationEndpoint
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
	// encrypted by the caller. (The tunnel transport below refuses redirects
	// the same way.)
	httpClient := policy.Client()
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	var doer httpDoer = httpClient
	if tunnelID := conv.PtrValOr(request.TunneledMcpServerID, ""); tunnelID != "" {
		parsed, perr := uuid.Parse(tunnelID)
		if perr != nil {
			return ProxyRegisterResponse{}, fmt.Errorf("parse tunneled_mcp_server_id: %w", perr)
		}
		doer, err = upstreamHTTPDoer(httpClient, tunnels, uuid.NullUUID{UUID: parsed, Valid: true})
		if err != nil {
			return ProxyRegisterResponse{}, fmt.Errorf("select tunnel transport for registration: %w", err)
		}
	}
	resp, err := doer.Do(httpReq)
	if err != nil {
		return ProxyRegisterResponse{}, fmt.Errorf("reach registration endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, proxyRegisterMaxBodyBytes))
	if err != nil {
		return ProxyRegisterResponse{}, fmt.Errorf("read DCR response: %w", err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusMultipleChoices+100 {
		return ProxyRegisterResponse{}, fmt.Errorf("registration endpoint redirected")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return ProxyRegisterResponse{}, &DynamicClientRegistrationError{StatusCode: resp.StatusCode, Detail: dcrErrorDetail(respBody, resp.StatusCode)}
	}

	var dcrResp DCRResponse
	if err := json.Unmarshal(respBody, &dcrResp); err != nil {
		return ProxyRegisterResponse{}, fmt.Errorf("decode DCR response: %w", err)
	}
	if dcrResp.ClientID == "" {
		return ProxyRegisterResponse{}, errors.New("DCR response missing client_id")
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
	authedCtx, err := s.sessions.Authenticate(ctx, sessionToken)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authentication required").LogError(ctx, s.logger)
	}
	ctx = authedCtx

	if s.policy == nil {
		return oops.E(oops.CodeUnexpected, nil, "proxy register handler is not configured").LogError(ctx, s.logger)
	}

	r.Body = http.MaxBytesReader(w, r.Body, proxyRegisterMaxBodyBytes)
	var req ProxyRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid JSON in request body").LogError(ctx, s.logger)
	}

	// Registration through a tunnel sends a request into customer
	// infrastructure before any client row exists to hang authorization off,
	// so v1 keeps it white-glove: platform admins only, validated against a
	// live tunnel, and audit-logged after the upstream accepts.
	var tunnel *repo.GetTunneledMcpServerBindingUnscopedRow
	if conv.PtrValOr(req.TunneledMcpServerID, "") != "" {
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		if !ok || authCtx == nil {
			return oops.C(oops.CodeUnauthorized)
		}
		if !authCtx.IsAdmin {
			return oops.E(oops.CodeForbidden, nil, "registering a client through an MCP tunnel requires a platform admin").LogError(ctx, s.logger)
		}
		tunnelID, terr := uuid.Parse(*req.TunneledMcpServerID)
		if terr != nil {
			return oops.E(oops.CodeBadRequest, terr, "invalid tunneled_mcp_server_id").LogError(ctx, s.logger)
		}
		row, terr := repo.New(s.db).GetTunneledMcpServerBindingUnscoped(ctx, tunnelID)
		if terr != nil {
			if errors.Is(terr, pgx.ErrNoRows) {
				return oops.E(oops.CodeNotFound, terr, "tunneled MCP server not found").LogError(ctx, s.logger)
			}
			return oops.E(oops.CodeUnexpected, terr, "get tunneled mcp server").LogError(ctx, s.logger)
		}
		tunnel = &row
	}

	registered, err := RegisterDynamicClient(ctx, s.policy, s.tunnels, s.serverURL, req)
	if err != nil {
		if errors.Is(err, ErrInvalidDynamicClientRegistrationEndpoint) {
			return oops.E(oops.CodeBadRequest, err, "invalid identity provider registration endpoint").LogWarn(ctx, s.logger)
		}
		var registrationErr *DynamicClientRegistrationError
		if errors.As(err, &registrationErr) && registrationErr.StatusCode >= http.StatusBadRequest && registrationErr.StatusCode < http.StatusInternalServerError {
			return oops.E(oops.CodeBadRequest, err, "identity provider rejected the client registration: %s", registrationErr.Detail).LogWarn(ctx, s.logger)
		}
		return oops.E(oops.CodeGatewayError, err, "failed to register client with identity provider").LogError(ctx, s.logger)
	}

	if tunnel != nil {
		authCtx, _ := contextvalues.GetAuthContext(ctx)
		if err := s.auditLogger.LogTunneledMcpServerDynamicClientRegistration(ctx, s.db, audit.LogTunneledMcpServerDynamicClientRegistrationEvent{
			OrganizationID:        authCtx.ActiveOrganizationID,
			ProjectID:             tunnel.ProjectID,
			Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:      authCtx.Email,
			ActorSlug:             nil,
			TunneledMcpServerURN:  urn.NewTunneledMcpServer(tunnel.ID),
			TunneledMcpServerName: tunnel.Name,
			RegistrationEndpoint:  req.RegistrationEndpoint,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log tunneled dynamic client registration").LogError(ctx, s.logger)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(registered); err != nil {
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
