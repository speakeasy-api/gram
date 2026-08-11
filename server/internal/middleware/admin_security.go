package middleware

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// AdminCORS allows the configured set of origins to make credentialed
// cross-origin requests to the admin service. Unlike CORSMiddleware which is
// pinned to a single server URL, this variant takes an explicit allowlist so
// the admin web UI (e.g. https://admin.speakeasy.com) can be served from a
// different origin than the admin API (e.g. https://gram-admin.speakeasy.com).
func AdminCORS(allowedOrigins []string) func(http.Handler) http.Handler {
	set := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o != "" {
			set[o] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := set[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Authorization, User-Agent")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AdminOriginCheck rejects any unsafe HTTP method whose Origin (or Referer if
// Origin is absent) is neither in the allowlist nor the host the request
// arrived on. CSRF defence for cookie-based admin auth when SameSite=None is
// required for cross-origin web UIs.
//
// The allowlist covers the split deployment, where the admin web UI is served
// from a different origin than the admin API. The same-host rule covers the
// single-origin deployment, where the UI and the API share one ingress: a
// browser sends Origin on every unsafe method, including a same-origin one,
// so an empty allowlist would otherwise reject every write.
func AdminOriginCheck(allowedOrigins []string) func(http.Handler) http.Handler {
	set := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o != "" {
			set[o] = struct{}{}
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				if ref := r.Header.Get("Referer"); ref != "" {
					if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
						origin = u.Scheme + "://" + u.Host
					}
				}
			}

			if _, ok := set[origin]; ok {
				next.ServeHTTP(w, r)
				return
			}

			if sameHost(r, origin) {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "forbidden: origin not allowed", http.StatusForbidden)
		})
	}
}

// sameHost reports whether origin names the host the request arrived on.
//
// Only the host is compared. Behind a TLS-terminating ingress the server sees
// a plain HTTP request, so the scheme it observes says nothing about the
// scheme the browser used.
//
// X-Forwarded-Host wins when present, because an ingress that rewrites Host to
// an internal service name leaves the public host only in that header. A
// browser cannot forge either header across sites: it sets Host itself, and a
// cross-site request that carries X-Forwarded-Host needs a preflight, which
// AdminCORS answers for allowlisted origins only.
func sameHost(r *http.Request, origin string) bool {
	if origin == "" {
		return false
	}

	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	// A comma separated chain means several proxies appended to the header.
	// The first entry is the host the browser asked for.
	host, _, _ = strings.Cut(host, ",")

	return strings.EqualFold(strings.TrimSpace(host), u.Host)
}

// AdminCookieAttributes rewrites Set-Cookie response headers for the admin
// session cookies (gram_admin, gram_admin_login_state, gram_admin_override),
// switching SameSite to None and forcing Secure so the cookie rides on
// cross-origin fetches from the admin web UI. Optionally adds Domain= when
// cookieDomain is non-empty (production cross-subdomain setup); leaves Domain
// unset when empty (localhost cross-port setup where Domain is unnecessary).
//
// Disabled when crossSite is false — preserves Goa-generated defaults for
// configurations that do not need cross-origin admin UIs.
func AdminCookieAttributes(crossSite bool, cookieDomain string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !crossSite {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&adminCookieRewriter{ResponseWriter: w, domain: cookieDomain, wroteHeader: false}, r)
		})
	}
}

type adminCookieRewriter struct {
	http.ResponseWriter
	domain      string
	wroteHeader bool
}

func (c *adminCookieRewriter) WriteHeader(code int) {
	if !c.wroteHeader {
		c.rewrite()
		c.wroteHeader = true
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *adminCookieRewriter) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.rewrite()
		c.wroteHeader = true
	}
	n, err := c.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("write admin response: %w", err)
	}
	return n, nil
}

// Flush must be forwarded explicitly — the embedded interface hides the
// underlying writer's Flush from type asserts, breaking streaming. Flushing
// commits headers, so the cookie rewrite has to happen first.
func (c *adminCookieRewriter) Flush() {
	if !c.wroteHeader {
		c.rewrite()
		c.wroteHeader = true
	}
	// ResponseController finds flush support through FlushError and Unwrap
	// chains that a direct http.Flusher assert would miss.
	_ = http.NewResponseController(c.ResponseWriter).Flush()
}

// Unwrap lets http.ResponseController reach controls of the underlying
// writer that this wrapper does not forward.
func (c *adminCookieRewriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func (c *adminCookieRewriter) rewrite() {
	h := c.Header()
	cookies := h.Values("Set-Cookie")
	if len(cookies) == 0 {
		return
	}
	h.Del("Set-Cookie")
	for _, raw := range cookies {
		h.Add("Set-Cookie", rewriteAdminCookie(raw, c.domain))
	}
}

// (keep helpers below; rewriteAdminCookie tolerates an empty domain by
// omitting the Domain attribute entirely.)

var adminCookieNames = map[string]struct{}{
	"gram_admin":             {},
	"gram_admin_login_state": {},
	"gram_admin_override":    {},
}

// rewriteAdminCookie mutates a Set-Cookie header value for known admin
// cookies: strips any existing Domain= and SameSite= attributes, then appends
// Domain=<domain>, SameSite=None, and Secure. Leaves other cookies untouched.
func rewriteAdminCookie(raw, domain string) string {
	parts := strings.Split(raw, ";")
	if len(parts) == 0 {
		return raw
	}
	first := strings.TrimSpace(parts[0])
	before, _, ok := strings.Cut(first, "=")
	if !ok {
		return raw
	}
	name := before
	if _, ok := adminCookieNames[name]; !ok {
		return raw
	}

	rebuilt := make([]string, 0, len(parts)+2)
	rebuilt = append(rebuilt, first)
	hasSecure := false
	for _, p := range parts[1:] {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		switch {
		case strings.HasPrefix(lower, "samesite="):
			continue
		case strings.HasPrefix(lower, "domain="):
			continue
		case lower == "secure":
			hasSecure = true
			rebuilt = append(rebuilt, trimmed)
		default:
			rebuilt = append(rebuilt, trimmed)
		}
	}
	if !hasSecure {
		rebuilt = append(rebuilt, "Secure")
	}
	rebuilt = append(rebuilt, "SameSite=None")
	if domain != "" {
		rebuilt = append(rebuilt, "Domain="+domain)
	}
	return strings.Join(rebuilt, "; ")
}
