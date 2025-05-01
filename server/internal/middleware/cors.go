package middleware

import (
	"net/http"
	"net/url"
)

func CORSMiddleware(env string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch env {
			case "minikube":
				fallthrough
			case "local":
				w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
				// Make this work on different ports
				origin := r.Header.Get("Origin")
				if origin != "" {
					originURL, err := url.Parse(origin)
					if err == nil && originURL.Hostname() == "localhost" {
						w.Header().Set("Access-Control-Allow-Origin", origin)
					}
				}
			case "dev":
				w.Header().Set("Access-Control-Allow-Origin", "http://dev.getgram.ai")
			case "prod":
				w.Header().Set("Access-Control-Allow-Origin", "http://prod.getgram.ai")
				w.Header().Set("Access-Control-Allow-Origin", "http://getgram.ai")
			default:
				// No CORS headers set for unspecified environments
			}

			w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, Gram-Session, Gram-Project, Gram-Token, idempotency-key, Gram-Admin-Override")
			w.Header().Set("Access-Control-Expose-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Gram-Session")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func DevCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173")
		// Make this work on different ports
		origin := r.Header.Get("Origin")
		if origin != "" {
			originURL, err := url.Parse(origin)
			if err == nil && originURL.Hostname() == "localhost" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, Gram-Session, Gram-Project, Gram-Token, idempotency-key, Gram-Admin-Override")
		w.Header().Set("Access-Control-Expose-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Gram-Session")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
