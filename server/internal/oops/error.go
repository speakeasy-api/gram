package oops

import "fmt"

type Error struct {
	err     error
	message string
}

func New(message string) *Error {
	return &Error{
		message: message,
	}
}

func (e *Error) Wrap(err error) error {
	return &Error{
		err:     err,
		message: e.message,
	}
}

func (e *Error) Error() string {
	if e.err != nil {
		return fmt.Sprintf("%s: %s", e.message, e.err.Error())
	}

	return e.message
}

func (e *Error) Unwrap() error {
	return e.err
}
