package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type cancellationError struct{ msg string }

func (e *cancellationError) Error() string { return e.msg }

var (
	cancelCauseHandlerCompleted error = &cancellationError{msg: "handler completed"}
	cancelCauseTimeout          error = &cancellationError{msg: "handler timeout"}
	cancelCauseTerminated       error = &cancellationError{msg: "terminated by function runner"}
)

type ToolCallRequest struct {
	ToolName    string            `json:"name"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment"`
}

func main() {
	if err := unzipCode("/data/code.zip", "/srv/app"); err != nil {
		fmt.Fprintf(os.Stderr, "failed to unzip code: %s\n", err.Error())
		os.Exit(1)
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	ctx, timeoutCancel := context.WithTimeoutCause(ctx, 5*time.Minute, cancelCauseTimeout)
	defer timeoutCancel()

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigch
		cancel(cancelCauseTerminated)
	}()

	mux := http.NewServeMux()

	mux.Handle("GET /healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	mux.Handle("POST /tool-call", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			var recErr error
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok {
					recErr = err
				} else {
					recErr = fmt.Errorf("panic: %v", rec)
				}
			}

			finalErr := cancelCauseHandlerCompleted
			if recErr != nil {
				finalErr = fmt.Errorf("%w: %w", cancelCauseHandlerCompleted, recErr)
			}

			cancel(finalErr)
		}()

		var req ToolCallRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			msg := fmt.Sprintf("invalid request: %s", err.Error())
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		stdoutReader, stdoutWriter, err := os.Pipe()
		if err != nil {
			msg := fmt.Sprintf("failed to create pipe: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
		defer stdoutWriter.Close()
		defer stdoutReader.Close()

		stderr := bytes.NewBuffer(nil)

		cmd := exec.CommandContext(r.Context(), "python3", "/srv/app/functions.py")
		cmd.Dir = "/srv/app"
		cmd.Stdout = stdoutWriter
		cmd.Stderr = stderr
		cmd.Env = make([]string, 0, len(req.Environment))
		for key, value := range req.Environment {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%q", key, value))
		}

		err = cmd.Start()
		if err != nil {
			msg := fmt.Sprintf("failed to execute tool: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}

		response, err := http.ReadResponse(bufio.NewReader(stdoutReader), nil)
		if err != nil {
			msg := fmt.Sprintf("failed to read response: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
		defer response.Body.Close()

		w.WriteHeader(response.StatusCode)
		if _, err := io.Copy(w, response.Body); err != nil {
			fmt.Fprintf(os.Stderr, "failed to copy response body: %s\n", err.Error())
		}

		if err := cmd.Wait(); err != nil {
			fmt.Fprintln(os.Stderr, stderr.String())
			fmt.Fprintln(os.Stderr, err.Error())
		}
	}))

	srv := &http.Server{
		Addr:              ":8888",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()

		var cancellation *cancellationError
		if errors.As(context.Cause(ctx), &cancellation) {
			fmt.Fprintf(os.Stderr, "shutting down: %s\n", cancellation.Error())
		} else {
			fmt.Fprintln(os.Stderr, "shutting down")
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown error: %s\n", err.Error())
		}
	}()

	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "server error: %s\n", err.Error())
		os.Exit(1)
	}
}

func unzipCode(zipPath string, dest string) error {
	zipFile, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("%s: open zip file: %w", zipPath, err)
	}
	defer zipFile.Close()

	for _, file := range zipFile.File {
		if err := handleZipFile(file, dest); err != nil {
			return err
		}
	}

	if err := os.Remove(zipPath); err != nil {
		return fmt.Errorf("%s: remove zip file: %w", zipPath, err)
	}

	return nil
}

func handleZipFile(file *zip.File, dest string) error {
	path := filepath.Join(dest, file.Name)

	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("%s: create directory: %w", path, err)
		}
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("%s: failed to create directory: %w", dir, err)
	}

	fileReader, err := file.Open()
	if err != nil {
		return fmt.Errorf("%s: open file in zip: %w", file.Name, err)
	}
	defer fileReader.Close()

	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("%s: create target file: %w", path, err)
	}
	defer targetFile.Close()

	_, err = io.Copy(targetFile, fileReader)
	if err != nil {
		return fmt.Errorf("%s: extract file: %w", file.Name, err)
	}

	return nil
}
