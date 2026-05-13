package remotemcp

import (
	"fmt"
	"net/url"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// slugSuffixLen is how many hex characters from the server ID the computed
// slug embeds as a uniqueness suffix. Sized to keep slugs short while making
// suffix collisions across servers within a project rare enough to surface
// as a 409 rather than be designed around.
const slugSuffixLen = 4

// computeServerSlug derives the slug stored on a remote MCP server row from
// its URL and ID. The transform takes the URL host plus path (scheme
// stripped), runs it through [conv.URLToSlug] so dots and slashes become
// hyphen boundaries, then appends the last [slugSuffixLen] hex characters of
// the ID so two servers in the same project with the same URL still land on
// distinct slugs. rawURL is expected to have already passed [validateURL].
func computeServerSlug(rawURL string, id uuid.UUID) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	idStr := id.String()
	if len(idStr) < slugSuffixLen {
		return "", fmt.Errorf("uuid string too short for slug suffix: %d", len(idStr))
	}
	suffix := idStr[len(idStr)-slugSuffixLen:]

	base := conv.URLToSlug(u.Host + u.Path)
	if base == "" {
		return suffix, nil
	}
	return base + "-" + suffix, nil
}
