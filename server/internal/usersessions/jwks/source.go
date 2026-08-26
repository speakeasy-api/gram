package jwks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// maxJWKSURILength caps a remote key source URL in application code. The
// value arrives from outside and eventually feeds TEXT columns and log
// lines; 2 KB accommodates any real key set URL with wide margin.
const maxJWKSURILength = 2048

// sourceKind discriminates the Source sum type. The zero value is invalid so
// a zero Source cannot be resolved.
type sourceKind string

const (
	sourceInline sourceKind = "inline"
	sourceRemote sourceKind = "remote"
)

// Source identifies where a verification key set comes from: an inline JWK
// Set document (no fetch at all) or a remote HTTPS URL. Construct one with
// NewInlineSource or NewRemoteSource.
type Source struct {
	kind   sourceKind
	inline json.RawMessage
	uri    string
	origin string

	// fetchScope names the party whose fetch budget an upstream consult of
	// this source is charged to, when the KeyResolver has a fetch limiter.
	// Empty means the shared unscoped budget.
	fetchScope string
}

// WithFetchScope returns a copy of the source whose upstream consults are
// charged to scope's fetch budget rather than the shared one. The scope
// plays no part in caching; the key set is still shared across every scope
// that names the same URL.
func (s Source) WithFetchScope(scope string) Source {
	s.fetchScope = scope
	return s
}

// NewInlineSource returns a Source backed by an inline JWK Set document, such
// as the client_jwks column captured from a registration request. Resolving
// it never fetches, so it has no cache key and an unknown kid is terminal.
// The document is parsed and screened at resolve time, not here.
func NewInlineSource(keySet json.RawMessage) (Source, error) {
	if len(keySet) == 0 {
		return Source{kind: "", inline: nil, uri: "", origin: "", fetchScope: ""}, errors.New("inline key set is empty")
	}
	return Source{kind: sourceInline, inline: keySet, uri: "", origin: "", fetchScope: ""}, nil
}

// NewRemoteSource returns a Source for a jwks_uri, whether it came from a
// client's metadata or a trusted issuer's discovery document. Deliberately
// no origin rule relates the URL to the party that published it: neither
// RFC 7591 (client metadata) nor RFC 8414 (issuer metadata) constrains
// jwks_uri beyond the https scheme, real deployments cross hosts (Google
// publishes issuer accounts.google.com with a jwks_uri on
// www.googleapis.com; platform-hosted client documents name key sets on the
// client's own domain), and the CIMD draft's §8.1 explicitly preserves
// unrestricted relationships between a document's URLs. The binding comes
// from declaration instead: the authenticated document that names the
// jwks_uri (the client metadata document at the client_id URL, or issuer
// metadata whose issuer RFC 8414 §3.3 requires to match the well-known URL)
// is what vouches for it, wherever it is hosted — and a signature only
// verifies for the party actually holding the private keys.
func NewRemoteSource(jwksURI string) (Source, error) {
	parsed, err := parseJWKSURI(jwksURI)
	if err != nil {
		return Source{kind: "", inline: nil, uri: "", origin: "", fetchScope: ""}, err
	}
	return Source{kind: sourceRemote, inline: nil, uri: jwksURI, origin: parsed.Host, fetchScope: ""}, nil
}

// ValidateURI reports whether a jwks_uri satisfies the syntax every remote
// key source must satisfy, without building a Source from it.
//
// The rules are NewRemoteSource's, and stay the same on purpose: no
// specification narrows jwks_uri beyond the https scheme, so registration
// applies no extra policy of its own.
func ValidateURI(jwksURI string) error {
	_, err := parseJWKSURI(jwksURI)
	return err
}

// CacheKey is the storage key for this source's resolved key set: the
// jwks_uri itself. Keying by URL rather than by consumer row is deliberate —
// OpenAI publishes one jwks_uri shared by every per-connector client_id, so
// per-consumer storage would refetch the same document once per referrer.
// Sharing the entry across consumers is safe because it records only what
// the URL serves: referencing a key set grants nothing without its private
// keys. Empty for inline sources, which never fetch and are never cached.
func (s Source) CacheKey() string {
	return s.uri
}

// parseJWKSURI enforces the syntax every remote key source must satisfy
// before its URL is allowed anywhere near an outbound fetch: absolute https,
// a host, and none of the components (userinfo, fragment) that have no
// business in a published key set URL. The guardian dialer separately blocks
// internal targets at fetch time; this check is about rejecting junk early
// with an actionable error.
func parseJWKSURI(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("jwks_uri is empty")
	}
	if len(raw) > maxJWKSURILength {
		return nil, fmt.Errorf("jwks_uri exceeds the %d byte limit", maxJWKSURILength)
	}
	// Detect fragments on the raw string so an empty "#" is caught too.
	if strings.Contains(raw, "#") {
		return nil, errors.New("jwks_uri must not contain a fragment")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("jwks_uri is not a valid URL")
	}
	if parsed.Scheme != "https" {
		return nil, errors.New("jwks_uri must use the https scheme")
	}
	if parsed.User != nil {
		return nil, errors.New("jwks_uri must not contain a userinfo component")
	}
	// Hostname rather than Host: a port-only authority like https://:443
	// leaves Host non-empty while naming no host at all.
	if parsed.Hostname() == "" {
		return nil, errors.New("jwks_uri must include a host")
	}
	return parsed, nil
}
