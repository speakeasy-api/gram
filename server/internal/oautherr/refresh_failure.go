package oautherr

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RefreshStatus is what the HTTP status of a refused refresh_token grant says.
type RefreshStatus string

const (
	// RefreshStatusRejected is any other status, including a 2xx carrying an error body.
	RefreshStatusRejected RefreshStatus = "rejected"

	// RefreshStatusRateLimited is HTTP 429 (RFC 6585 §4).
	RefreshStatusRateLimited RefreshStatus = "rate_limited"

	// RefreshStatusTimeout is HTTP 408 (RFC 9110 §15.5.9).
	RefreshStatusTimeout RefreshStatus = "timeout"

	// RefreshStatusServerError is any 5xx (RFC 9110 §15.6).
	RefreshStatusServerError RefreshStatus = "server_error"
)

// RefreshCode is what the error code of a refused refresh_token grant says.
type RefreshCode string

const (
	// RefreshCodeMissing means the body carried no recognizable error code.
	RefreshCodeMissing RefreshCode = "missing"

	// RefreshCodeInvalidGrant is RFC 6749 §5.2 invalid_grant: the refresh token can never renew.
	RefreshCodeInvalidGrant RefreshCode = "invalid_grant"

	// RefreshCodeSignInRequired is an OIDC Core §3.1.2.6 code: the person must sign in again.
	RefreshCodeSignInRequired RefreshCode = "sign_in_required"

	// RefreshCodeInvalidClient is RFC 6749 §5.2 invalid_client: the client itself was refused.
	RefreshCodeInvalidClient RefreshCode = "invalid_client"

	// RefreshCodeUnauthorizedClient is RFC 6749 §5.2 unauthorized_client.
	RefreshCodeUnauthorizedClient RefreshCode = "unauthorized_client"

	// RefreshCodeInvalidTarget is RFC 8707 §2 invalid_target: the resource, not the grant, was refused.
	RefreshCodeInvalidTarget RefreshCode = "invalid_target"

	// RefreshCodeTransient is temporarily_unavailable or server_error (RFC 6749 §4.1.2.1).
	RefreshCodeTransient RefreshCode = "transient"

	// RefreshCodeOther is any other code.
	RefreshCodeOther RefreshCode = "other"
)

// RefreshFailure keeps status and code apart: an intermediary's 429 or 5xx says nothing about the code.
type RefreshFailure struct {
	Status RefreshStatus
	Code   RefreshCode
}

// ClassifyRefreshFailure reads a refused refresh; vendor dead-grant codes read as invalid_grant.
func ClassifyRefreshFailure(status int, code string) RefreshFailure {
	return RefreshFailure{Status: refreshStatus(status), Code: refreshCode(CanonicalTokenErrorCode(code))}
}

func refreshStatus(status int) RefreshStatus {
	switch {
	case status == http.StatusTooManyRequests:
		return RefreshStatusRateLimited
	case status == http.StatusRequestTimeout:
		return RefreshStatusTimeout
	case status >= http.StatusInternalServerError:
		return RefreshStatusServerError
	default:
		return RefreshStatusRejected
	}
}

func refreshCode(code string) RefreshCode {
	switch {
	case code == "":
		return RefreshCodeMissing
	case code == CodeInvalidGrant:
		return RefreshCodeInvalidGrant
	case IsOIDCSignInCode(code):
		return RefreshCodeSignInRequired
	case code == CodeInvalidClient:
		return RefreshCodeInvalidClient
	case code == CodeUnauthorizedClient:
		return RefreshCodeUnauthorizedClient
	case code == CodeInvalidTarget:
		return RefreshCodeInvalidTarget
	case code == CodeTemporarilyUnavailable || code == CodeServerError:
		return RefreshCodeTransient
	default:
		return RefreshCodeOther
	}
}

// IsOIDCSignInCode reports an OIDC Core §3.1.2.6 code that asks the person to sign in.
func IsOIDCSignInCode(code string) bool {
	switch code {
	case OIDCCodeLoginRequired, OIDCCodeInteractionRequired, OIDCCodeConsentRequired, OIDCCodeAccountSelectionRequired:
		return true
	}
	return false
}

// ParseRetryAfter reads RFC 9110 §10.2.3 seconds or an HTTP date, zero when absent, malformed or past; callers cap it.
func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(math.MaxInt64/time.Second) {
			return math.MaxInt64
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(at.Sub(now), 0)
	}
	return 0
}
