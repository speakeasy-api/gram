package runner

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

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

	// maxConcurrency bounds the number of tool/resource calls executing at
	// once. It mirrors the Fly proxy hard concurrency limit so memory tiers no
	// longer inflate the request cap. A value <= 0 disables limiting.
	maxConcurrency int

	// slots is a weighted semaphore sized to maxConcurrency; one unit is held
	// for the duration of each in-flight execution. It is nil when limiting is
	// disabled.
	slots *semaphore.Weighted

	// inFlight tracks the number of executions currently holding a slot, for
	// saturation instrumentation.
	inFlight atomic.Int64

	// holdTimeout is how long a request waits for a free slot before the runner
	// sheds it with a 429. The brief hold lets the Fly proxy observe sustained
	// soft-concurrency pressure (and trigger autostart) instead of the
	// connection being freed immediately by a fast rejection.
	holdTimeout time.Duration

	// retryAfter is advertised in the Retry-After header on a shed request,
	// hinting how long the client should wait before retrying.
	retryAfter time.Duration
}

func NewService(
	logger *slog.Logger,
	enc *encryption.Client,
	workDir string,
	cmd string,
	cmdArgs []string,
	maxConcurrency int,
) *Service {
	var slots *semaphore.Weighted
	if maxConcurrency > 0 {
		slots = semaphore.NewWeighted(int64(maxConcurrency))
	}

	return &Service{
		logger: logger.With(
			attr.SlogComponent("runner"),
			attr.SlogMaxConcurrency(maxConcurrency),
		),
		encryption:     enc,
		workDir:        workDir,
		command:        cmd,
		args:           cmdArgs,
		maxConcurrency: maxConcurrency,
		slots:          slots,
		inFlight:       atomic.Int64{},
		holdTimeout:    defaultHoldTimeout,
		retryAfter:     defaultRetryAfter,
	}
}

func (s *Service) Attach(mux *http.ServeMux) {
	mux.Handle(
		"POST /tool-call",
		auth.AuthorizeRequest(s.logger, s.encryption, s.limit(http.HandlerFunc(s.handleToolCall))),
	)

	mux.Handle(
		"POST /resource-request",
		auth.AuthorizeRequest(s.logger, s.encryption, s.limit(http.HandlerFunc(s.handleResourceRequest))),
	)
}
