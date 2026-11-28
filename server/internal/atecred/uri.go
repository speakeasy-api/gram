// Package atecred implements the ATE egress credential-provider contract,
// provider-to-server Google OIDC authentication, and the internal Speakeasy
// mint handler for on-demand Kit turn credentials.
package atecred

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// ProviderClass is the DNS provider class name configured on the ATE egress
	// gateway (--credential-provider='{"name":"speakeasy.com",...}').
	ProviderClass = "speakeasy.com"

	// URIScheme is the credential URI scheme enforced by ATE egress policies.
	URIScheme = "ate-secret"

	// MintEndpointPath is the internal HTTP path on gram start that mints
	// short-lived turn credentials for live ATE thread actors.
	MintEndpointPath = "/internal/assistants/ate/credentials"

	// DefaultInjectorIdentity is the default SPIFFE URI SAN presented by ATE's
	// egress gateway when dialing a credential provider over mTLS.
	DefaultInjectorIdentity = "spiffe://cluster.local/ns/ate-system/sa/atenet-egress"

	// ActorSPIFFETrustDomain is the SPIFFE trust domain ATE uses for actor certs.
	ActorSPIFFETrustDomain = "substrate-actor.local"

	// DefaultListenAddress is the default gRPC mTLS listen address matching ATE's
	// reference credential provider layout.
	DefaultListenAddress = ":50051"

	// DefaultHealthAddress is the default HTTP /healthz, /livez, /readyz address.
	DefaultHealthAddress = ":9091"

	// DefaultCacheTTL bounds how long the credential provider reuses a minted
	// credential in memory before requesting a fresh one from the server.
	DefaultCacheTTL = time.Minute

	// DefaultRequestTimeout bounds one mint HTTP call from the provider to the
	// Speakeasy server so it stays within ATE's 4-second egress injection budget.
	DefaultRequestTimeout = 3 * time.Second

	// MintedCredentialLifetime is the lifetime of a turn credential minted for
	// on-demand injection by the ATE credential provider.
	MintedCredentialLifetime = 5 * time.Minute

	// maxDNSLabelLen is the RFC 1123 DNS-label length limit for ATE names.
	maxDNSLabelLen = 63

	// maxActorUIDLen bounds actor UIDs carried in credential URIs and requests.
	maxActorUIDLen = 128

	// uriVersionSegment is the versioned path prefix inside Speakeasy credential URIs.
	uriVersionSegment = "v1"

	// uriActorsSegment names the actors collection inside Speakeasy credential URIs.
	uriActorsSegment = "actors"
)

var (
	ateNamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	actorUIDChars  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// ActorRef identifies one concrete live ATE actor generation.
type ActorRef struct {
	// Atespace is the ATE namespace owning the actor.
	Atespace string

	// ActorName is the DNS-label name of the actor within Atespace.
	ActorName string

	// ActorUID is the unique server-assigned generation UID of the actor.
	ActorUID string
}

// MintRequest is the contractual JSON body sent by the ATE credential provider
// to POST /internal/assistants/ate/credentials.
type MintRequest struct {
	// Atespace is the ATE namespace verified from both the SPIFFE ID and URI.
	Atespace string `json:"atespace"`

	// ActorName is the actor name verified from both the SPIFFE ID and URI.
	ActorName string `json:"actor_name"`

	// ActorUID is the actor generation UID parsed from the credential URI.
	ActorUID string `json:"actor_uid"`
}

// MintResponse is the contractual JSON body returned by
// POST /internal/assistants/ate/credentials.
type MintResponse struct {
	// Credential is the minted Speakeasy principal credential JWT.
	Credential string `json:"credential"`

	// ExpiresAt is the RFC3339 expiration timestamp of Credential.
	ExpiresAt time.Time `json:"expires_at"`
}

// ValidATEName reports whether name is a valid RFC 1123 DNS label for an ATE
// atespace or actor name.
func ValidATEName(name string) bool {
	return len(name) > 0 && len(name) <= maxDNSLabelLen && ateNamePattern.MatchString(name)
}

// ValidActorUID reports whether uid is a safe, non-empty ATE actor UID token
// suitable for a single URI path segment.
func ValidActorUID(uid string) bool {
	return len(uid) > 0 && len(uid) <= maxActorUIDLen && actorUIDChars.MatchString(uid)
}

// BuildCredentialURI builds the exact Speakeasy credential URI for an ATE actor:
//
//	ate-secret://speakeasy.com/v1/actors/<atespace>/<actor-name>/<actor-uid>
func BuildCredentialURI(atespace, actorName, actorUID string) (string, error) {
	if !ValidATEName(atespace) {
		return "", fmt.Errorf("invalid ATE atespace %q", atespace)
	}
	if !ValidATEName(actorName) {
		return "", fmt.Errorf("invalid ATE actor name %q", actorName)
	}
	if !ValidActorUID(actorUID) {
		return "", fmt.Errorf("invalid ATE actor UID %q", actorUID)
	}
	return fmt.Sprintf("%s://%s/%s/%s/%s/%s/%s",
		URIScheme, ProviderClass, uriVersionSegment, uriActorsSegment, atespace, actorName, actorUID), nil
}

// ParseCredentialURI strictly parses a Speakeasy ATE credential URI.
func ParseCredentialURI(raw string) (ActorRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return ActorRef{}, fmt.Errorf("parse credential URI: %w", err)
	}
	if u.Scheme != URIScheme {
		return ActorRef{}, fmt.Errorf("credential URI scheme %q is not %q", u.Scheme, URIScheme)
	}
	if u.Host != ProviderClass {
		return ActorRef{}, fmt.Errorf("credential URI provider %q is not %q", u.Host, ProviderClass)
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return ActorRef{}, errors.New("credential URI must not contain userinfo, query, or fragment")
	}
	if u.EscapedPath() != u.Path {
		return ActorRef{}, errors.New("credential URI path must not contain percent-encoding")
	}

	trimmed := strings.TrimPrefix(u.Path, "/")
	if trimmed == "" || strings.HasSuffix(trimmed, "/") {
		return ActorRef{}, errors.New("credential URI path has invalid trailing slash")
	}
	segments := strings.Split(trimmed, "/")
	if len(segments) != 5 || segments[0] != uriVersionSegment || segments[1] != uriActorsSegment {
		return ActorRef{}, errors.New("credential URI path must match /v1/actors/<atespace>/<actor-name>/<actor-uid>")
	}
	atespace, actorName, actorUID := segments[2], segments[3], segments[4]
	if !ValidATEName(atespace) || !ValidATEName(actorName) || !ValidActorUID(actorUID) {
		return ActorRef{}, errors.New("credential URI contains invalid actor reference segments")
	}
	return ActorRef{
		Atespace:  atespace,
		ActorName: actorName,
		ActorUID:  actorUID,
	}, nil
}

// ParseActorSPIFFEID parses an ATE actor SPIFFE URI of the exact form
// spiffe://substrate-actor.local/actor/<atespace>/<actor-name>. Worker-for-actor
// and non-actor SPIFFE IDs are rejected.
func ParseActorSPIFFEID(raw string) (atespace, actorName string, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse actor SPIFFE ID: %w", err)
	}
	if u.Scheme != "spiffe" || u.Host != ActorSPIFFETrustDomain || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", "", errors.New("actor SPIFFE ID must match spiffe://substrate-actor.local/actor/<atespace>/<actor-name>")
	}
	if u.EscapedPath() != u.Path {
		return "", "", errors.New("actor SPIFFE ID path must not contain percent-encoding")
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(segments) != 3 || segments[0] != "actor" {
		return "", "", errors.New("actor SPIFFE ID must match spiffe://substrate-actor.local/actor/<atespace>/<actor-name>")
	}
	if !ValidATEName(segments[1]) || !ValidATEName(segments[2]) {
		return "", "", errors.New("actor SPIFFE ID contains invalid atespace or actor name")
	}
	return segments[1], segments[2], nil
}

// MintEndpointURL derives the exact internal mint endpoint URL from the
// Speakeasy API server base URL.
func MintEndpointURL(serverURL *url.URL) (string, error) {
	if serverURL == nil || (serverURL.Scheme != "https" && serverURL.Scheme != "http") || serverURL.Host == "" {
		return "", errors.New("speakeasy server URL must include scheme and host")
	}
	base := *serverURL
	base.Path = ""
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	base.User = nil
	return strings.TrimRight(base.String(), "/") + MintEndpointPath, nil
}
