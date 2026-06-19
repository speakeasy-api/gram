package shadowmcp

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/matchvalue"
)

const (
	MatchBreadthFullURL = "full_url"
	MatchBreadthURLHost = "url_host"
)

type AccessEvidence struct {
	FullURL        string
	URLHost        string
	ServerIdentity string
}

func ObservedName(evidence AccessEvidence, toolName string) *string {
	switch {
	case evidence.URLHost != "":
		return &evidence.URLHost
	case evidence.ServerIdentity != "":
		name := HumanizeServerIdentity(evidence.ServerIdentity)
		return &name
	case toolName != "":
		return &toolName
	default:
		return nil
	}
}

func HumanizeServerIdentity(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == ':' || r == ' '
	})
	if len(parts) == 0 {
		return value
	}

	words := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		words = append(words, humanizeServerIdentityWord(part))
	}
	if len(words) == 0 {
		return value
	}
	return strings.Join(words, " ")
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

	normalized.ServerIdentity = strings.TrimSpace(evidence.ServerIdentity)

	return normalized
}

func (e AccessEvidence) Empty() bool {
	return e.FullURL == "" && e.URLHost == ""
}

func NormalizeHost(host string) string {
	return matchvalue.NormalizeHost(host)
}

func NormalizeURLHost(scheme string, host string) string {
	return matchvalue.NormalizeURLHost(scheme, host)
}

func humanizeServerIdentityWord(value string) string {
	lower := strings.ToLower(value)
	switch lower {
	case "ai", "api", "http", "https", "mcp", "oauth", "url":
		return strings.ToUpper(lower)
	case "github":
		return "GitHub"
	default:
		runes := []rune(lower)
		if len(runes) == 0 {
			return ""
		}
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}
}
