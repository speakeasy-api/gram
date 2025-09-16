package oops

import (
	"errors"
	"fmt"
)

type permanentError struct{}

func (e *permanentError) Error() string { return "permanent error" }

var ErrPermanent = &permanentError{}

func Permanent(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrPermanent) {
		return err
	}

	return fmt.Errorf("%w: %w", ErrPermanent, err)
}
