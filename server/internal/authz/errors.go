package authz

import (
	"errors"
	"fmt"
)

var ErrMissingGrants = errors.New("authz grants missing from context")

var ErrNoChecks = errors.New("at least one authz check is required")

var ErrInvalidCheck = errors.New("invalid authz check")

var ErrDenied = errors.New("authz denied")

func InvalidCheck(scope Scope, resourceID string) error {
	return &InvalidCheckError{
		Scope:      scope,
		ResourceID: resourceID,
		Reason:     "",
		cause:      ErrInvalidCheck,
	}
}

func invalidCheckReason(scope Scope, resourceID, reason string) error {
	return &InvalidCheckError{Scope: scope, ResourceID: resourceID, Reason: reason, cause: ErrInvalidCheck}
}

func Denied(scope Scope, selector Selector) error {
	return &DeniedError{
		Scope:    scope,
		Selector: selector,
		cause:    ErrDenied,
	}
}

type DeniedError struct {
	Scope    Scope
	Selector Selector
	cause    error
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("authz denied for scope %q on selector %v", e.Scope, map[string]string(e.Selector))
}

func (e *DeniedError) Unwrap() error {
	return e.cause
}

type InvalidCheckError struct {
	Scope      Scope
	ResourceID string
	// Reason says why the check is malformed when the resource id is fine.
	Reason string
	cause  error
}

func (e *InvalidCheckError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("authz check for scope %q is invalid: %s", e.Scope, e.Reason)
	}
	if e.ResourceID == WildcardResource {
		return fmt.Sprintf("authz check for scope %q requires a specific resource id", e.Scope)
	}

	return fmt.Sprintf("authz check for scope %q requires a non-empty resource id", e.Scope)
}

func (e *InvalidCheckError) Unwrap() error {
	return e.cause
}
