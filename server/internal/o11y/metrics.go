package o11y

import (
	"context"
	"errors"
	"net"
)

type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	// OutcomeTimeout is a failure that is specifically a timeout — a deadline
	// exceeded or a network-level timeout. It is split out from OutcomeFailure so
	// timeouts are alertable on their own, distinct from the many other reasons a
	// call can fail (e.g. a socket hang up). Use OutcomeFromErrorWithTimeout to
	// classify; OutcomeFromError still collapses timeouts into OutcomeFailure for
	// callers that do not care.
	OutcomeTimeout Outcome = "timeout"
)

func OutcomeFromError(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	return OutcomeFailure
}

// OutcomeFromErrorWithTimeout is like OutcomeFromError but returns OutcomeTimeout
// for deadline-exceeded and network timeout errors, so callers that impose their
// own call deadline (e.g. the LLM judges) can alert on timeouts distinctly. A
// canceled context (caller gave up) is not a timeout and stays OutcomeFailure.
func OutcomeFromErrorWithTimeout(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return OutcomeTimeout
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return OutcomeTimeout
	}
	return OutcomeFailure
}
