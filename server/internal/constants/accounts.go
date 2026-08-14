package constants

import "slices"

// AccountTypes is the closed set an operator may write. Shared by the Goa
// design, which rejects anything else at the HTTP boundary, and by the admin
// service, which repeats the check for a caller that arrives another way.
var AccountTypes = []string{"free", "pro", "enterprise"}

func IsAccountType(v string) bool {
	return slices.Contains(AccountTypes, v)
}

// Far above any realistic selection, far below a list worth echoing back.
const MaxBulkAccountTypeIDs = 1000
