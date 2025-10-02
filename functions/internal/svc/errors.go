package svc

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
)

type CancellationError struct{ msg string }

func (e *CancellationError) Error() string { return e.msg }

var (
	ErrIdleServerTimeout error = &CancellationError{msg: "idle server timeout"}
	ErrTerminated        error = &CancellationError{msg: "terminated by function runner"}
)

type MethodError struct {
	// ID is a unique value for each occurrence of the error.
	ID string `json:"id"`
	// Message contains the specific error details.
	Message string `json:"message"`
	// Is the error a timeout?
	Timeout bool `json:"timeout"`
	// Is the error temporary?
	Temporary bool `json:"temporary"`
	// Is the error a server-side fault?
	Fault bool `json:"fault"`
	// HTTP status code
	httpCode int
	// err holds the original error if exists.
	err error
}

func NewMethodError(err error, httpCode int, timeout, temporary, fault bool) *MethodError {
	return &MethodError{
		httpCode:  httpCode,
		ID:        newErrorID(),
		Message:   err.Error(),
		Timeout:   timeout,
		Temporary: temporary,
		Fault:     fault,
		err:       err,
	}
}

func Fault(err error, httpCode int) *MethodError {
	return NewMethodError(err, httpCode, false, false, true)
}

func NewPermanentError(err error, httpCode int) *MethodError {
	return NewMethodError(err, httpCode, false, false, false)
}

func NewTemporaryError(err error, httpCode int) *MethodError {
	return NewMethodError(err, httpCode, false, true, false)
}

func NewTemporaryTimeoutError(err error, httpCode int) *MethodError {
	return NewMethodError(err, httpCode, true, true, false)
}

func (e *MethodError) Error() string {
	return e.Message
}

func (e *MethodError) Unwrap() error {
	return e.err
}

func (e *MethodError) Code() int {
	if e.httpCode <= 0 {
		return http.StatusInternalServerError
	}

	return e.httpCode
}

func newErrorID() string {
	b := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
