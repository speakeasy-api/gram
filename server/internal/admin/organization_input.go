package admin

import (
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const maxOrganizationSlugLength = 100

var organizationSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateOrganizationSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	if len(slug) < 2 {
		return "", oops.E(oops.CodeInvalid, nil, "organization slug must contain at least 2 characters")
	}
	if len(slug) > maxOrganizationSlugLength {
		return "", oops.E(oops.CodeInvalid, nil, "organization slug must be at most %d characters", maxOrganizationSlugLength)
	}
	if !organizationSlugPattern.MatchString(slug) {
		return "", oops.E(oops.CodeInvalid, nil, "organization slug must use lowercase letters, numbers, and single hyphens between words")
	}

	return slug, nil
}
