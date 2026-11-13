// Package httpsorigin validates HTTPS origins used to bind outbound credentials.
package httpsorigin

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
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
	if strings.HasSuffix(u.Host, ":") {
		return "", fmt.Errorf("expected a non-empty HTTPS port")
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		numericPort, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return "", fmt.Errorf("expected a valid HTTPS port")
		}
		if numericPort != 443 {
			host = net.JoinHostPort(host, strconv.FormatUint(numericPort, 10))
		} else if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	origin := &url.URL{Scheme: "https", Host: host}
	return origin.String(), nil
}
