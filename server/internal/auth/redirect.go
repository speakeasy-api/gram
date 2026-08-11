package auth

import (
	"net/url"
	"strings"
)

// parseSiteOrigin reduces a configured site URL to the "scheme://host" that a
// post-login redirect target must name to count as the dashboard itself.
// Anything that is not an absolute URL yields "", which leaves relative paths
// as the only redirect shape the sanitizer will accept.
func parseSiteOrigin(rawSiteURL string) string {
	parsed, err := url.Parse(rawSiteURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

// safeRedirectPath normalizes a caller-supplied post-login destination into a
// rooted, same-origin path, or returns "" when the value cannot be trusted and
// the caller should fall back to a destination of its own choosing.
//
// The result is handed to a browser in a Location header, so the only question
// that matters is whether the browser could read it as another origin. Two
// shapes allow that and both are refused here: an absolute URL naming a host
// other than allowedOrigin, and anything a browser resolves as a
// protocol-relative reference. The second has more spellings than it looks
// like — "//evil.com", "///evil.com" (the URL parser skips the extra slashes
// while looking for an authority), and "/\evil.com" (a backslash is read as a
// slash). Requiring exactly one leading slash on a rebuilt, re-escaped path
// rejects all of them.
//
// allowedOrigin is a "scheme://host" as produced by parseSiteOrigin. When it is
// empty, only relative paths survive.
func safeRedirectPath(raw string, allowedOrigin string) string {
	if raw == "" {
		return ""
	}

	// A browser reads an unescaped backslash as a slash, so "/\evil.com" is a
	// host and "/a\b" is two path segments, while Go's url package reads both as
	// literal path characters. Rather than pick a side, refuse the input: a
	// destination that means one thing here and another in the browser is not a
	// destination worth honoring, and nothing the dashboard links to has one.
	if strings.Contains(raw, `\`) {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	// A scheme or a host means the value is trying to name an origin — either an
	// absolute URL or a protocol-relative one, which url.Parse reports as a host
	// with an empty scheme. Compare on Host alone so that userinfo tricks such as
	// "https://app.example.com@evil.com/" are judged by "evil.com".
	if parsed.Scheme != "" || parsed.Host != "" {
		origin := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
		if allowedOrigin == "" || origin != allowedOrigin {
			return ""
		}
	}

	path := parsed.EscapedPath()
	if path == "" && parsed.Host != "" {
		// An allowed absolute URL with no path, e.g. "https://app.example.com",
		// asks for the dashboard root.
		path = "/"
	}

	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return ""
	}

	dest := path
	if parsed.RawQuery != "" {
		dest += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		dest += "#" + parsed.EscapedFragment()
	}

	return dest
}
