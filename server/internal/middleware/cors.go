package middleware

import (
	"net/http"
	"net/url"
)

func CORSMiddleware(env string, serverURL string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch env {
			case "local", "minikube":
				origin := r.Header.Get("Origin")
				if _, err := url.Parse(origin); err == nil {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				}
			case "dev", "prod":
				w.Header().Set("Access-Control-Allow-Origin", serverURL)
			default:
				// No CORS headers set for unspecified environments
			}

			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, Gram-Session, Gram-Project, Gram-Token, idempotency-key, Gram-Admin-Override, Gram-Chat-ID")
			w.Header().Set("Access-Control-Expose-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Gram-Session, Gram-Chat-ID")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
