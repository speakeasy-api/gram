package middleware

import (
	"net/http"
	"strings"
)

// isChannelsEndpoint matches only the canonical project-scoped resource shape.
// Do not clean paths: empty, dot, escaped, or extra segments are not aliases.
func isChannelsEndpoint(path, endpoint string) bool {
	rest, ok := strings.CutPrefix(path, "/channels/")
	if !ok {
		return false
	}
	segments := strings.Split(rest, "/")
	if len(segments) != 3 || segments[2] != endpoint {
		return false
	}
	for _, slug := range segments[:2] {
		if slug == "" || slug == "." || slug == ".." || strings.ContainsAny(slug, "%\\ \t\r\n") {
			return false
		}
	}
	return true
}

// channelsClassificationPath denies encoded spellings the public policy of a
// canonical Channels route, while preserving existing classifiers elsewhere.
func channelsClassificationPath(r *http.Request) string {
	path := r.URL.Path
	if r.URL.EscapedPath() == path {
		return path
	}
	for _, namespace := range []string{
		"/channels",
		"/.well-known/oauth-authorization-server/channels",
		"/.well-known/oauth-protected-resource/channels",
	} {
		if path == namespace || strings.HasPrefix(path, namespace+"/") {
			return "/channels"
		}
	}
	return path
}
