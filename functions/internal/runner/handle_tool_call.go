package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/guardian"
	"github.com/speakeasy-api/gram/functions/internal/ipc"
	"github.com/speakeasy-api/gram/functions/internal/o11y"
	"github.com/speakeasy-api/gram/functions/internal/svc"
)

const (
	cpuHeader           = "X-Gram-Functions-Cpu"
	memoryHeader        = "X-Gram-Functions-Memory"
	executionTimeHeader = "X-Gram-Functions-Execution-Time"
)

var allowedHeaders = map[string]struct{}{
	"content-type":            {},
	"retry-after":             {},
	"x-ratelimit-limit":       {},
	"x-ratelimit-remaining":   {},
	"x-ratelimit-reset-after": {},
	"x-ratelimit-reset":       {},
}

type CallToolPayload struct {
	ToolName    string            `json:"name"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}

type callRequest struct {
	requestArg  []byte
	environment map[string]string
	requestType string
}

func (s *Service) executeRequest(ctx context.Context, logger *slog.Logger, req callRequest, w http.ResponseWriter) error {
	fifoPath, cleanup, err := ipc.Mkfifo()
	if err != nil {
		return svc.Fault(
			fmt.Errorf("create pipe: %w", err),
			http.StatusInternalServerError,
		)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return cleanup() })

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer timeoutCancel()

	var logwg sync.WaitGroup
	stdoutRdr, stdoutWrt := io.Pipe()
	stderrRdr, stderrWrt := io.Pipe()
	logwg.Go(func() {
		if err := o11y.CaptureRawLogLines(ctx, logger, stdoutRdr, attr.SlogDevice("stdout"), attr.SlogEventOrigin("user")); err != nil {
			s.logger.ErrorContext(ctx, "failed to capture stdout log lines", attr.SlogError(err))
		}
	})
	logwg.Go(func() {
		if err := o11y.CaptureRawLogLines(ctx, logger, stderrRdr, attr.SlogDevice("stderr"), attr.SlogEventOrigin("user")); err != nil {
			s.logger.ErrorContext(ctx, "failed to capture stderr log lines", attr.SlogError(err))
		}
	})
	defer o11y.LogDefer(ctx, logger, func() error {
		var err error
		if e := stdoutWrt.Close(); e != nil {
			err = errors.Join(err, fmt.Errorf("close stdout writer: %w", e))
		}
		if e := stderrWrt.Close(); e != nil {
			err = errors.Join(err, fmt.Errorf("close stderr writer: %w", e))
		}

		logwg.Wait()
		return err

	})

	args := s.args
	args = append(args, fifoPath, string(req.requestArg), req.requestType)
	cmd := guardian.NewCommand(timeoutCtx, s.command, args...)
	cmd.Dir = s.workDir
	cmd.Stdout = stdoutWrt
	cmd.Stderr = stderrWrt

	cmd.Env = make([]string, 0, len(req.environment))
	for key, value := range req.environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	startTime := time.Now()
	err = cmd.Start()
	if err != nil {
		return svc.NewPermanentError(
			fmt.Errorf("execute %s: %w", req.requestType, err),
			http.StatusInternalServerError,
		)
	}

	// Open the FIFO for reading in a separate goroutine to avoid blocking
	// indefinitely.
	// Using syscall.O_NONBLOCK is not a good idea because the sub-process
	// might not open the FIFO for writing by the time this process attempts
	// to read from the pipe. This can result in an io.UnexpectedEOF error.
	pipech := make(chan *os.File, 1)
	errch := make(chan error, 1)
	go func() {
		resultReader, err := os.OpenFile(filepath.Clean(fifoPath), os.O_RDONLY, os.ModeNamedPipe)
		if err != nil {
			errch <- err
			return
		}
		pipech <- resultReader
	}()

	var pipe *os.File
	select {
	case <-ctx.Done():
		return svc.NewTemporaryError(
			fmt.Errorf("timed out waiting for sub-process: %w", ctx.Err()),
			http.StatusRequestTimeout,
		)
	case err := <-errch:
		return svc.Fault(
			fmt.Errorf("open pipe (ro): %w", err),
			http.StatusInternalServerError,
		)
	case f := <-pipech:
		pipe = f
		defer o11y.LogDefer(ctx, logger, func() error { return pipe.Close() })
	}

	response, err := http.ReadResponse(bufio.NewReader(pipe), nil)
	if err != nil {
		return svc.NewPermanentError(
			fmt.Errorf("read response: %w", err),
			http.StatusInternalServerError,
		)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return response.Body.Close() })

	ct := response.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "application/vnd.fly.replay") {
		return svc.NewPermanentError(
			fmt.Errorf("function attempted fly replay"),
			http.StatusBadGateway,
		)
	}

	for key, values := range response.Header {
		if _, ok := allowedHeaders[strings.ToLower(key)]; !ok {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// The following two headers must be removed to prevent conflicts with
	// chunked encoding and trailers.
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Encoding")
	// Announce trailers for resource usage metrics
	// We currently have to write these after WriteHeader
	w.Header().Set("Trailer", cpuHeader+", "+memoryHeader+", "+executionTimeHeader)

	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		s.logger.ErrorContext(ctx, "failed to copy response body", attr.SlogError(err))
	}

	err = cmd.Wait()
	executionTime := time.Since(startTime).Seconds()

	// Write resource usage as trailers after response body is sent
	w.Header().Set(executionTimeHeader, fmt.Sprintf("%.17g", executionTime))

	if cmd.ProcessState != nil {
		sysUsage := cmd.ProcessState.SysUsage()
		if usage, ok := sysUsage.(*syscall.Rusage); ok {
			// Convert CPU time to seconds (user + system time)
			cpuSeconds := float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1000000 +
				float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1000000
			w.Header().Set(cpuHeader, fmt.Sprintf("%.17g", cpuSeconds))

			// Get total system RAM in GB
			if memGB := getTotalMemoryGB(); memGB > 0 {
				w.Header().Set(memoryHeader, fmt.Sprintf("%.17g", memGB))
			}
		}
	}

	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		s.logger.ErrorContext(ctx, "sub-process exited with non-zero status", attr.SlogError(err), attr.SlogProcessExitCode(exitErr.ExitCode()))
	case err != nil:
		s.logger.ErrorContext(ctx, "sub-process failed", attr.SlogError(err))
	default:
		s.logger.InfoContext(ctx, "sub-process completed successfully")
	}

	return nil
}

func (s *Service) callTool(ctx context.Context, logger *slog.Logger, payload CallToolPayload, w http.ResponseWriter) error {
	if payload.ToolName == "" {
		return svc.NewPermanentError(
			fmt.Errorf("invalid request: missing name"),
			http.StatusBadRequest,
		)
	}

	reqCopy := payload
	reqCopy.Environment = nil
	reqArg, err := json.Marshal(reqCopy)
	if err != nil {
		return svc.NewPermanentError(
			fmt.Errorf("serialize tool call request: %w", err),
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
		requestType: "tool",
	}, w)
}

func (s *Service) handleToolCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authCtx := auth.FromContext(ctx)
	if authCtx == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload CallToolPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.ErrorContext(ctx, "failed to decode tool call request", attr.SlogError(err))

		msg := fmt.Sprintf("decode tool call request: %s", err.Error())
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	logger := s.logger.With(
		attr.SlogURN(authCtx.Subject),
	)

	err := s.callTool(ctx, logger, payload, w)
	if err != nil {
		s.handleError(ctx, err, "call tool", w)
		return
	}
}

func (s *Service) handleError(ctx context.Context, err error, operation string, w http.ResponseWriter) {
	var methodError *svc.MethodError
	switch {
	case errors.As(err, &methodError):
		s.logger.ErrorContext(ctx, operation, attr.SlogError(methodError), attr.SlogErrorID(methodError.ID))
		bs, err := json.Marshal(methodError)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to serialize method error", attr.SlogError(err))
			http.Error(w, methodError.Message, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(methodError.Code())
		if _, err := w.Write(bs); err != nil {
			s.logger.ErrorContext(ctx, "failed to write method error response", attr.SlogError(err))
		}
	case err != nil:
		s.logger.ErrorContext(ctx, operation, attr.SlogError(err))
		http.Error(w, "unexpected server error", http.StatusInternalServerError)
	}
}
