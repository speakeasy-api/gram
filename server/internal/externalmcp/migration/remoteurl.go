package migration

import (
	"errors"
	"net/url"
	"strings"
)

// ErrTemplatedPath marks an upstream URL whose path still carries an unfilled
// registry variable, which has no safe default.
var ErrTemplatedPath = errors.New("upstream URL path contains an unfilled template variable")

// CleanRemoteURL drops query parameters that still carry an unfilled registry
// variable such as {TOOLSETS}. Registry remotes declare these for the client to
// fill; the external MCP proxy sent them upstream verbatim, so the cleaned URL
// is what the upstream effectively served.
func CleanRemoteURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("upstream URL is not a valid URL")
	}
	if hasTemplate(parsed.Path) || hasTemplate(parsed.RawPath) {
		return "", ErrTemplatedPath
	}

	kept := make([]string, 0)
	for part := range strings.SplitSeq(parsed.RawQuery, "&") {
		if part == "" || hasTemplate(part) {
			continue
		}
		kept = append(kept, part)
	}
	parsed.RawQuery = strings.Join(kept, "&")
	parsed.ForceQuery = false
	return parsed.String(), nil
}

func hasTemplate(value string) bool {
	if strings.ContainsAny(value, "{}") {
		return true
	}
	lower := strings.ToLower(value)
	return strings.Contains(lower, "%7b") || strings.Contains(lower, "%7d")
}

// sameUpstream compares two upstream URLs the way credential routing does:
// verbatim apart from trailing slashes.
func sameUpstream(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}
