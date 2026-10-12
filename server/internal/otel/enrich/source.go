package enrich

import (
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
// becomes claude-code), then the result is slugified. An empty name, or one
// that yields no slug, is SourceUnknown.
func CanonicalSource(serviceName string) string {
	slug := conv.URLToSlug(chat.CanonicalSource(serviceName))
	if slug == "" {
		return SourceUnknown
	}
	return slug
}
