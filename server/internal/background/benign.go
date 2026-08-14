package background

import (
	"errors"

	"go.temporal.io/sdk/temporal"
)

// asBenignWorkflowError returns a top-level ApplicationError with
// CategoryBenign so Temporal's workflow_failed metric and OTel span status
// stay quiet. Activity-caused errors arrive wrapped in *ActivityError (and
// often fmt.Errorf), which the SDK's isBenignApplicationError type-assertion
// does not unwrap — only a bare *ApplicationError at the workflow return
// skips those counters.
func asBenignWorkflowError(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	errType := ""
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		msg = appErr.Message()
		errType = appErr.Type()
	}

	return temporal.NewApplicationErrorWithOptions(msg, errType, temporal.ApplicationErrorOptions{
		NonRetryable: true,
		Category:     temporal.ApplicationErrorCategoryBenign,
		Cause:        err,
	})
}
