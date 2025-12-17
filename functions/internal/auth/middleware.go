package auth

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
)

func AuthorizeRequest(logger *slog.Logger, enc *encryption.Client, handler http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("auth"))
	text := http.StatusText(http.StatusUnauthorized)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		header := r.Header.Get("Authorization")
		if header == "" {
			logger.InfoContext(ctx, "no authorization header provided")
			http.Error(w, text, http.StatusUnauthorized)
			return
		}
		if len(header) <= len("bearer ") {
			logger.InfoContext(ctx, "invalid authorization header")
			http.Error(w, text, http.StatusUnauthorized)
			return
		}

		token := header[len("bearer "):]
		if len(token) < 4 {
			logger.InfoContext(ctx, "gram functions bearer token is missing version prefix")
			http.Error(w, text, http.StatusUnauthorized)
			return
		}

		version, token := token[:4], token[4:]
		var err error
		switch version {
		case "v01.":
			if payload, autherr := authorizeV1(enc, token); autherr == nil {
				w.Header().Set("Gram-Invoke-ID", payload.ID)
				ctx = WithContext(ctx, &AuthContext{
					InvocationID: payload.ID,
					Subject:      payload.Subject,
				})
				r = r.WithContext(ctx)
			} else {
				err = autherr
			}
		default:
			err = fmt.Errorf("unsupported bearer token version")
		}

		if err != nil {
			logger.WarnContext(ctx, "bad authorization credentials", attr.SlogError(err))
			http.Error(w, text, http.StatusUnauthorized)
			return
		}

		handler.ServeHTTP(w, r)
	})
}
