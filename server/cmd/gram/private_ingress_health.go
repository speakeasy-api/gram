package gram

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/netingress"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func privateIngressReadinessHandler(reviewer netingress.TokenReviewer, tokenFile string, dependencies http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			// Read on each probe so projected service-account token rotation is respected.
			token, err := os.ReadFile(tokenFile) // #nosec G304 -- fixed pod mount path supplied at startup, never request input.
			if err != nil || len(strings.TrimSpace(string(token))) == 0 {
				http.Error(w, "private ingress token review unavailable", http.StatusServiceUnavailable)
				return
			}
			// An omitted audience uses the API server audience of this pod's token,
			// not the separate audience required for ingress attestors.
			//nolint:exhaustruct // TokenReview request only sets spec; Kubernetes owns metadata and status.
			review, err := reviewer.Create(ctx, &authenticationv1.TokenReview{
				Spec: authenticationv1.TokenReviewSpec{Token: strings.TrimSpace(string(token)), Audiences: nil},
			}, metav1.CreateOptions{})
			if err != nil || review == nil || !review.Status.Authenticated || review.Status.Error != "" {
				// Neither API errors nor review status are safe to echo with a credential-bearing request.
				http.Error(w, "private ingress token review unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		dependencies.ServeHTTP(w, r)
	})
}
