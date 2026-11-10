package middleware

import (
	"net/http"
	"strconv"
	"time"
)

const (
	// deprecationHeader carries the RFC 9745 deprecation date as a structured-field date.
	deprecationHeader = "Deprecation"

	// sunsetHeader carries the RFC 8594 removal date as an HTTP-date.
	sunsetHeader = "Sunset"
)

// DeprecatedEndpoint marks every response from the wrapped handler with the RFC 9745 Deprecation and
// RFC 8594 Sunset headers.
func DeprecatedEndpoint(deprecatedAt, sunset time.Time) func(http.Handler) http.Handler {
	deprecation := "@" + strconv.FormatInt(deprecatedAt.Unix(), 10)
	sunsetValue := sunset.UTC().Format(http.TimeFormat)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(deprecationHeader, deprecation)
			w.Header().Set(sunsetHeader, sunsetValue)
			next.ServeHTTP(w, r)
		})
	}
}
