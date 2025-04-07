package inv

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Check checks a set of invariants and returns an error if any of them fail.
// Each invariant is passed as a pair: the name of the invariant as a string and
// a value that may be a boolean or error or function that return either of
// these types. If any value is false or a non-nil error across the pairs, then
// an error is returned with all the unmet invariants. Since this function
// checks multiple invariants, a group name is used to relate these checks
// together.
//
//go:noinline
func Check(group string, pairs ...any) error {
	if len(pairs)%2 != 0 {
		return errors.New("invariants must be passed as pairs")
	}

	var errs []*CheckError
	_, file, no, ok := runtime.Caller(1)
	caller := "<unknown>:0"
	if ok {
		caller = fmt.Sprintf("%s:%d", file, no)
	}

	for i := 0; i < len(pairs); i += 2 {
		id, pred := pairs[i].(string), pairs[i+1]

		if pred == nil {
			// Nothing to do for nil values.
			continue
		}

		switch val := pred.(type) {
		case func() error:
			if cause := val(); cause != nil {
				errs = append(errs, &CheckError{
					Group: group, ID: id, Caller: caller, Cause: cause,
				})
			}
		case error:
			if val != nil {
				errs = append(errs, &CheckError{
					Group: group, ID: id, Caller: caller, Cause: val,
				})
			}
		case func() bool:
			if !val() {
				errs = append(errs, &CheckError{
					Group: group, ID: id, Caller: caller,
				})
			}
		case bool:
			if !val {
				errs = append(errs, &CheckError{
					Group: group, ID: id, Caller: caller,
				})
			}
		default:
			panic(fmt.Sprintf("invalid value type for invariant: %s: %T", id, pred))
		}
	}

	if len(errs) > 0 {
		return &InvariantError{Failures: errs}
	}

	return nil
}

// InvariantError captures a group of invariant check failures.
type InvariantError struct {
	Failures []*CheckError
}

func (e *InvariantError) Error() string {
	if len(e.Failures) == 1 {
		return e.Failures[0].Error()
	}

	all := make([]string, 0, len(e.Failures))
	for _, err := range e.Failures {
		all = append(all, err.Error())
	}

	return strings.Join(all, "\n")
}

// CheckError represents an invariant mismatch. If the invariant failed
// because of a non-nil error then Cause will be set.
type CheckError struct {
	// ID is the name of the invariant that was not met.
	ID string
	// Group is a label that relates this error to others.
	Group string
	// The location in code where the invariant was checked.
	Caller string
	// Cause is the underlying error that caused the invariant check to fail.
	Cause error
}

// Error returns a string representation of the invariant error.
func (ae *CheckError) Error() string {
	msg := fmt.Sprintf("%s: invariant mismatch: %s: %s", ae.Caller, ae.Group, ae.ID)
	if ae.Cause != nil {
		msg += ": " + ae.Cause.Error()
	}

	return msg
}

// Unwrap returns the underlying error that caused the invariant check to fail.
func (ae *CheckError) Unwrap() error {
	return ae.Cause
}
