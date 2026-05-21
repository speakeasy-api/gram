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
		u.Host = NormalizeURLHost(u.Scheme, u.Host)
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
			return NormalizeURLHost(strings.ToLower(u.Scheme), u.Host), nil
		}
		return NormalizeHost(value), nil
	case MatchBreadthServerIdentity:
		return strings.ToLower(value), nil
	default:
		return "", fmt.Errorf("invalid match_breadth")
	}
}

func NormalizeHost(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

func NormalizeURLHost(scheme string, host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return name
	}
	return host
}
