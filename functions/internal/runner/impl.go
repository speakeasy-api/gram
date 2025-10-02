package runner

import (
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Service struct {
	logger     *slog.Logger
	encryption *encryption.Client

	workDir string
	command string
	program string
}

func NewService(
	logger *slog.Logger,
	enc *encryption.Client,
	workDir string,
	command string,
	program string,
) *Service {
	return &Service{
		logger: logger.With(
			attr.SlogComponent("runner"),
		),
		encryption: enc,
		workDir:    workDir,
		command:    command,
		program:    program,
	}
}

func (s *Service) Attach(mux *http.ServeMux) {
	mux.Handle(
		"POST /tool-call",
		otelhttp.WithRouteTag(
			"http.toolCall",
			auth.AuthorizeRequest(s.logger, s.encryption, http.HandlerFunc(s.handleToolCall)),
		),
	)

}
