package middleware

import (
	"net/http"
	"strings"
)

// PrivateIngressAttestationHeader is reserved for workload attestation on the
// isolated private listener. Public requests must never be able to supply it.
const PrivateIngressAttestationHeader = "X-Gram-Network-Ingress-Attestation"

// StripPrivateIngressHeaders removes provider identity and Gram attestation
// headers from the public listener before tracing, logging, or context
// extraction. Header names are case-insensitive under net/http.
func StripPrivateIngressHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if strings.HasPrefix(strings.ToLower(name), "tailscale-") {
				r.Header.Del(name)
			}
		}
		r.Header.Del(PrivateIngressAttestationHeader)
		next.ServeHTTP(w, r)
	})
}
