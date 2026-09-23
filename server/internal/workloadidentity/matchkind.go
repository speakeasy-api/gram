package workloadidentity

import (
	"errors"
	"fmt"
	"strings"
)

// WildcardSuffix terminates a wildcard rule's stored value. Required, and
// required to be last: see MatchKindWildcard.
const WildcardSuffix = "*"

// MatchKind is how an admission or an agent assignment compares the subject it
// stores against the one an assertion presents.
//
// A string rather than an enum in the database, so adding a kind is a code
// change rather than a migration. The set is closed here instead.
type MatchKind string

const (
	// MatchKindExact compares the whole subject. The default, and the only kind
	// a row gets without someone asking for the other.
	MatchKindExact MatchKind = "exact"

	// MatchKindWildcard matches any subject beginning with the stored value's
	// stem, which is everything before its trailing "*".
	//
	// It exists because a platform that mints an identity per resource does not
	// let an operator know the subject in advance: Claude Tag's agent id is
	// created with a Slack channel, is never shown in its console, and changes
	// when a channel is recreated. Without it the only way to admit one is to
	// let an exchange fail and read the subject out of a log line.
	//
	// The "*" is mandatory and must be last. It is not a glob engine — there is
	// no interior matching and no other metacharacter — it is a required
	// terminator, for two reasons.
	//
	// It makes breadth legible. A bare stem matches the same subjects while
	// hiding that it does: `system:serviceaccount:ns` reads as one service
	// account and also matches `ns-two`, where `system:serviceaccount:ns:*`
	// states what it covers. The operator marks the boundary, so a truncated rule
	// is visible rather than silent.
	//
	// And it avoids inventing a delimiter rule. Requiring a stem to end at a
	// structural boundary would mean knowing each platform's separator, and there
	// is no common one: subjects are "/"-separated on Claude Tag and SPIFFE,
	// ":"-separated on Kubernetes, both on GitHub Actions, and unstructured on
	// Entra and Google.
	//
	// An interior "*" is refused because it would allow matching a suffix while
	// leaving the middle open, which is strictly more dangerous than a stem and
	// buys nothing this needs.
	MatchKindWildcard MatchKind = "wildcard"
)

var (
	// ErrMatchKindUnknown reports a kind outside the closed set above.
	ErrMatchKindUnknown = errors.New("unknown subject match kind")

	// ErrSubjectEmpty reports a rule that would match on nothing.
	ErrSubjectEmpty = errors.New("subject must not be empty")

	// ErrWildcardNotPermitted reports a wildcard rule under an issuer that does
	// not allow wildcard matching.
	ErrWildcardNotPermitted = errors.New("this issuer does not permit wildcard matching")

	// ErrWildcardSuffixRequired reports a wildcard rule whose value does not end
	// in "*", which would match the same subjects without saying so.
	ErrWildcardSuffixRequired = errors.New(`a wildcard subject must end in "*"`)

	// ErrWildcardNotTerminal reports a "*" anywhere but the end.
	ErrWildcardNotTerminal = errors.New(`"*" is only allowed as the last character of a wildcard subject`)

	// ErrWildcardStemEmpty reports a bare "*", which would admit every subject
	// the issuer signs — every other tenant of a shared issuer included.
	ErrWildcardStemEmpty = errors.New(`a wildcard subject needs something before its "*"`)

	// ErrExactSubjectHasWildcard reports a "*" in an exact rule, which is almost
	// always someone expecting a wildcard and getting a literal that matches
	// nothing.
	ErrExactSubjectHasWildcard = errors.New(`an exact subject must not contain "*"; use the wildcard match kind`)
)

// ParseMatchKind resolves a stored or submitted value to a known kind. An empty
// value is exact, matching the column default, so a caller that does not care
// about matching does not have to name it.
func ParseMatchKind(value string) (MatchKind, error) {
	switch MatchKind(value) {
	case "", MatchKindExact:
		return MatchKindExact, nil
	case MatchKindWildcard:
		return MatchKindWildcard, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrMatchKindUnknown, value)
	}
}

// WildcardStem is the portion of a wildcard rule's subject that must lead a
// presented subject: everything before the trailing "*". It mirrors what the
// lookup queries compute in SQL, and is only meaningful for a value that has
// passed ValidateSubjectRule.
func WildcardStem(subject string) string {
	return strings.TrimSuffix(subject, WildcardSuffix)
}

// ValidateSubjectRule checks one admission or assignment before it is written.
// Shared so the admin surface and the tenant-facing management API cannot drift
// into accepting different rules for the same table.
//
// allowWildcard comes from the issuer's allow_wildcard_admission. It is
// re-checked on the read path too, which is what makes clearing it revoke
// wildcard rules already written rather than only preventing new ones; this call
// is the early, legible refusal rather than the security boundary.
func ValidateSubjectRule(kind MatchKind, subject string, allowWildcard bool) error {
	if subject == "" {
		return ErrSubjectEmpty
	}

	switch kind {
	case MatchKindExact:
		// A subject is otherwise opaque, so no character would be reserved here.
		// "*" is refused anyway: no platform puts one in a sub, so its presence
		// means the author wanted a wildcard and would instead have stored a
		// literal that matches nothing at all.
		if strings.Contains(subject, WildcardSuffix) {
			return ErrExactSubjectHasWildcard
		}
		return nil
	case MatchKindWildcard:
		if !allowWildcard {
			return ErrWildcardNotPermitted
		}
		if !strings.HasSuffix(subject, WildcardSuffix) {
			return ErrWildcardSuffixRequired
		}
		stem := WildcardStem(subject)
		if strings.Contains(stem, WildcardSuffix) {
			return ErrWildcardNotTerminal
		}
		if stem == "" {
			return ErrWildcardStemEmpty
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrMatchKindUnknown, kind)
	}
}
