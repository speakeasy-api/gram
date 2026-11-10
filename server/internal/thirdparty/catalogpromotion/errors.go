package catalogpromotion

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrorKind determines whether delivery should retry or await operator action.
type ErrorKind string

const (
	// ErrorUnknown is an unclassified error and defaults to transient handling.
	ErrorUnknown ErrorKind = ""
	// ErrorTransient can recover without changing the approved metadata or configuration.
	ErrorTransient ErrorKind = "transient"
	// ErrorConfiguration requires credentials, repository access, or factory configuration to change.
	ErrorConfiguration ErrorKind = "configuration"
	// ErrorPermanent requires staff to reconcile the reviewed input or conflicting external state.
	ErrorPermanent ErrorKind = "permanent"
	// defaultRateLimitDelay respects GitHub's minimum wait for secondary rate limits without a hint.
	defaultRateLimitDelay = time.Minute
)

// DeliveryError exposes only bounded local messages, never provider response bodies
// or URLs that might contain credentials. Its cause remains available through errors.Is.
type DeliveryError struct {
	// Kind selects retry or needs-attention handling.
	Kind ErrorKind
	// RetryAfter is the provider's minimum retry delay; zero uses the caller's backoff.
	RetryAfter time.Duration
	// Code identifies a safe local failure category suitable for persistence.
	Code    string
	message string
	cause   error
}

func (e *DeliveryError) Error() string {
	if e.message != "" {
		return e.message
	}
	return "catalog guide delivery failed"
}

func (e *DeliveryError) Unwrap() error { return e.cause }

// Classify defaults unknown errors, including context cancellation, to transient.
// Only known permanent and configuration failures require operator intervention.
func Classify(err error) (ErrorKind, time.Duration) {
	if failure, ok := errors.AsType[*DeliveryError](err); ok {
		switch failure.Kind {
		case ErrorConfiguration, ErrorPermanent:
			return failure.Kind, 0
		case ErrorTransient:
			return ErrorTransient, max(failure.RetryAfter, 0)
		case ErrorUnknown:
			return ErrorTransient, 0
		}
	}
	return ErrorTransient, 0
}

func deliveryError(kind ErrorKind, code, message string, cause error) *DeliveryError {
	return &DeliveryError{Kind: kind, RetryAfter: 0, Code: code, message: message, cause: cause}
}

func classifyHTTP(response *http.Response, now time.Time) *DeliveryError {
	status := response.StatusCode
	rateLimited := status == http.StatusTooManyRequests
	if status == http.StatusForbidden {
		rateLimited = response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0"
		if !rateLimited {
			// Secondary rate limits can omit retry headers. The provider message is
			// inspected only for classification and never attached to an error.
			var body struct {
				// Message is GitHub's rate-limit explanation, never persisted or displayed.
				Message string `json:"message"`
			}
			if json.NewDecoder(io.LimitReader(response.Body, responseLimit)).Decode(&body) == nil {
				rateLimited = strings.Contains(strings.ToLower(body.Message), "rate limit")
			}
		}
	}
	if rateLimited {
		delay := retryAfter(response.Header, now, true)
		if delay <= 0 {
			delay = defaultRateLimitDelay
		}
		return &DeliveryError{Kind: ErrorTransient, RetryAfter: delay, Code: "rate_limited", message: "guide request was rate limited", cause: nil}
	}
	kind := ErrorPermanent
	code := "request_rejected"
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound:
		kind, code = ErrorConfiguration, "repository_access"
	case status == http.StatusRequestTimeout || status == http.StatusConflict || status >= http.StatusInternalServerError:
		kind, code = ErrorTransient, "provider_unavailable"
	}
	failure := deliveryError(kind, code, fmt.Sprintf("guide request returned HTTP %d", status), nil)
	if kind == ErrorTransient {
		failure.RetryAfter = retryAfter(response.Header, now, false)
	}
	return failure
}

func retryAfter(headers http.Header, now time.Time, includeReset bool) time.Duration {
	var delay time.Duration
	if raw := headers.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.ParseUint(raw, 10, 64); err == nil {
			// Saturation avoids wrapping oversized provider delays into immediate retries.
			if seconds > uint64(math.MaxInt64/int64(time.Second)) {
				delay = time.Duration(math.MaxInt64)
			} else {
				delay = time.Duration(seconds) * time.Second
			}
		} else if date, err := http.ParseTime(raw); err == nil {
			delay = max(date.Sub(now), 0)
		}
	}
	if seconds, err := strconv.ParseInt(headers.Get("X-RateLimit-Reset"), 10, 64); includeReset && err == nil && seconds > now.Unix() {
		delay = max(delay, time.Unix(seconds, 0).Sub(now))
	}
	return delay
}
