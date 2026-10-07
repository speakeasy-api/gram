package requestorigin

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type Surface string

const (
	SurfacePlatform       Surface = "platform"
	SurfaceCustomDomain   Surface = "custom_domain"
	SurfacePrivateNetwork Surface = "private_network"
)

// NetworkIdentity is advisory identity supplied by a private-network provider.
// It is not a Gram principal or an authorization grant.
type NetworkIdentity struct {
	Login string
	Name  string
}

type Origin struct {
	Surface          Surface
	BaseURL          string
	OrganizationID   string
	NetworkIngressID uuid.UUID
	NetworkIdentity  *NetworkIdentity
}

type contextKey struct{}

func WithContext(ctx context.Context, origin Origin) context.Context {
	return context.WithValue(ctx, contextKey{}, origin)
}

func FromContext(ctx context.Context) (Origin, bool) {
	origin, ok := ctx.Value(contextKey{}).(Origin)
	return origin, ok
}

func BaseURL(ctx context.Context, fallback string) string {
	if origin, ok := FromContext(ctx); ok && origin.BaseURL != "" {
		return origin.BaseURL
	}
	return fallback
}

// PlatformHostBaseURL returns the base URL of the extra platform host (see
// GRAM_PLATFORM_HOSTS) the request arrived on, and fallback for every other
// request. Session cookies are host-only, so browser redirects must stay on
// the platform host the user is signed in to. Only requests the custom-domains
// middleware classified as platform qualify, never the raw Host header, and a
// request on serverURL itself gets fallback so its behaviour, including local
// site URL overrides, is unchanged.
func PlatformHostBaseURL(ctx context.Context, serverURL, fallback string) string {
	origin, ok := FromContext(ctx)
	if !ok || origin.Surface != SurfacePlatform || origin.BaseURL == "" {
		return fallback
	}
	if strings.TrimRight(origin.BaseURL, "/") == strings.TrimRight(serverURL, "/") {
		return fallback
	}
	return origin.BaseURL
}

// HTTPSBaseURL returns a canonical externally visible HTTPS origin for a host
// that has already passed the same authority validation used for request routing.
func HTTPSBaseURL(rawHost string) (string, error) {
	host := strings.ToLower(rawHost)
	if net.ParseIP(host) == nil {
		var err error
		host, err = CanonicalHost(rawHost)
		if err != nil {
			return "", err
		}
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: "https", Host: host}).String(), nil
}

// CanonicalHost returns the lowercase hostname used for request routing. A
// syntactically valid port is discarded. Trailing dots and ambiguous Host
// forms are rejected rather than normalized so authorization and emitted URLs
// cannot disagree about the request authority.
func CanonicalHost(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", fmt.Errorf("invalid host")
	}
	if strings.ContainsAny(raw, "/\\@,?#") {
		return "", fmt.Errorf("invalid host")
	}

	u, err := url.Parse("https://" + raw)
	if err != nil || u.Host != raw || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid host")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.HasSuffix(host, ".") {
		return "", fmt.Errorf("invalid host")
	}

	port := u.Port()
	if strings.HasSuffix(raw, ":") {
		return "", fmt.Errorf("invalid host port")
	}
	if port != "" {
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil || value == 0 {
			return "", fmt.Errorf("invalid host port")
		}
	}
	return host, nil
}

// URLOrigin is the lowercased scheme://host[:port] of raw, with the scheme's
// default port dropped, or "" when raw is not an absolute URL or carries
// userinfo.
func URLOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" && (scheme != "https" || port != "443") && (scheme != "http" || port != "80") {
		return scheme + "://" + net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}
