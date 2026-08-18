package researchagent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

//go:embed trusted_sources.json
var rawTrustedSources []byte

// trustedSource is one registry entry: a domain (optionally with a path
// prefix, like github.com/advisories) and the category it is recognized for.
type trustedSource struct {
	Domain   string `json:"domain"`
	Category string `json:"category"`
}

// trustedSources is the deterministic registry citations are annotated
// against. It annotates only: nothing here gates fetching, and the model's
// source_reputation label is never overridden — the two render side by side
// so a mismatch is something the admin sees rather than something code
// hides. The initial list was AI-authored and is human-curated from here.
var trustedSources = mustLoadTrustedSources()

func mustLoadTrustedSources() []trustedSource {
	var parsed struct {
		Sources []trustedSource `json:"sources"`
	}
	if err := json.Unmarshal(rawTrustedSources, &parsed); err != nil {
		panic(fmt.Sprintf("parse trusted_sources.json: %v", err))
	}
	return parsed.Sources
}

// trustedSourceCategory reports the registry category for a citation URL,
// empty when its domain is unlisted. A domain entry matches itself and any
// subdomain; an entry carrying a path (github.com/advisories) additionally
// requires that path prefix, so a listing for one section of a shared host
// never vouches for the rest of it.
func trustedSourceCategory(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	path := parsed.EscapedPath()

	for _, source := range trustedSources {
		domain, pathPrefix, hasPath := strings.Cut(source.Domain, "/")
		if host != domain && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		if hasPath && !strings.HasPrefix(strings.TrimPrefix(path, "/"), pathPrefix) {
			continue
		}
		return source.Category
	}

	return ""
}
