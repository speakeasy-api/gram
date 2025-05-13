package oops

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	goa "goa.design/goa/v3/pkg"
)

func ErrHandle(logger *slog.Logger, handler func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := handler(w, r)
		if err == nil {
			return
		}

		code := http.StatusInternalServerError
		var payload any
		var se *ShareableError
		if errors.As(err, &se) {
			code = se.HTTPStatus()
			payload = se.AsGoa()
		} else {
			gerr := goa.NewServiceError(
				errors.New(CodeUnexpected.UserMessage()),
				string(CodeUnexpected),
				false,
				false,
				true,
			)
			logger.ErrorContext(r.Context(), "unexpected error", slog.String("error_id", gerr.ID), slog.String("error", err.Error()))
			payload = gerr
		}

		w.WriteHeader(code)
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(payload)
		if err != nil {
			logger.ErrorContext(r.Context(), "failed to encode response", slog.String("error", err.Error()))
		}
	})
}
