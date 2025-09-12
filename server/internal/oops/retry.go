package oops

import (
	"fmt"
)

type permanentError struct{}

func (e *permanentError) Error() string { return "" }

var ErrPermanent = &permanentError{}

func Permanent(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w%w", err, ErrPermanent)
}
