package workloadidentity

import (
	"errors"
	"fmt"
	"strings"
)

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

	// MatchKindPrefix matches any subject beginning with the stored value.
	//
	// It exists because a platform that mints an identity per resource does not
	// let an operator know the subject in advance: Claude Tag's agent id is
	// created with a Slack channel, is never shown in its console, and changes
	// when a channel is recreated. Without it the only way to admit one is to
	// let an exchange fail and read the subject out of a log line.
	MatchKindPrefix MatchKind = "prefix"
)

var (
	// ErrMatchKindUnknown reports a kind outside the closed set above.
	ErrMatchKindUnknown = errors.New("unknown subject match kind")

	// ErrSubjectEmpty reports a rule that would match on nothing.
	ErrSubjectEmpty = errors.New("subject must not be empty")

	// ErrPrefixNotPermitted reports a prefix rule under an issuer that does not
	// allow prefix matching.
	ErrPrefixNotPermitted = errors.New("this issuer does not permit prefix matching")

	// ErrPrefixIsNotAGlob reports a prefix containing a wildcard character.
	ErrPrefixIsNotAGlob = errors.New("a prefix is matched literally and must not contain '*'")
)

// ParseMatchKind resolves a stored or submitted value to a known kind. An empty
// value is exact, matching the column default, so a caller that does not care
// about matching does not have to name it.
func ParseMatchKind(value string) (MatchKind, error) {
	switch MatchKind(value) {
	case "", MatchKindExact:
		return MatchKindExact, nil
	case MatchKindPrefix:
		return MatchKindPrefix, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrMatchKindUnknown, value)
	}
}

// ValidateSubjectRule checks one admission or assignment before it is written.
// Shared so the admin surface and the tenant-facing management API cannot drift
// into accepting different rules for the same table.
//
// allowPrefix comes from the issuer's allow_prefix_admission. It is re-checked
// on the read path too, which is what makes clearing it revoke prefix rules
// already written rather than only preventing new ones; this call is the early,
// legible refusal rather than the security boundary.
func ValidateSubjectRule(kind MatchKind, subject string, allowPrefix bool) error {
	if subject == "" {
		return ErrSubjectEmpty
	}

	switch kind {
	case MatchKindExact:
		// A subject is opaque, so no character is reserved in an exact rule.
		return nil
	case MatchKindPrefix:
		if !allowPrefix {
			return ErrPrefixNotPermitted
		}
		// Someone pasting a glob would otherwise store a literal that matches
		// nothing at all, silently, while reading as correct wherever it is
		// listed. Refusing it turns a silent no-match into a correction.
		if strings.Contains(subject, "*") {
			return ErrPrefixIsNotAGlob
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrMatchKindUnknown, kind)
	}
}
