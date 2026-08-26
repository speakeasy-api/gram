// Package hosts is the single source of truth for the hosts a Gram deployment
// answers on and the host every rendered URL should carry.
//
// A deployment serves three kinds of host:
//
//   - Platform hosts. First-party hosts Gram itself owns (app.getgram.ai,
//     ai.speakeasy.com). Every one of them is equally first-party: no
//     custom-domain lookup, no per-org scoping. One of them is the canonical
//     host, the fallback whenever nothing better is known.
//   - Custom domains. Customer-owned hosts, verified and activated per
//     organization. An 'app'-scoped one may serve the full control plane; an
//     'mcp'-scoped one serves MCP endpoints only.
//   - The outbound callback host. Pinned separately from all of the above,
//     because it appears in upstream OAuth registrations we do not control.
//
// Resolve is how a caller turns "which org is this for, and where did the
// request come from" into the host to render URLs with. Everything else here
// is deployment configuration.
package hosts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	domainsRepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// Hosts answers host questions for one deployment. Construct it once at
// startup with New and share it; it holds no per-request state.
type Hosts struct {
	logger *slog.Logger
	db     *pgxpool.Pool

	// canonical and outboundCallback keep whatever base path they were
	// configured with. Callers join onto them, so dropping a configured prefix
	// would silently move every URL rendered from them.
	canonical        *url.URL
	outboundCallback *url.URL

	// platform holds scheme+host only: it exists to answer host membership,
	// where a path would never match a Host header.
	platform []*url.URL
}

// New builds the host model for a deployment.
//
// canonical is the deployment's primary host (GRAM_SERVER_URL). platform is
// every additional first-party host the deployment answers on. outboundCallback
// is the host upstream OAuth providers redirect back to.
//
// The canonical host and the outbound callback host are always platform hosts,
// whether or not they appear in the list. The callback host has to be: Gram
// serves the callback route there, and a host that is not first-party is
// rejected by the custom-domain middleware, which would 403 every upstream
// redirect back into a remote login.
func New(logger *slog.Logger, db *pgxpool.Pool, canonical *url.URL, platform []*url.URL, outboundCallback *url.URL) (*Hosts, error) {
	if db == nil {
		return nil, errors.New("hosts: database pool is required")
	}
	if err := validate("canonical url", canonical); err != nil {
		return nil, err
	}
	if err := validate("outbound callback url", outboundCallback); err != nil {
		return nil, err
	}

	all := []*url.URL{origin(canonical)}
	add := func(u *url.URL) {
		if o := origin(u); !slices.ContainsFunc(all, func(known *url.URL) bool { return known.Host == o.Host }) {
			all = append(all, o)
		}
	}
	add(outboundCallback)
	for _, u := range platform {
		if err := validate("platform host", u); err != nil {
			return nil, err
		}
		add(u)
	}

	return &Hosts{
		logger:           logger.With(attr.SlogComponent("hosts")),
		db:               db,
		canonical:        base(canonical),
		outboundCallback: base(outboundCallback),
		platform:         all,
	}, nil
}

// NewFromConfig builds the host model from the raw flag values the server and
// worker commands carry, so the two stay in step as the model grows. An empty
// outboundCallbackRaw follows the canonical host, which is what a single-host
// deployment wants.
func NewFromConfig(logger *slog.Logger, db *pgxpool.Pool, canonical *url.URL, platformHostsRaw, outboundCallbackRaw string) (*Hosts, error) {
	platform, err := ParseList(platformHostsRaw)
	if err != nil {
		return nil, err
	}

	outboundCallback := canonical
	if outboundCallbackRaw != "" {
		outboundCallback, err = url.Parse(outboundCallbackRaw)
		if err != nil {
			return nil, fmt.Errorf("parse outbound callback url %q: %w", outboundCallbackRaw, err)
		}
	}

	return New(logger, db, canonical, platform, outboundCallback)
}

// validate rejects a URL that cannot be rendered as an absolute public one. A
// scheme-relative URL such as //app.getgram.ai parses without error and has a
// host, so the host check alone would let it through, and a non-HTTP scheme
// would render a URL no browser or Authorization Server can follow.
func validate(what string, u *url.URL) error {
	switch {
	case u == nil || u.Host == "":
		return fmt.Errorf("hosts: %s is required", what)
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("hosts: %s %q must be an http or https url", what, u.String())
	case u.User != nil:
		// Credentials in a configured URL would be copied into every URL
		// rendered from it, including ones sent to upstream providers.
		return fmt.Errorf("hosts: %s must not carry userinfo", what)
	default:
		return nil
	}
}

// ParseList parses a comma-separated list of platform host URLs, as the
// platform-hosts flag carries them. An empty string yields no hosts, which New
// reads as "the canonical host alone".
func ParseList(raw string) ([]*url.URL, error) {
	var out []*url.URL
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		u, err := url.Parse(part)
		if err != nil {
			return nil, fmt.Errorf("parse platform host %q: %w", part, err)
		}
		if err := validate("platform host", u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// Canonical is the deployment's primary host: the fallback for every URL that
// has no better host to render with. Returned with its configured base path
// intact, so callers can join onto it.
func (h *Hosts) Canonical() *url.URL { return clone(h.canonical) }

// PlatformHosts is every first-party host the deployment answers on, canonical
// first. Requests arriving on any of them skip the custom-domain lookup.
func (h *Hosts) PlatformHosts() []*url.URL {
	out := make([]*url.URL, 0, len(h.platform))
	for _, u := range h.platform {
		out = append(out, clone(u))
	}
	return out
}

// Auth is the host that serves login, session, and IdP callback routes. Pinned
// to the canonical host: the auth surface is single-homed so session cookies
// and the IdP's registered redirect URIs stay on one origin. Those redirect
// URIs are ours to change, so this follows the canonical host rather than
// being configured separately — unlike OutboundCallback.
func (h *Hosts) Auth() *url.URL { return clone(h.canonical) }

// OutboundCallback is the host Gram sends upstream OAuth providers as its own
// redirect target and publishes as its CIMD client_id.
//
// It is configured independently of the canonical host and does not follow it.
// The value is registered on customer-owned OAuth apps and vendor allowlists
// (Figma, Canva, ...) that we cannot migrate, so moving the canonical host must
// not move this one. Handlers still serve on every platform host; only the
// advertised URL is pinned.
func (h *Hosts) OutboundCallback() *url.URL { return clone(h.outboundCallback) }

// IsPlatform reports whether a request host (the Host header, so possibly with
// a port) is one of the deployment's first-party hosts. Hosts are compared
// case-insensitively: a Host header is not required to be lowercase, and
// rejecting a mixed-case one would drop a first-party request into the
// custom-domain lookup and 403 it.
func (h *Hosts) IsPlatform(host string) bool {
	if host == "" {
		return false
	}
	host = strings.ToLower(host)
	return slices.ContainsFunc(h.platform, func(u *url.URL) bool { return u.Host == host })
}

// Resolve is the host to render customer-facing URLs with for an organization,
// in preference order:
//
//  1. The host the request arrived on, when the organization may be served
//     there — any platform host, or the org's own active app-scoped domain.
//  2. The organization's configured default host, subject to the same rule.
//  3. The canonical host.
//
// The validity rule is re-checked on every call rather than trusted from the
// stored column, so an organization whose app-scoped domain was deleted or
// deactivated falls back to the canonical host on the next request.
//
// req may be nil for callers with no request in hand (background work), which
// simply skips step 1.
func (h *Hosts) Resolve(ctx context.Context, req *http.Request, organizationID string) *url.URL {
	appDomainLoaded := false
	var appDomain string
	servable := func(host string) bool {
		if host == "" {
			return false
		}
		if h.IsPlatform(host) {
			return true
		}
		if organizationID == "" {
			return false
		}
		if !appDomainLoaded {
			appDomain = h.appScopedDomain(ctx, organizationID)
			appDomainLoaded = true
		}
		return appDomain != "" && strings.EqualFold(appDomain, host)
	}

	if req != nil && servable(req.Host) {
		return h.at(req.Host)
	}

	if organizationID != "" {
		if def := h.defaultHost(ctx, organizationID); servable(def) {
			return h.at(def)
		}
	}

	return h.Canonical()
}

// at renders a host as a URL on the canonical scheme and base path. Platform
// hosts and custom domains share the deployment's scheme (https everywhere but
// local dev) and its path prefix, so there is nothing per-host to carry.
func (h *Hosts) at(host string) *url.URL {
	rendered := *h.canonical
	rendered.Host = strings.ToLower(host)
	return &rendered
}

// defaultHost is the organization's configured default host, empty when unset
// or unreadable. A failed read is logged and degrades to the canonical host
// rather than failing the request: rendering a URL on the wrong first-party
// host is recoverable, refusing to render one is not.
func (h *Hosts) defaultHost(ctx context.Context, organizationID string) string {
	org, err := orgRepo.New(h.db).GetOrganizationMetadata(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ""
	case err != nil:
		h.logger.ErrorContext(ctx, "failed to read organization default host",
			attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return ""
	}
	if !org.DefaultHost.Valid {
		return ""
	}
	return org.DefaultHost.String
}

// appScopedDomain is the organization's active app-scoped custom domain, empty
// when it has none.
func (h *Hosts) appScopedDomain(ctx context.Context, organizationID string) string {
	domain, err := domainsRepo.New(h.db).GetActiveAppScopedCustomDomainForOrganization(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ""
	case err != nil:
		h.logger.ErrorContext(ctx, "failed to read organization app-scoped custom domain",
			attr.SlogOrganizationID(organizationID), attr.SlogError(err))
		return ""
	}
	return domain.Domain
}

// origin strips everything but scheme and host, for the platform-host set,
// where membership is answered against a Host header.
func origin(u *url.URL) *url.URL {
	return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host)}
}

// base keeps scheme, host, and path. A query or fragment on a configured URL
// would end up before the path every caller joins onto, so the callback path
// would land after it and the redirect would break. RawPath rides along so a
// path holding an escaped character still renders the way it was configured.
func base(u *url.URL) *url.URL {
	return &url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host), Path: u.Path, RawPath: u.RawPath}
}

func clone(u *url.URL) *url.URL {
	c := *u
	return &c
}
