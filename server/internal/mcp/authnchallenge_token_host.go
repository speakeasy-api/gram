package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
)

// errTokenHostMismatch: a per-endpoint user-session token was presented on a
// host other than the one that minted it.
var errTokenHostMismatch = errors.New("user-session token was minted on another host")

// issuerGateReasonIssuerMismatch labels errTokenHostMismatch rejections, so a
// host cutover can be watched for clients that carry a token across hosts.
const issuerGateReasonIssuerMismatch = "issuer_mismatch"

// checkPerEndpointTokenHost binds a per-endpoint user-session token to the
// host that minted it. The token endpoint stamps `iss` with the issuer the
// request's origin derives (issuerURL over the platform host, the server URL
// or the custom domain), so the token's `iss` origin must match the origin
// this request would mint under. The audience alone does not bind the host:
// it names the endpoint's user session issuer (or, before the issuer
// migration, the toolset), which every host shares.
//
// Scope: callers apply this only to tokens accepted on the issuer-scoped
// audiences. Resource-scoped tokens (ID-JAG, workload) carry the exact
// resource URL as their audience and are already bound to the host. AIM-399
// shared-mode issuers pin `iss` to `<host>/oauth/usi/{id}` and bind `aud` to
// the exact MCP resource URL; their `iss` never names the request host, so
// they must bypass this check. Skip them here when that issuer kind lands.
//
// Accepted without a host check:
//   - requests with no stamped origin (internal callers) and requests that
//     arrived through a private network ingress, which keep their behaviour;
//   - dashboard-minted tokens (client_id client:first-party), whose `iss` is
//     a descriptive endpoint URL built from the server URL, not the host the
//     dashboard was served on;
//   - tokens with no `iss`, which no current minting path produces.
//
// The authentication host only ever stands in for the server URL, so an
// `iss` on either names the canonical host. That keeps tokens valid when an
// issuer toggles use_authentication_host.
func (s *Service) checkPerEndpointTokenHost(ctx context.Context, session sessiontokens.ValidatedSession, endpoint *ResolvedMcpEndpoint, baseURL string) error {
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface == requestorigin.SurfacePrivateNetwork {
		return nil
	}
	if session.ClientID() == sessiontokens.FirstPartyClientID || session.Issuer() == "" {
		return nil
	}

	expectedIssuer, err := s.issuerURL(endpoint, baseURL)
	if err != nil {
		return fmt.Errorf("build expected token issuer: %w", err)
	}
	expected := s.tokenMintOrigin(expectedIssuer)
	minted := s.tokenMintOrigin(session.Issuer())
	if expected == "" || minted != expected {
		return fmt.Errorf("%w: token issuer origin %q, request origin %q", errTokenHostMismatch, minted, expected)
	}
	return nil
}

// tokenMintOrigin reduces an issuer URL to the origin it was minted under,
// mapping the authentication host to the server URL it stands in for. It
// returns "" for a value that is not an absolute URL.
func (s *Service) tokenMintOrigin(issuer string) string {
	minted := urlOrigin(issuer)
	if minted != "" && s.authenticationHostBaseURL != "" && minted == urlOrigin(s.authenticationHostBaseURL) {
		return urlOrigin(s.serverURL.String())
	}
	return minted
}

// urlOrigin is the lowercased scheme://host[:port] of raw, with the scheme's
// default port dropped, or "" when raw is not an absolute URL or carries
// userinfo.
func urlOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "http" && port == "80") {
		return scheme + "://" + net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}
