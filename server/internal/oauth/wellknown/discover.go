package wellknown

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// discoverProtectedResourceTimeout bounds the total time spent probing an
	// upstream resource server for RFC 9728 metadata so a slow upstream cannot
	// tie up a synchronous management API request.
	discoverProtectedResourceTimeout = 10 * time.Second

	// discoverProtectedResourceMaxBodyBytes caps how many response bytes are
	// read when decoding an upstream metadata document. Real-world documents
	// are well under this; the cap exists to bound allocations on a hostile
	// target. Bodies exceeding the cap will fail JSON decode and bucket as a
	// "malformed" failure. Set to 1MiB.
	discoverProtectedResourceMaxBodyBytes = 1 << 20
)

// errReadProbeBody marks a probe failure that happened while consuming the
// response body of a 200 OK upstream. It exists so Code() can distinguish a
// connection that died mid-response (transport_error) from a body that was
// fully delivered but did not decode (malformed). Private — callers branch on
// Code(), not on the cause chain.
var errReadProbeBody = errors.New("read probe body")

// ProtectedResourceDiscoveryError carries the probe URL and optional upstream
// HTTP status alongside the wrapped cause, so callers can compose a typed
// failure code and a user-facing message describing what went wrong. Status is
// zero when no HTTP response was received (transport error, host blocked,
// timeout, malformed probe URL).
type ProtectedResourceDiscoveryError struct {
	ProbeURL string
	Status   int
	cause    error
}

func (e *ProtectedResourceDiscoveryError) Error() string {
	switch {
	case e.ProbeURL == "":
		return e.cause.Error()
	case e.Status > 0:
		return fmt.Sprintf("discover %s: HTTP %d: %s", e.ProbeURL, e.Status, e.cause)
	default:
		return fmt.Sprintf("discover %s: %s", e.ProbeURL, e.cause)
	}
}

func (e *ProtectedResourceDiscoveryError) Unwrap() error { return e.cause }

// Code returns a short, machine-readable string identifying the failure mode.
// Dashboards and SDK callers branch on this; the vocabulary is intentionally
// string-typed (not an enum) so adding new codes does not require a SDK bump.
//
// Current vocabulary:
//
//   - "invalid_url"       — the supplied resource URL could not be parsed or
//     was missing a scheme/host. No probe was attempted.
//   - "host_blocked"      — the upstream host failed guardian.Policy
//     validation (private network, blocked CIDR, DNS rebinding).
//   - "timeout"           — the probe deadline expired before a response was
//     received.
//   - "transport_error"   — connection / TLS / generic transport failure
//     with no HTTP response, or a 200 OK whose body could not be read to
//     completion.
//   - "not_found"         — the upstream returned 404 at the well-known path.
//     The expected outcome for resource servers without OAuth.
//   - "http_error"        — any other non-2xx HTTP status.
//   - "malformed"         — 200 OK but the body was not valid JSON or could
//     not be decoded into the RFC 9728 shape.
func (e *ProtectedResourceDiscoveryError) Code() string {
	switch {
	case errors.Is(e.cause, guardian.ErrBlockedIP), errors.Is(e.cause, guardian.ErrBadHost):
		return "host_blocked"
	case errors.Is(e.cause, context.DeadlineExceeded):
		return "timeout"
	case e.ProbeURL == "":
		return "invalid_url"
	case e.Status == http.StatusNotFound:
		return "not_found"
	case errors.Is(e.cause, errReadProbeBody):
		// Reached the upstream and got 200 OK, but the body never finished
		// arriving — classify as transport, not malformed.
		return "transport_error"
	case e.Status == http.StatusOK:
		return "malformed"
	case e.Status >= 400:
		return "http_error"
	default:
		if netErr, ok := errors.AsType[net.Error](e.cause); ok && netErr.Timeout() {
			return "timeout"
		}
		return "transport_error"
	}
}

// UserMessage returns a public-facing summary suitable for surfacing in the
// dashboard. Callers should render it verbatim — it intentionally names the
// probed URL and HTTP status so operators have enough context to act.
func (e *ProtectedResourceDiscoveryError) UserMessage() string {
	switch e.Code() {
	case "invalid_url":
		return "Could not compute OAuth protected resource metadata URL for the remote MCP server"
	case "host_blocked":
		return "Host is not allowed by network policy"
	case "timeout":
		return fmt.Sprintf("Timed out probing OAuth protected resource metadata at %s", e.ProbeURL)
	case "not_found":
		return fmt.Sprintf("OAuth protected resource metadata not advertised at %s", e.ProbeURL)
	case "malformed":
		return fmt.Sprintf("OAuth protected resource metadata at %s was not a valid RFC 9728 document", e.ProbeURL)
	case "http_error":
		return fmt.Sprintf("Unexpected HTTP %d from %s", e.Status, e.ProbeURL)
	default:
		if _, ok := errors.AsType[*tls.CertificateVerificationError](e.cause); ok {
			return fmt.Sprintf("TLS certificate verification failed probing %s", e.ProbeURL)
		}
		if _, ok := errors.AsType[*tls.RecordHeaderError](e.cause); ok {
			return fmt.Sprintf("TLS handshake failed probing %s", e.ProbeURL)
		}
		return fmt.Sprintf("Could not reach OAuth protected resource metadata at %s", e.ProbeURL)
	}
}

// DiscoverProtectedResourceMetadata fetches and decodes an RFC 9728
// .well-known/oauth-protected-resource document advertised by a remote
// resource server. The probe runs through the supplied [guardian.Policy] so
// SSRF blocklists, DNS rebinding checks, and OTel instrumentation all apply.
//
// On any failure, returns a *[ProtectedResourceDiscoveryError] carrying the
// probed URL plus enough context to render a typed code and message.
//
// Per RFC 9728 §3.1, when the resource URL includes a path component the
// well-known document may live at either "<origin>/.well-known/oauth-protected-resource<path>"
// (path-style) or "<origin>/.well-known/oauth-protected-resource" (origin-style).
// This helper attempts the path-style candidate first; a 404 falls through to
// the origin-style candidate. Any other status (2xx, 5xx, etc.) returns the
// first attempt's result directly — 5xx in particular must not be masked by a
// fallback probe. Resource URLs without a path component skip straight to
// origin-style. The whole sequence shares a single discoverProtectedResourceTimeout
// budget.
func DiscoverProtectedResourceMetadata(ctx context.Context, policy *guardian.Policy, resourceURL string) (OAuthProtectedResourceMetadata, []string, error) {
	candidates, err := protectedResourceProbeCandidates(resourceURL)
	if err != nil {
		return OAuthProtectedResourceMetadata{}, nil, &ProtectedResourceDiscoveryError{
			ProbeURL: "",
			Status:   0,
			cause:    fmt.Errorf("compute probe url: %w", err),
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, discoverProtectedResourceTimeout)
	defer cancel()

	client := policy.Client()

	var lastErr *ProtectedResourceDiscoveryError
	for _, probeURL := range candidates {
		doc, attemptErr := attemptProtectedResourceProbe(reqCtx, client, probeURL)
		if attemptErr == nil {
			return doc, collectProtectedResourceWarnings(resourceURL, doc), nil
		}

		// Only a 404 at one candidate falls through to the next. Every other
		// failure mode — including 5xx, transport errors, and timeouts —
		// returns immediately so the caller sees the upstream's real signal.
		if attemptErr.Status != http.StatusNotFound {
			return OAuthProtectedResourceMetadata{}, nil, attemptErr
		}
		lastErr = attemptErr
	}

	return OAuthProtectedResourceMetadata{}, nil, lastErr
}

// attemptProtectedResourceProbe issues a single GET against probeURL and
// returns either the decoded metadata or a typed error annotated with the
// upstream URL and status. The caller is responsible for the timeout context
// and for any retry/fallback logic; this helper does one HTTP round trip.
func attemptProtectedResourceProbe(ctx context.Context, client *guardian.HTTPClient, probeURL string) (OAuthProtectedResourceMetadata, *ProtectedResourceDiscoveryError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return OAuthProtectedResourceMetadata{}, &ProtectedResourceDiscoveryError{
			ProbeURL: probeURL,
			Status:   0,
			cause:    fmt.Errorf("build probe request: %w", err),
		}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return OAuthProtectedResourceMetadata{}, &ProtectedResourceDiscoveryError{
			ProbeURL: probeURL,
			Status:   0,
			cause:    fmt.Errorf("fetch probe document: %w", err),
		}
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return OAuthProtectedResourceMetadata{}, &ProtectedResourceDiscoveryError{
			ProbeURL: probeURL,
			Status:   resp.StatusCode,
			cause:    fmt.Errorf("probe returned status %d", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, discoverProtectedResourceMaxBodyBytes))
	if err != nil {
		return OAuthProtectedResourceMetadata{}, &ProtectedResourceDiscoveryError{
			ProbeURL: probeURL,
			Status:   resp.StatusCode,
			cause:    fmt.Errorf("%w: %w", errReadProbeBody, err),
		}
	}

	var doc OAuthProtectedResourceMetadata
	if err := json.Unmarshal(body, &doc); err != nil {
		return OAuthProtectedResourceMetadata{}, &ProtectedResourceDiscoveryError{
			ProbeURL: probeURL,
			Status:   resp.StatusCode,
			cause:    fmt.Errorf("decode probe document: %w", err),
		}
	}

	return doc, nil
}

// protectedResourceProbeCandidates returns the ordered list of RFC 9728
// well-known URLs to probe for a given resource URL.
//
// Per RFC 9728 §3.1, when the resource URL has a non-empty path component the
// metadata document may live at either the path-style location
// ("<origin>/.well-known/oauth-protected-resource<path>") or the origin-style
// location ("<origin>/.well-known/oauth-protected-resource"). Both forms are
// returned in that order so callers can attempt path-style first and fall
// back to origin-style only on a 404. Resource URLs whose path is empty or
// "/" collapse to a single origin-style candidate.
func protectedResourceProbeCandidates(resourceURL string) ([]string, error) {
	u, err := url.Parse(resourceURL)
	if err != nil {
		return nil, fmt.Errorf("parse resource url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("resource url must use http or https scheme")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("resource url must include a host")
	}

	originStyle := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: OAuthProtectedResourcePath}).String()

	// Strip any trailing slash so "<origin>/" collapses to no path and only
	// origin-style is probed.
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" {
		return []string{originStyle}, nil
	}

	pathStyle := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: OAuthProtectedResourcePath + path}).String()
	return []string{pathStyle, originStyle}, nil
}

// collectProtectedResourceWarnings reports RFC 9728 deviations on the parsed
// metadata document. Warnings are informational; discovery never fails on
// these — the caller still receives a "metadata available" result so the
// dashboard can render whichever fields the upstream did supply.
func collectProtectedResourceWarnings(requestedResource string, doc OAuthProtectedResourceMetadata) []string {
	warnings := []string{}
	if doc.Resource == "" {
		warnings = append(warnings, "resource field missing from protected resource metadata")
	} else if !resourceURLsEquivalent(doc.Resource, requestedResource) {
		warnings = append(warnings, fmt.Sprintf("protected resource metadata resource %q does not match requested %q", doc.Resource, requestedResource))
	}
	if len(doc.AuthorizationServers) == 0 {
		warnings = append(warnings, "authorization_servers missing or empty in protected resource metadata")
	}
	return warnings
}

// resourceURLsEquivalent compares two resource URLs for RFC 9728 §3.3
// equivalence purposes, treating trailing slashes as insignificant. We don't
// canonicalise scheme case or host case because real upstreams already do; the
// trailing slash is the common deviation worth tolerating without a warning.
func resourceURLsEquivalent(a, b string) bool {
	au, aerr := url.Parse(a)
	bu, berr := url.Parse(b)
	if aerr != nil || berr != nil {
		return a == b
	}
	au.Path = strings.TrimSuffix(au.Path, "/")
	bu.Path = strings.TrimSuffix(bu.Path, "/")
	return au.String() == bu.String()
}
