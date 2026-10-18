package migration

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrTemplatedPath marks an upstream URL whose path still carries an unfilled
// registry variable, which has no safe default.
var ErrTemplatedPath = errors.New("upstream URL path contains an unfilled template variable")

// ErrInvalidURL marks an upstream URL that is not an absolute HTTP(S) URL.
var ErrInvalidURL = errors.New("upstream URL is not an absolute http or https URL")

// templateVariable matches a registry placeholder such as {TOOLSETS}.
var templateVariable = regexp.MustCompile(`\{[A-Za-z_][A-Za-z0-9_]*\}`)

// CleanRemoteURL drops query parameters that still carry an unfilled registry
// variable such as {TOOLSETS}. Registry remotes declare these for the client to
// fill; the external MCP proxy sent them upstream verbatim, so the cleaned URL
// is what the upstream effectively served.
func CleanRemoteURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", ErrInvalidURL
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

// hasTemplate reports whether value holds a template variable, either as is or
// percent-encoded.
func hasTemplate(value string) bool {
	if templateVariable.MatchString(value) {
		return true
	}
	decoded, err := url.QueryUnescape(value)
	return err == nil && templateVariable.MatchString(decoded)
}

// sameUpstream compares two upstream URLs the way credential routing does:
// verbatim apart from trailing slashes.
func sameUpstream(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}
