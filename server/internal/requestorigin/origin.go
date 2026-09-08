package requestorigin

import (
	"context"
	"fmt"
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
