package classifier

import (
	"context"
	"errors"
)

// ErrDisabled indicates that classification is disabled.
var ErrDisabled = errors.New("classifier is disabled")

// Noop is a disabled classifier. Its zero value is ready to use. It performs no
// validation, accounting, or inference and retains no request data. Classify
// returns an empty result whose Err is ErrDisabled, never fabricated successful answers.
type Noop struct{}

var _ Classifier = Noop{}

// Classify returns an empty result whose Err is ErrDisabled.
func (Noop) Classify(context.Context, *Request) Result {
	return NewResult(nil, ErrDisabled)
}
