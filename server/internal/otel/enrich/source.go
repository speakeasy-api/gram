package enrich

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

// SourceUnknown is the source of a record whose resource carries no usable
// service.name.
const SourceUnknown = "unknown"

// ServiceNameAttribute is the OTel resource attribute a record's source is
// derived from.
const ServiceNameAttribute = "service.name"

// CanonicalSource derives the source slug from a resource service.name, the
// same way for the event feed and for agent_events. Known product-surface
// aliases are folded first (through chat.CanonicalSource, so ClaudeCode
// becomes claude-code) so the slug lines up with the rest of the product,
// then the result is slugified: lowercased, with runs of non-alphanumerics
// collapsed to single hyphens. An empty name, or one that yields no slug, is
// SourceUnknown.
func CanonicalSource(serviceName string) string {
	slug := slugifySource(chat.CanonicalSource(serviceName))
	if slug == "" {
		return SourceUnknown
	}
	return slug
}

func slugifySource(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	pendingHyphen := false
	for _, r := range strings.ToLower(s) {
		isAlphanumeric := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isAlphanumeric {
			pendingHyphen = true
			continue
		}
		if pendingHyphen && b.Len() > 0 {
			b.WriteByte('-')
		}
		pendingHyphen = false
		b.WriteRune(r)
	}
	return b.String()
}
