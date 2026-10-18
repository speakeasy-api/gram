// Package orghost resolves the base URL of the URLs Speakeasy renders for an
// organization without an inbound request to follow: emails, Slack messages,
// and background jobs. Request-driven URLs follow the request's platform
// origin instead.
//
// An organization's organization_metadata.default_host names the platform host
// those URLs use. NULL means the legacy host, which stays fixed when the
// canonical host moves, so existing organizations keep their links. A stored
// value is re-checked on every read: one that is no longer the server URL's
// host or a configured platform host falls back to the legacy host.
package orghost

import (
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// Config configures a Resolver.
type Config struct {
	// ServerURL is the public URL of the server (GRAM_SERVER_URL).
	ServerURL *url.URL

	// SiteURL is the dashboard URL (GRAM_SITE_URL). It is nil when the process
	// has none configured, and then dashboard links of organizations on the
	// legacy host are not rendered.
	SiteURL *url.URL

	// PlatformHosts maps each extra platform host to its base URL, as
	// customdomains.ParsePlatformHosts returns it.
	PlatformHosts map[string]string

	// LegacyDefaultHost is the origin a NULL default_host resolves to. Nil
	// keeps the configured URLs: SiteURL for dashboard links and ServerURL for
	// server routes.
	LegacyDefaultHost *url.URL

	// NewOrganizationDefaultHost is the origin recorded on organizations
	// created from now on. Nil records none, so they use the legacy host.
	NewOrganizationDefaultHost *url.URL
}

// Resolver resolves organizations' request-less base URLs.
type Resolver struct {
	cfg        Config
	serverHost string
}

// New returns a Resolver for cfg.
func New(cfg Config) *Resolver {
	serverHost := ""
	if cfg.ServerURL != nil {
		// An unparseable server host matches no stored value, so every
		// organization falls back to the legacy host.
		serverHost, _ = requestorigin.CanonicalHost(cfg.ServerURL.Host)
	}
	return &Resolver{cfg: cfg, serverHost: serverHost}
}

// NewOrganizationDefaultHost is the default_host value to write when an
// organization is created.
func (r *Resolver) NewOrganizationDefaultHost() pgtype.Text {
	if r.cfg.NewOrganizationDefaultHost == nil {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: r.cfg.NewOrganizationDefaultHost.String(), Valid: true}
}

// SiteURL returns the dashboard base URL for an organization with the given
// default_host. It returns nil only when the organization uses the legacy host
// and no dashboard URL is configured.
func (r *Resolver) SiteURL(defaultHost pgtype.Text) *url.URL {
	return r.resolve(defaultHost, r.cfg.SiteURL)
}

// ServerURL returns the base URL of server routes, such as the invitation
// callback, for an organization with the given default_host.
func (r *Resolver) ServerURL(defaultHost pgtype.Text) *url.URL {
	return r.resolve(defaultHost, r.cfg.ServerURL)
}

// StoredPlatformHost returns the server and dashboard base URLs of the platform
// host an organization's stored default_host names. ok is false when the value
// is NULL, no longer names the server URL's host or a configured platform host,
// or names the server host while no dashboard URL is configured. Callers that
// move a browser to the organization's host use it so that they never act on
// the legacy fallback.
func (r *Resolver) StoredPlatformHost(defaultHost pgtype.Text) (serverURL, siteURL *url.URL, ok bool) {
	host, ok := storedHost(defaultHost)
	if !ok {
		return nil, nil, false
	}
	if host == r.serverHost {
		if r.cfg.ServerURL == nil || r.cfg.SiteURL == nil {
			return nil, nil, false
		}
		return clone(r.cfg.ServerURL), clone(r.cfg.SiteURL), true
	}
	baseURL, ok := r.cfg.PlatformHosts[host]
	if !ok {
		return nil, nil, false
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, nil, false
	}
	return parsed, clone(parsed), true
}

func (r *Resolver) resolve(defaultHost pgtype.Text, configured *url.URL) *url.URL {
	if host, ok := storedHost(defaultHost); ok {
		// The server URL's own host keeps the configured URL, so a local
		// dashboard URL that differs from the server URL still applies.
		if host == r.serverHost {
			return clone(configured)
		}
		if baseURL, ok := r.cfg.PlatformHosts[host]; ok {
			if parsed, err := url.Parse(baseURL); err == nil {
				return parsed
			}
		}
	}
	if r.cfg.LegacyDefaultHost != nil {
		return clone(r.cfg.LegacyDefaultHost)
	}
	return clone(configured)
}

// storedHost returns the canonical host of a stored default_host. The column
// holds an origin, but a bare host is accepted too. Anything more than an
// origin (userinfo, a path, a query, or a fragment) is rejected rather than
// trimmed, so a malformed value falls back to the legacy host.
func storedHost(defaultHost pgtype.Text) (string, bool) {
	if !defaultHost.Valid || defaultHost.String == "" {
		return "", false
	}
	raw := defaultHost.String
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
			parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") ||
			parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") {
			return "", false
		}
		raw = parsed.Host
	}
	host, err := requestorigin.CanonicalHost(raw)
	if err != nil {
		return "", false
	}
	return host, true
}

func clone(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	copied := *u
	return &copied
}

// IsPlatformHost reports whether host is the server URL's host or a configured
// extra platform host, and returns its configured base URL if so.
func (r *Resolver) IsPlatformHost(host string) (baseURL string, ok bool) {
	canonical, err := requestorigin.CanonicalHost(host)
	if err != nil {
		return "", false
	}
	if canonical == r.serverHost {
		if r.cfg.ServerURL == nil {
			return "", false
		}
		return r.cfg.ServerURL.String(), true
	}
	if baseURL, ok := r.cfg.PlatformHosts[canonical]; ok {
		return baseURL, true
	}
	return "", false
}
