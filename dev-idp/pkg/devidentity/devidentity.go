// Package devidentity holds the identity constants the dev-idp hands out for
// its default organization and the rule that derives a developer's user id
// from their git committer email.
//
// It lives under pkg/ rather than dev-idp/internal/ because the Gram server's
// local seed needs the same values: the seed provisions data for the org the
// dev-idp will log you into, so both sides must agree on the org id, the slug,
// and how an email becomes a user id. A copied constant would drift silently
// and leave a freshly seeded developer staring at an empty org.
package devidentity

import (
	"strings"

	"github.com/google/uuid"
)

const (
	// DefaultOrgName is the display name of the dev-idp's default org.
	DefaultOrgName = "Speakeasy"
	// DefaultOrgSlug is its slug.
	DefaultOrgSlug = "speakeasy"

	// DefaultOrgWorkosID is a stable WorkOS-style org ID assigned to the
	// default "Speakeasy" org. Matches production format so Gram-side's
	// organization_metadata.workos_id looks realistic in local dev.
	DefaultOrgWorkosID = "org_devidp_speakeasy"
)

// userIDNamespace is a fixed UUID v5 namespace used to derive deterministic
// user IDs from email addresses. This ensures the same email always maps to
// the same UUID, surviving dev-idp SQLite resets without colliding with the
// Gram server's users_email_key unique constraint.
var userIDNamespace = uuid.MustParse("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")

// DeterministicUserID returns a stable UUID v5 derived from the given email.
func DeterministicUserID(email string) uuid.UUID {
	return uuid.NewSHA1(userIDNamespace, []byte(email))
}

// WorkOSUserIDPrefix is the prefix the dev-idp puts on the WorkOS-style user
// ids it presents as the OIDC subject. Real WorkOS returns "user_01J5C09...".
const WorkOSUserIDPrefix = "user_devidp_"

// WorkOSUserID formats an internal user UUID the way the dev-idp presents it
// as the login subject. The local seed writes it into users.workos_id so the
// row a developer's first login finds is already linked, rather than a bare
// UUID that no login will ever match.
func WorkOSUserID(id uuid.UUID) string {
	return WorkOSUserIDPrefix + strings.ReplaceAll(id.String(), "-", "")
}
