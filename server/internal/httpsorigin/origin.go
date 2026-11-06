// Package httpsorigin validates HTTPS origins used to bind outbound credentials.
package httpsorigin

import (
	"fmt"
	"net/url"
	"strings"
)

// Parse requires an origin, allowing only an optional trailing slash.
func Parse(raw string) (string, error) {
	return normalize(raw, true)
}

// FromURL extracts an origin from a destination URL. Unlike Parse, it allows
// paths, queries, and fragments, but never credentials or non-HTTPS URLs.
func FromURL(raw string) (string, error) {
	return normalize(raw, false)
}

func normalize(raw string, strict bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", fmt.Errorf("expected an HTTPS URL without credentials")
	}
	if strict && (u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/")) {
		return "", fmt.Errorf("expected an HTTPS origin without path, query, or fragment")
	}
	origin := &url.URL{Scheme: "https", Host: strings.TrimSuffix(strings.ToLower(u.Host), ":443")}
	return origin.String(), nil
}
