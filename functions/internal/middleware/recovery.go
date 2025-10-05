package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/speakeasy-api/gram/functions/internal/attr"
)

func NewRecovery(
	logger *slog.Logger,
	handler http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				var recErr error
				if err, ok := rec.(error); ok {
					recErr = err
				} else {
					recErr = fmt.Errorf("panic: %v", rec)
				}

				logger.ErrorContext(r.Context(), "panic recovered", attr.SlogError(recErr), attr.SlogExceptionStacktrace(string(debug.Stack())))
				w.Header().Set("Connection", "close")
				http.Error(w, "unhandled server error", http.StatusInternalServerError)
			}
		}()

		handler.ServeHTTP(w, r)
	})
}
