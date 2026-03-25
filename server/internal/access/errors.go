package access

import (
	"errors"
	"fmt"
)

var ErrMissingGrants = errors.New("access grants missing from context")

var ErrNoChecks = errors.New("at least one access check is required")

var ErrInvalidCheck = errors.New("invalid access check")

var ErrDenied = errors.New("access denied")

func InvalidCheck(scope Scope, resourceID string) error {
	return &InvalidCheckError{
		Scope:      scope,
		ResourceID: resourceID,
		cause:      ErrInvalidCheck,
	}
}

func Denied(scope Scope, resourceID string) error {
	return &DeniedError{
		Scope:      scope,
		ResourceID: resourceID,
		cause:      ErrDenied,
	}
}

type DeniedError struct {
	Scope      Scope
	ResourceID string
	cause      error
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("access denied for scope %q on resource %q", e.Scope, e.ResourceID)
}

func (e *DeniedError) Unwrap() error {
	return e.cause
}

type InvalidCheckError struct {
	Scope      Scope
	ResourceID string
	cause      error
}

func (e *InvalidCheckError) Error() string {
	if e.ResourceID == WildcardResource {
		return fmt.Sprintf("access check for scope %q requires a specific resource id", e.Scope)
	}

	return fmt.Sprintf("access check for scope %q requires a non-empty resource id", e.Scope)
}

func (e *InvalidCheckError) Unwrap() error {
	return e.cause
}
