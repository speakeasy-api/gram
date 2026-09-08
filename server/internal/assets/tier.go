package assets

import (
	"fmt"
	"path"

	"github.com/google/uuid"
)

// assetTier identifies which ownership tier an asset row lives in. The
// taxonomy matches the assets table: project rows carry both project_id and
// organization_id, organization rows carry only organization_id, platform
// rows carry neither. Tier validity is enforced in application code, not by a
// CHECK constraint: every write path must state its tier explicitly and the
// tier-specific queries pin the owner columns, so an organization-tier write
// missing its organization id fails loudly on the foreign key instead of
// silently landing as a platform-tier row (which would be publicly served and
// upsertable across tenants).
type assetTier string

const (
	tierProject      assetTier = "project"
	tierOrganization assetTier = "organization"
	tierPlatform     assetTier = "platform"
)

// assetStorageKey returns the object-store key for a file owned at the given
// tier, validating the tier's identifiers. The prefixes are disjoint
// namespaces: project keys start with the project UUID in its canonical
// 36-character form, which can never equal the literal "organizations" or
// "platform" segments.
func assetStorageKey(tier assetTier, projectID uuid.UUID, organizationID string, filename string) (string, error) {
	switch tier {
	case tierProject:
		if projectID == uuid.Nil {
			return "", fmt.Errorf("project asset storage key: project id is empty")
		}
		return path.Join(projectID.String(), filename), nil
	case tierOrganization:
		if organizationID == "" {
			return "", fmt.Errorf("organization asset storage key: organization id is empty")
		}
		return path.Join("organizations", organizationID, filename), nil
	case tierPlatform:
		return path.Join("platform", filename), nil
	default:
		return "", fmt.Errorf("unknown asset tier %q", tier)
	}
}
