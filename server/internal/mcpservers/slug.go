package mcpservers

import (
	"fmt"

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

	base := conv.ToSlug(name)
	if base == "" {
		return suffix, nil
	}
	return base + "-" + suffix, nil
}
