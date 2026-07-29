package oops

import "net/http"

type Code string

const (
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeBadRequest         Code = "bad_request"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeUnsupportedMedia   Code = "unsupported_media"
	CodeMethodNotAllowed   Code = "method_not_allowed"
	CodeRequestTooLarge    Code = "request_too_large"
	CodeInvalid            Code = "invalid"
	CodeUnexpected         Code = "unexpected"
	CodeInvariantViolation Code = "invariant_violation"
	CodeGatewayError       Code = "gateway_error"
	// CodeUnavailable is a temporary, retryable inability to serve caused by
	// a Gram-side dependency being down (e.g. Redis unreachable), distinct
	// from CodeGatewayError (502, an upstream/tunnel failure). Maps to 503.
	CodeUnavailable         Code = "unavailable"
	CodeNotImplemented      Code = "not_implemented"
	CodeInsufficientCredits Code = "insufficient_credits"
	CodeRateLimitExceeded   Code = "rate_limit_exceeded"
	// CodeCanceled represents a request whose client disconnected mid-flight,
	// surfacing as a context.Canceled cause while the request context is itself
	// canceled. It is not a server fault. It is never authored directly:
	// ShareableError.effectiveCode promotes such errors to this code so the
	// logging, span, and HTTP status behavior is handled centrally. Server- and
	// application-initiated cancellations, where the request context is still
	// live, are left at their authored code.
	CodeCanceled Code = "canceled"
)

var StatusCodes = map[Code]int{
	CodeUnauthorized:        http.StatusUnauthorized,
	CodeForbidden:           http.StatusForbidden,
	CodeBadRequest:          http.StatusBadRequest,
	CodeNotFound:            http.StatusNotFound,
	CodeConflict:            http.StatusConflict,
	CodeUnsupportedMedia:    http.StatusUnsupportedMediaType,
	CodeMethodNotAllowed:    http.StatusMethodNotAllowed,
	CodeRequestTooLarge:     http.StatusRequestEntityTooLarge,
	CodeInvalid:             http.StatusUnprocessableEntity,
	CodeUnexpected:          http.StatusInternalServerError,
	CodeInvariantViolation:  http.StatusUnprocessableEntity,
	CodeGatewayError:        http.StatusBadGateway,
	CodeUnavailable:         http.StatusServiceUnavailable,
	CodeNotImplemented:      http.StatusNotImplemented,
	CodeInsufficientCredits: http.StatusPaymentRequired,
	CodeRateLimitExceeded:   http.StatusTooManyRequests,
	// 499 (client closed request) is non-standard but matches the convention
	// already used by the request access log middleware.
	CodeCanceled: 499,
}

func (c Code) UserMessage() string {
	switch c {
	case CodeUnauthorized:
		return "unauthorized access"
	case CodeForbidden:
		return "permission denied"
	case CodeBadRequest:
		return "request is invalid"
	case CodeMethodNotAllowed:
		return "method not allowed"
	case CodeNotFound:
		return "resource not found"
	case CodeConflict:
		return "resource already exists"
	case CodeUnsupportedMedia:
		return "unsupported media type"
	case CodeRequestTooLarge:
		return "request exceeds maximum allowed size"
	case CodeInvalid:
		return "request contains one or more invalidation fields"
	case CodeNotImplemented:
		return "requested feature is not implemented"
	case CodeInsufficientCredits:
		return "token balance exhausted"
	case CodeRateLimitExceeded:
		return "rate limit exceeded"
	case CodeUnavailable:
		return "service temporarily unavailable"
	case CodeCanceled:
		return "request was canceled"
	default:
		return "an unexpected error occurred"
	}
}

func (c Code) IsTemporary() bool {
	switch c {
	case CodeUnexpected, CodeGatewayError, CodeUnavailable:
		return true
	default:
		return false
	}
}

func (c Code) MCPCode() MCPCode {
	switch c {
	case CodeUnauthorized:
		return MCPCodeUnauthorized
	case CodeForbidden:
		return MCPCodeForbidden
	case CodeBadRequest, CodeConflict, CodeUnsupportedMedia:
		return MCPCodeInvalidRequest
	case CodeMethodNotAllowed:
		return MCPCodeServerError
	case CodeNotFound:
		return MCPCodeResourceNotFound
	case CodeInvalid:
		return MCPCodeInvalidParams
	case CodeNotImplemented:
		return MCPCodeMethodNotFound
	default:
		return MCPCodeInternalError
	}
}
