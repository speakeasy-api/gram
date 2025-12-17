package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/svc"
)

type CallResourcePayload struct {
	URI         string            `json:"uri"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}

func (s *Service) handleResourceRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authCtx := auth.FromContext(ctx)
	if authCtx == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload CallResourcePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.ErrorContext(ctx, "failed to decode resource request", attr.SlogError(err))

		msg := fmt.Sprintf("decode resource request: %s", err.Error())
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	logger := s.logger.With(
		attr.SlogURN(authCtx.Subject),
	)

	err := s.getResource(ctx, logger, payload, w)
	if err != nil {
		s.handleError(ctx, err, "call resource", w)
		return
	}
}

func (s *Service) getResource(ctx context.Context, logger *slog.Logger, payload CallResourcePayload, w http.ResponseWriter) error {
	if payload.URI == "" {
		return svc.NewPermanentError(
			fmt.Errorf("invalid request: missing uri"),
			http.StatusBadRequest,
		)
	}

	reqCopy := payload
	reqCopy.Environment = nil
	reqArg, err := json.Marshal(reqCopy)
	if err != nil {
		return svc.NewPermanentError(
			fmt.Errorf("serialize resource request: %w", err),
			http.StatusInternalServerError,
		)
	}

	if len(payload.Input) == 0 {
		return svc.NewPermanentError(
			fmt.Errorf("invalid request: missing input"),
			http.StatusBadRequest,
		)
	}

	return s.executeRequest(ctx, logger, callRequest{
		requestArg:  reqArg,
		environment: payload.Environment,
		requestType: "resource",
	}, w)
}
