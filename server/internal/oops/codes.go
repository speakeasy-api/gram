package oops

type Code string

const (
	CodeUnauthorized       Code = "unauthorized"
	CodeForbidden          Code = "forbidden"
	CodeBadRequest         Code = "bad_request"
	CodeNotFound           Code = "not_found"
	CodeConflict           Code = "conflict"
	CodeUnsupportedMedia   Code = "unsupported_media"
	CodeInvalid            Code = "invalid"
	CodeUnexpected         Code = "unexpected"
	CodeInvariantViolation Code = "invariant_violation"
)

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
	default:
		return "an unexpected error occurred"
	}
}
