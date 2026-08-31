package middleware

import (
	"net/http"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/networkaccess"
)

// NetworkServingPolicyVersion stamps every response with the serving-policy
// contract implemented by this process. Rollout tooling can probe each pod
// directly and must not enable non-public writes until every serving pod
// reports at least the required version.
func NetworkServingPolicyVersion(next http.Handler) http.Handler {
	value := strconv.Itoa(networkaccess.ServingPolicyVersion)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(networkaccess.ServingPolicyVersionHeader, value)
		next.ServeHTTP(w, r)
	})
}
