package shadowmcp

import "github.com/speakeasy-api/gram/server/internal/matchvalue"

const (
	MatchBreadthFullURL        = "full_url"
	MatchBreadthURLHost        = "url_host"
	MatchBreadthServerIdentity = "server_identity"
)

func NormalizeMatchValue(matchBreadth string, matchValue string) (string, error) {
	return matchvalue.Normalize(matchBreadth, matchValue)
}

func NormalizeHost(host string) string {
	return matchvalue.NormalizeHost(host)
}

func NormalizeURLHost(scheme string, host string) string {
	return matchvalue.NormalizeURLHost(scheme, host)
}
