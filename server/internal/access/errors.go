package access

import (
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

var ErrMissingGrants = oops.E(oops.CodeUnexpected, nil, "access grants missing from context")

var ErrNoChecks = oops.E(oops.CodeInvariantViolation, nil, "at least one access check is required")

func InvalidCheck(scope Scope) error {
	return &InvalidCheckError{
		Scope: scope,
		cause: oops.E(oops.CodeInvariantViolation, nil, "access check requires resource id for scope %q", scope),
	}
}

func Denied(scope Scope, resourceID string) error {
	return &DeniedError{
		Scope:      scope,
		ResourceID: resourceID,
		cause:      oops.C(oops.CodeForbidden),
	}
}

type DeniedError struct {
	Scope      Scope
	ResourceID string
	cause      *oops.ShareableError
}

func (e *DeniedError) Error() string {
	if e.ResourceID == "" {
		return fmt.Sprintf("access denied for scope %q", e.Scope)
	}

	return fmt.Sprintf("access denied for scope %q on resource %q", e.Scope, e.ResourceID)
}

func (e *DeniedError) Unwrap() error {
	return e.cause
}

func (e *DeniedError) Is(target error) bool {
	return errors.Is(e.cause, target)
}

type InvalidCheckError struct {
	Scope Scope
	cause *oops.ShareableError
}

func (e *InvalidCheckError) Error() string {
	return fmt.Sprintf("access check for scope %q requires a non-empty resource id", e.Scope)
}

func (e *InvalidCheckError) Unwrap() error {
	return e.cause
}

func (e *InvalidCheckError) Is(target error) bool {
	return errors.Is(e.cause, target)
}
