package mcpservers

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// slugSuffixLen is how many hex characters from the server ID the computed
// slug embeds as a uniqueness suffix. Sized to keep slugs short while making
// suffix collisions across servers within a project rare enough to surface
// as a 409 rather than be designed around. Mirrors the remotemcp slug
// helper.
const slugSuffixLen = 4

// computeServerSlug derives the slug stored on an mcp_servers row from its
// name and ID. The name is run through [conv.ToSlug] to produce a URL-safe
// base, then the last [slugSuffixLen] hex characters of the ID are appended
// so two servers in the same project with the same name still land on
// distinct slugs. name is expected to already be trimmed and non-empty.
func computeServerSlug(name string, id uuid.UUID) (string, error) {
	idStr := id.String()
	if len(idStr) < slugSuffixLen {
		return "", fmt.Errorf("uuid string too short for slug suffix: %d", len(idStr))
	}
	suffix := idStr[len(idStr)-slugSuffixLen:]

	base := ServerSlugPrefix(name)
	if base == "" {
		return suffix, nil
	}
	return base + "-" + suffix, nil
}

// ServerSlugPrefix is the part of a server's slug that its name determines;
// the server's own ID completes it. Exported so a caller can show the exact
// prefix before creating the server rather than describing the derivation.
func ServerSlugPrefix(name string) string {
	return conv.ToSlug(name)
}

// Static dashboard /mcp segments; mirrors RESERVED_MCP_SEGMENTS in client/dashboard/src/lib/sources.ts.
var dashboardReservedSegments = map[string]struct{}{
	"add":                  {},
	"catalog":              {},
	"deployments":          {},
	"sources":              {},
	"organization-gateway": {},
	"gateway":              {},
	"x":                    {},
}

// DashboardRouteParam is the /mcp/<server> segment: the slug, or the id when the slug is empty or reserved.
func DashboardRouteParam(slug string, id uuid.UUID) string {
	slug = strings.TrimSpace(slug)
	if _, reserved := dashboardReservedSegments[slug]; slug == "" || reserved {
		return id.String()
	}
	return slug
}
