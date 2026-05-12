package shadowmcp

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	MatchBreadthFullURL        = "full_url"
	MatchBreadthURLHost        = "url_host"
	MatchBreadthServerIdentity = "server_identity"
)

func NormalizeMatchValue(matchBreadth string, matchValue string) (string, error) {
	if strings.TrimSpace(matchValue) == "" {
		return "", fmt.Errorf("match_value is required")
	}

	value := strings.TrimSpace(matchValue)
	switch matchBreadth {
	case MatchBreadthFullURL:
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf("match_value must be a full URL")
		}
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = NormalizeHost(u.Host)
		u.Fragment = ""
		if u.Path == "/" && u.RawPath == "" {
			u.Path = ""
		}
		u.RawQuery = u.Query().Encode()
		return u.String(), nil
	case MatchBreadthURLHost:
		if strings.Contains(value, "://") {
			u, err := url.Parse(value)
			if err != nil || u.Host == "" {
				return "", fmt.Errorf("match_value must include a URL host")
			}
			value = u.Host
		}
		return NormalizeHost(value), nil
	case MatchBreadthServerIdentity:
		return strings.ToLower(value), nil
	default:
		return "", fmt.Errorf("invalid match_breadth")
	}
}

func NormalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	if port == "80" || port == "443" {
		return name
	}
	return host
}
