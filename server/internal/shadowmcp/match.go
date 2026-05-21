package shadowmcp

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/matchvalue"
)

const (
	MatchBreadthFullURL        = "full_url"
	MatchBreadthURLHost        = "url_host"
	MatchBreadthServerIdentity = "server_identity"
)

type AccessEvidence struct {
	FullURL        string
	URLHost        string
	ServerIdentity string
}

func NormalizeMatchValue(matchBreadth string, matchValue string) (string, error) {
	value, err := matchvalue.Normalize(matchBreadth, matchValue)
	if err != nil {
		return "", fmt.Errorf("normalize match value: %w", err)
	}
	return value, nil
}

func NormalizeAccessEvidence(evidence AccessEvidence) AccessEvidence {
	var normalized AccessEvidence

	if evidence.FullURL != "" {
		if value, err := NormalizeMatchValue(MatchBreadthFullURL, evidence.FullURL); err == nil {
			normalized.FullURL = value
		}
		if normalized.URLHost == "" {
			if u, err := url.Parse(evidence.FullURL); err == nil && u.Host != "" {
				normalized.URLHost = NormalizeURLHost(strings.ToLower(u.Scheme), u.Host)
			}
		}
	}

	if evidence.URLHost != "" {
		if value, err := NormalizeMatchValue(MatchBreadthURLHost, evidence.URLHost); err == nil {
			normalized.URLHost = value
		}
	}

	if evidence.ServerIdentity != "" {
		if value, err := NormalizeMatchValue(MatchBreadthServerIdentity, evidence.ServerIdentity); err == nil {
			normalized.ServerIdentity = value
		}
	}

	return normalized
}

func (e AccessEvidence) Empty() bool {
	return e.FullURL == "" && e.URLHost == "" && e.ServerIdentity == ""
}

func NormalizeHost(host string) string {
	return matchvalue.NormalizeHost(host)
}

func NormalizeURLHost(scheme string, host string) string {
	return matchvalue.NormalizeURLHost(scheme, host)
}
