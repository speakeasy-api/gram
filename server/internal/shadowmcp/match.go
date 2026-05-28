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

func NormalizeMatchValue(matchBreadth string, matchValue string) (string, error) {
	value, err := matchvalue.Normalize(matchBreadth, matchValue)
	if err != nil {
		return "", fmt.Errorf("normalize match value: %w", err)
	}
	return value, nil
}

func NormalizeHost(host string) string {
	return matchvalue.NormalizeHost(host)
}

func NormalizeURLHost(scheme string, host string) string {
	return matchvalue.NormalizeURLHost(scheme, host)
}
