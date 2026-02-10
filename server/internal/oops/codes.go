package oops

import "net/http"

type Code string

const (
	CodeUnauthorized     Code = "unauthorized"
	CodeForbidden        Code = "forbidden"
	CodeBadRequest       Code = "bad_request"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeUnsupportedMedia   Code = "unsupported_media"
	CodeInvalid            Code = "invalid"
	CodeUnexpected         Code = "unexpected"
	CodeInvariantViolation Code = "invariant_violation"
	CodeGatewayError       Code = "gateway_error"
	CodeNotImplemented     Code = "not_implemented"
)

var StatusCodes = map[Code]int{
	CodeUnauthorized: http.StatusUnauthorized,
	CodeForbidden:    http.StatusForbidden,
	CodeBadRequest:   http.StatusBadRequest,
	CodeNotFound:           http.StatusNotFound,
	CodeConflict:           http.StatusConflict,
	CodeUnsupportedMedia:   http.StatusUnsupportedMediaType,
	CodeInvalid:            http.StatusUnprocessableEntity,
	CodeUnexpected:         http.StatusInternalServerError,
	CodeInvariantViolation: http.StatusUnprocessableEntity,
	CodeGatewayError:       http.StatusBadGateway,
	CodeNotImplemented:     http.StatusNotImplemented,
}

func (c Code) UserMessage() string {
	switch c {
	case CodeUnauthorized:
		return "unauthorized access"
	case CodeForbidden:
		return "permission denied"
	case CodeBadRequest:
		return "request is invalid"
	case CodeNotFound:
		return "resource not found"
	case CodeConflict:
		return "resource already exists"
	case CodeUnsupportedMedia:
		return "unsupported media type"
	case CodeInvalid:
		return "request contains one or more invalidation fields"
	case CodeNotImplemented:
		return "requested feature is not implemented"
	default:
		return "an unexpected error occurred"
	}
}

func (c Code) IsTemporary() bool {
	switch c {
	case CodeUnexpected, CodeGatewayError:
		return true
	default:
		return false
	}
}
