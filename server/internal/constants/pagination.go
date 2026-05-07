package constants

// DefaultPageLimit is the page size used by keyset-paginated list handlers
// when the caller omits an explicit limit.
const DefaultPageLimit = 50

// MaxPageLimit is the upper bound enforced by keyset-paginated list handlers
// regardless of the caller-supplied limit.
const MaxPageLimit = 100
