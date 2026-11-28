package constants

import "slices"

// AccountTypes is the closed set an operator may write. Shared by the Goa
// design, which rejects anything else at the HTTP boundary, and by the admin
// service, which repeats the check for a caller that arrives another way.
var AccountTypes = []string{"free", "pro", "payg", "enterprise"}

func IsAccountType(v string) bool {
	return slices.Contains(AccountTypes, v)
}

// Far above any realistic selection, far below a list worth echoing back.
const MaxBulkAccountTypeIDs = 1000

// MaxResolveOrganizationsBatch bounds one organization lookup, counting ids and
// slugs together. It keeps the lookup to one bounded query and its answer to a
// size a caller can list back to an operator.
const MaxResolveOrganizationsBatch = 1000

// MaxBulkDisableOrganizations bounds one bulk disable. Every organization is
// locked and audited in one transaction, so the cap is what keeps that
// transaction short.
const MaxBulkDisableOrganizations = 1000
