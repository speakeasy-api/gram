package middleware

import (
	"net/http"

	"github.com/speakeasy-api/gram/functions/buildinfo"
)

func NewVersion(
	handler http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Gram-Runner-Version", buildinfo.Version)
		handler.ServeHTTP(w, r)
	})
}
