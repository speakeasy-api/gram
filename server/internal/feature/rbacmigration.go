package feature

import (
	"context"
	"regexp"
)

// Each authorization migration unit has its own multivariate flag, keyed
// RBACMigrationFlag(unit), that selects which evaluator decides the unit's
// operations for an organization:
//
//   - no variant, an unknown variant, a missing provider or a failed
//     evaluation: the engine decides, as before the unit migrated;
//   - RBACMigrationVariantShadow: the engine still decides, and the registry
//     evaluator answers the same request for comparison only;
//   - RBACMigrationVariantNew: the registry evaluator decides.
//
// The flags are evaluated locally with the organization ID as the distinct
// ID, so each PostHog flag must be person-aggregated and match distinct_id in
// a list of organization IDs, like FlagAuthClientResolver. The authorization
// platform owns these keys; a unit's flag is removed once the engine path for
// the unit is deleted.

// RBACMigrationVariantShadow compares the registry evaluator with the engine
// while the engine decides.
const RBACMigrationVariantShadow Variant = "shadow"

// RBACMigrationVariantNew lets the registry evaluator decide.
const RBACMigrationVariantNew Variant = "new"

var migrationUnitName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidMigrationUnit reports whether unit can name a migration flag.
func ValidMigrationUnit(unit string) bool {
	return migrationUnitName.MatchString(unit)
}

// RBACMigrationFlag is the flag that routes one migration unit.
func RBACMigrationFlag(unit string) Flag {
	return Flag("rbac-migration-" + unit)
}

// RBACMigrationVariant resolves the routing variant of a migration unit for
// an organization from locally evaluated flags. It returns "" whenever the
// flag cannot be evaluated, which keeps the engine deciding.
func RBACMigrationVariant(ctx context.Context, flags Provider, unit, organizationID string) Variant {
	if flags == nil || organizationID == "" || !ValidMigrationUnit(unit) {
		return ""
	}
	variant, err := FlagVariantLocal(ctx, flags, RBACMigrationFlag(unit), organizationID, nil, nil)
	if err != nil {
		return ""
	}
	switch variant {
	case RBACMigrationVariantShadow, RBACMigrationVariantNew:
		return variant
	default:
		return ""
	}
}
