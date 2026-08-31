package networkingress

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const (
	NamespacePlatform     = "platform"
	NamespaceCustomDomain = "custom_domain"
)

// Authority is the non-sensitive, provider-neutral request authority pinned to
// short-lived OAuth state. It intentionally excludes advisory network identity
// and provider credentials.
type Authority struct {
	Surface          requestorigin.Surface `json:"surface,omitempty"`
	BaseURL          string                `json:"base_url,omitempty"`
	OrganizationID   string                `json:"organization_id,omitempty"`
	NetworkIngressID uuid.UUID             `json:"network_ingress_id,omitempty"`
	NamespaceKind    string                `json:"namespace_kind,omitempty"`
	CustomDomainID   uuid.NullUUID         `json:"custom_domain_id,omitzero"`
}

// FromRequest captures the mint-time authority already established by request
// middleware and the resolved endpoint namespace. Absence of Origin preserves
// TTL-bounded compatibility for internal callers and pre-field cached states.
func FromRequest(ctx context.Context, baseURL, organizationID string, customDomainID uuid.NullUUID) Authority {
	authority := Authority{
		Surface:          requestorigin.SurfacePlatform,
		BaseURL:          baseURL,
		OrganizationID:   organizationID,
		NamespaceKind:    NamespacePlatform,
		NetworkIngressID: uuid.Nil,
		CustomDomainID:   customDomainID,
	}
	if customDomainID.Valid {
		authority.Surface = requestorigin.SurfaceCustomDomain
		authority.NamespaceKind = NamespaceCustomDomain
	}
	origin, ok := requestorigin.FromContext(ctx)
	if !ok {
		return authority
	}
	authority.Surface = origin.Surface
	if origin.OrganizationID != "" {
		authority.OrganizationID = origin.OrganizationID
	}
	authority.NetworkIngressID = origin.NetworkIngressID
	switch origin.Surface {
	case requestorigin.SurfacePlatform:
		authority.NamespaceKind = NamespacePlatform
		authority.CustomDomainID = uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	case requestorigin.SurfaceCustomDomain:
		authority.NamespaceKind = NamespaceCustomDomain
	case requestorigin.SurfacePrivateNetwork:
		// Replaced with the live ingress-pinned namespace by EndpointRef.
	default:
	}
	return authority
}

func (a Authority) IsPrivate() bool {
	return a.Surface == requestorigin.SurfacePrivateNetwork
}

// ValidateEndpointRef ensures the duplicated endpoint-address fields used for
// rolling compatibility agree with the authoritative namespace snapshot.
func (a Authority) ValidateEndpointRef(baseURL string, customDomainID uuid.NullUUID) error {
	if a.Surface == "" {
		return nil
	}
	if a.BaseURL != baseURL {
		return fmt.Errorf("OAuth endpoint origin is inconsistent")
	}
	if err := validateNamespace(a.NamespaceKind, a.CustomDomainID); err != nil {
		return err
	}
	switch a.Surface {
	case requestorigin.SurfacePlatform:
		if a.OrganizationID == "" || a.NetworkIngressID != uuid.Nil || a.NamespaceKind != NamespacePlatform {
			return fmt.Errorf("platform OAuth authority is inconsistent")
		}
	case requestorigin.SurfaceCustomDomain:
		if a.OrganizationID == "" || a.NetworkIngressID != uuid.Nil || a.NamespaceKind != NamespaceCustomDomain || a.CustomDomainID != customDomainID {
			return fmt.Errorf("custom-domain OAuth authority is inconsistent")
		}
	case requestorigin.SurfacePrivateNetwork:
		if a.OrganizationID == "" || a.NetworkIngressID == uuid.Nil || a.CustomDomainID != customDomainID {
			return fmt.Errorf("private OAuth authority is incomplete")
		}
	default:
		return fmt.Errorf("unknown OAuth origin surface %q", a.Surface)
	}
	return nil
}

// ValidateRequest requires a private continuation to arrive through the same
// attested ingress and externally visible origin. Public/custom continuations
// retain their existing endpoint checks.
func (a Authority) ValidateBaseURL(baseURL string) error {
	if a.IsPrivate() {
		canonical, err := canonicalPrivateBaseURL(baseURL)
		if err != nil || canonical != a.BaseURL {
			return fmt.Errorf("private OAuth request origin mismatch")
		}
		return nil
	}
	if a.BaseURL != "" && a.BaseURL != baseURL {
		return fmt.Errorf("OAuth request origin mismatch")
	}
	return nil
}

func (a Authority) ValidateRequest(ctx context.Context) error {
	if a.Surface == "" {
		return nil
	}
	origin, ok := requestorigin.FromContext(ctx)
	if !ok {
		// Internal callers and states minted before request-origin middleware keep
		// TTL-bounded compatibility, but no private authority can use this path.
		if a.IsPrivate() {
			return fmt.Errorf("private OAuth state requires private request authority")
		}
		return nil
	}
	if origin.Surface != a.Surface {
		return fmt.Errorf("OAuth request surface mismatch")
	}
	if a.IsPrivate() {
		if origin.NetworkIngressID == uuid.Nil || origin.NetworkIngressID != a.NetworkIngressID || origin.OrganizationID != a.OrganizationID {
			return fmt.Errorf("private OAuth request authority mismatch")
		}
	}
	if a.Surface == requestorigin.SurfaceCustomDomain && origin.OrganizationID != a.OrganizationID {
		return fmt.Errorf("custom-domain OAuth request authority mismatch")
	}
	return a.ValidateBaseURL(origin.BaseURL)
}

// ValidateLive re-loads the ingress before a global callback performs side
// effects. A deleted or disabled row is indistinguishable from a missing row.
// LoadRequestAuthority reconstructs private namespace authority from the live
// ingress identified by trusted request middleware. It is used by route adapters
// that need explicit private resolution before the private listener lands.
func LoadRequestAuthority(ctx context.Context, db *pgxpool.Pool) (Authority, error) {
	origin, ok := requestorigin.FromContext(ctx)
	if !ok || origin.Surface != requestorigin.SurfacePrivateNetwork {
		return Authority{}, fmt.Errorf("private request origin is unavailable")
	}
	row, err := repo.New(db).GetLiveNetworkIngressAuthority(ctx, origin.NetworkIngressID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Authority{}, fmt.Errorf("private network ingress is unavailable")
	}
	if err != nil {
		return Authority{}, fmt.Errorf("load private network ingress authority: %w", err)
	}
	baseURL, err := canonicalPrivateBaseURL(origin.BaseURL)
	if err != nil {
		return Authority{}, err
	}
	authority := Authority{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          baseURL,
		OrganizationID:   origin.OrganizationID,
		NetworkIngressID: origin.NetworkIngressID,
		NamespaceKind:    row.EndpointNamespaceKind,
		CustomDomainID:   row.CustomDomainID,
	}
	if err := authority.ValidateLive(ctx, db); err != nil {
		return Authority{}, err
	}
	return authority, nil
}

func (a Authority) ValidateLive(ctx context.Context, db *pgxpool.Pool) error {
	if !a.IsPrivate() {
		return nil
	}
	if a.NetworkIngressID == uuid.Nil || a.OrganizationID == "" || a.BaseURL == "" {
		return fmt.Errorf("private OAuth authority is incomplete")
	}
	if err := validateNamespace(a.NamespaceKind, a.CustomDomainID); err != nil {
		return err
	}

	row, err := repo.New(db).GetLiveNetworkIngressAuthority(ctx, a.NetworkIngressID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("private network ingress is unavailable")
	}
	if err != nil {
		return fmt.Errorf("load private network ingress authority: %w", err)
	}
	if row.OrganizationID != a.OrganizationID || row.EndpointNamespaceKind != a.NamespaceKind || row.CustomDomainID != a.CustomDomainID {
		return fmt.Errorf("private network ingress authority changed")
	}
	if expectedBaseURL(row.DnsName) != a.BaseURL {
		return fmt.Errorf("private network ingress origin changed")
	}
	return nil
}

func validateNamespace(kind string, customDomainID uuid.NullUUID) error {
	switch kind {
	case NamespacePlatform:
		if customDomainID.Valid {
			return fmt.Errorf("platform namespace cannot pin a custom domain")
		}
	case NamespaceCustomDomain:
		if !customDomainID.Valid || customDomainID.UUID == uuid.Nil {
			return fmt.Errorf("custom-domain namespace is incomplete")
		}
	default:
		return fmt.Errorf("unknown endpoint namespace kind %q", kind)
	}
	return nil
}

func canonicalPrivateBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("private network ingress origin is invalid")
	}
	if u.Port() != "" {
		return "", fmt.Errorf("private network ingress origin must not include a port")
	}
	host, err := requestorigin.CanonicalHost(u.Host)
	if err != nil {
		return "", fmt.Errorf("canonicalize private network ingress origin: %w", err)
	}
	return (&url.URL{Scheme: "https", Host: host}).String(), nil
}

func expectedBaseURL(dnsName pgtype.Text) string {
	if !dnsName.Valid {
		return ""
	}
	host := strings.TrimSpace(dnsName.String)
	if host == "" {
		return ""
	}
	baseURL, err := canonicalPrivateBaseURL((&url.URL{Scheme: "https", Host: strings.ToLower(strings.TrimSuffix(host, "."))}).String())
	if err != nil {
		return ""
	}
	return baseURL
}
