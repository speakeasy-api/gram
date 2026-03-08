package ratelimit

import (
	"net/http"
	"strconv"
)

// Header names for rate limit response headers.
const (
	HeaderRateLimitLimit     = "X-RateLimit-Limit"
	HeaderRateLimitRemaining = "X-RateLimit-Remaining"
	HeaderRateLimitReset     = "X-RateLimit-Reset"
)

// SetHeaders sets rate limit headers on the HTTP response.
func SetHeaders(w http.ResponseWriter, result Result) {
	h := w.Header()
	h.Set(HeaderRateLimitLimit, strconv.Itoa(result.Limit))
	h.Set(HeaderRateLimitRemaining, strconv.Itoa(result.Remaining))
	h.Set(HeaderRateLimitReset, strconv.FormatInt(result.ResetAt.Unix(), 10))
}
