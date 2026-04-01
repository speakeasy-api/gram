package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	goa "goa.design/goa/v3/pkg"
	"golang.org/x/net/http/httpguts"
)

func NewRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	logger = logger.With(attr.SlogComponent("recovery_middleware"))

	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recValue := recover(); recValue != nil {
					maybeErr, _ := recValue.(error)

					// if the response to client is being aborted with this sentinel
					// error then repanic with it. It will deliberately not be
					// logged by this middleware or anything above.
					if errors.Is(maybeErr, http.ErrAbortHandler) {
						panic(recValue)
					}

					ctx := r.Context()

					code := http.StatusInternalServerError
					payload := goa.NewServiceError(
						errors.New(oops.CodeUnexpected.UserMessage()),
						string(oops.CodeUnexpected),
						false,
						false,
						true,
					)

					errID := payload.ID
					var se *oops.ShareableError
					if errors.As(maybeErr, &se) {
						code = se.HTTPStatus()
						payload = se.AsGoa()
						errID = payload.ID
					}

					attrs := []slog.Attr{
						attr.SlogError(fmt.Errorf("panic: %v", recValue)),
						attr.SlogErrorKind("panic"),
						attr.SlogErrorStack(string(debug.Stack())),
					}
					if errID != "" {
						attrs = append(attrs, attr.SlogErrorID(errID))
					}

					logger.LogAttrs(ctx, slog.LevelError, "recovered from panic", attrs...)

					// Skip writing an HTTP error response for upgraded connections
					// ((e.g. WebSocket). The underlying ResponseWriter is no longer
					// usable after hijack, and writing to it causes undefined
					// behavior.
					// Use httpguts to handle case-insensitive matching and
					// multi-value Connection headers (e.g. "keep-alive, Upgrade").
					// Reference: https://github.com/go-chi/chi/issues/661
					if httpguts.HeaderValuesContainsToken(r.Header["Connection"], "upgrade") {
						return
					}

					w.Header().Set("Connection", "close")
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(code)
					err := json.NewEncoder(w).Encode(payload)
					if err != nil {
						logger.ErrorContext(ctx, "failed to encode response", attr.SlogError(err))
					}
				}
			}()

			next.ServeHTTP(w, r)
		}

		return http.HandlerFunc(fn)
	}
}
