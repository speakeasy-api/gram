package runner

import (
	"log/slog"
	"net/http"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
)

type Service struct {
	logger     *slog.Logger
	encryption *encryption.Client

	workDir string
	command string
	args    []string
}

func NewService(
	logger *slog.Logger,
	enc *encryption.Client,
	workDir string,
	cmd string,
	cmdArgs []string,
) *Service {
	return &Service{
		logger: logger.With(
			attr.SlogComponent("runner"),
		),
		encryption: enc,
		workDir:    workDir,
		command:    cmd,
		args:       cmdArgs,
	}
}

func (s *Service) Attach(mux *http.ServeMux) {
	mux.Handle(
		"POST /tool-call",
		auth.AuthorizeRequest(s.logger, s.encryption, http.HandlerFunc(s.handleToolCall)),
	)

	mux.Handle(
		"POST /resource-request",
		auth.AuthorizeRequest(s.logger, s.encryption, http.HandlerFunc(s.handleResourceRequest)),
	)
}
