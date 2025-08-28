package main

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/javascript"
	"github.com/speakeasy-api/gram/functions/internal/python"
)

var (
	language = flag.String("language", "javascript", "Programming language for the function. Can be 'javascript', 'typescript', or 'python'.")
	codePath = flag.String("codePath", "/data/code.zip", "Path to the code zip file")
	workDir  = flag.String("workDir", "/srv/app", "Working directory for the application")
)

type cancellationError struct{ msg string }

func (e *cancellationError) Error() string { return e.msg }

var (
	cancelCauseHandlerCompleted error = &cancellationError{msg: "handler completed"}
	cancelCauseHostTimeout      error = &cancellationError{msg: "host timeout"}
	cancelCauseTerminated       error = &cancellationError{msg: "terminated by function runner"}
)

type ToolCallRequest struct {
	ToolName    string            `json:"name"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}

type Args struct {
	codePath string
	workDir  string
	language string
}

func main() {
	cmdOut := flag.CommandLine.Output()

	flag.Parse()

	args, err := validateArgs()
	if err != nil {
		flag.Usage()
		fmt.Fprintln(cmdOut)
		fmt.Fprintf(cmdOut, "invalid arguments: %s\n", err.Error())
		os.Exit(1)
		return
	}

	if err := unzipCode(args.codePath, args.workDir); err != nil {
		fmt.Fprintf(cmdOut, "unzip code: %s\n", err.Error())
		os.Exit(1)
		return
	}

	command, program, err := prepareProgram(args.workDir, args.language)
	if err != nil {
		fmt.Fprintf(cmdOut, "prepare program: %s\n", err.Error())
		os.Exit(1)
		return
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	ctx, timeoutCancel := context.WithTimeoutCause(ctx, 5*time.Minute, cancelCauseHostTimeout)
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
			msg := fmt.Sprintf("deserialize tool call request: %s", err.Error())
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		reqCopy := req
		reqCopy.Environment = nil
		reqArg, err := json.Marshal(reqCopy)
		if err != nil {
			msg := fmt.Sprintf("serialize tool call request: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}

		if len(req.Input) == 0 {
			msg := "invalid request: missing input"
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		fifoPath, cleanup, err := mkfifo()
		if err != nil {
			msg := fmt.Sprintf("create pipe: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
		defer cleanup()

		cmd := exec.CommandContext(r.Context(), command, filepath.Base(program), fifoPath, string(reqArg))
		cmd.Dir = args.workDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = make([]string, 0, len(req.Environment))
		for key, value := range req.Environment {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%q", key, value))
		}

		err = cmd.Start()
		if err != nil {
			msg := fmt.Sprintf("execute tool: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}

		// Open the FIFO for reading in a separate goroutine to avoid blocking
		// indefinitely.
		// Using syscall.O_NONBLOCK is not a good idea because the sub-process
		// might not open the FIFO for writing by the time this process attempts
		// to read from the pipe. This can result in an io.UnexpectedEOF error.
		pipech := make(chan *os.File, 1)
		errch := make(chan error, 1)
		go func() {
			resultReader, err := os.OpenFile(fifoPath, os.O_RDONLY, os.ModeNamedPipe)
			if err != nil {
				errch <- err
				return
			}
			pipech <- resultReader
		}()

		var pipe *os.File
		select {
		case <-r.Context().Done():
			http.Error(w, "timed out waiting for sub-process", http.StatusRequestTimeout)
			return
		case err := <-errch:
			msg := fmt.Sprintf("open pipe (ro): %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		case f := <-pipech:
			pipe = f
			defer pipe.Close()
		}

		response, err := http.ReadResponse(bufio.NewReader(pipe), nil)
		if err != nil {
			msg := fmt.Sprintf("read response: %s", err.Error())
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
		defer response.Body.Close()

		w.WriteHeader(response.StatusCode)
		if _, err := io.Copy(w, response.Body); err != nil {
			fmt.Fprintf(cmdOut, "copy response body: %s\n", err.Error())
		}

		if err := cmd.Wait(); err != nil {
			fmt.Fprintf(cmdOut, "tool execution error: %s\n", err.Error())
		}
	}))

	srv := &http.Server{
		Addr:              ":8888",
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()

		var cancellation *cancellationError
		if errors.As(context.Cause(ctx), &cancellation) {
			fmt.Fprintf(cmdOut, "shutting down: %s\n", cancellation.Error())
		} else {
			fmt.Fprintln(cmdOut, "shutting down")
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(cmdOut, "shutdown error: %s\n", err.Error())
		}
	}()

	err = srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(cmdOut, "server error: %s\n", err.Error())
		os.Exit(1)
		return
	}
}

func unzipCode(zipPath string, dest string) error {
	zipFile, err := zip.OpenReader(zipPath)
	if err != nil {
		if zipFile != nil {
			zipFile.Close()
		}
		return fmt.Errorf("%s: open zip file: %w", zipPath, err)
	}
	defer zipFile.Close()

	for _, file := range zipFile.File {
		if err := handleZipFile(file, dest); err != nil {
			return err
		}
	}

	return nil
}

func handleZipFile(file *zip.File, dest string) error {
	path := filepath.Join(dest, file.Name)

	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(path, 0755); err != nil {
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

	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
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

func validateArgs() (*Args, error) {
	if language == nil || *language == "" {
		return nil, fmt.Errorf("language is required")
	}
	switch *language {
	case "javascript", "typescript", "python":
	default:
		return nil, fmt.Errorf("unsupported language: %s", *language)
	}

	if codePath == nil || *codePath == "" {
		return nil, fmt.Errorf("codePath is required")
	}

	codeStat, err := os.Stat(*codePath)
	if err != nil {
		return nil, fmt.Errorf("stat: %s: %w", *codePath, err)
	}
	if !codeStat.Mode().IsRegular() {
		return nil, fmt.Errorf("stat: %s: not a regular file", *codePath)
	}

	if workDir == nil || *workDir == "" {
		return nil, fmt.Errorf("workDir is required")
	}

	wdStat, err := os.Stat(*workDir)
	switch {
	case err == nil:
		if !wdStat.IsDir() {
			return nil, fmt.Errorf("stat: %s: not a directory", *workDir)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(*workDir, 0755); err != nil {
			return nil, fmt.Errorf("create dir: %s: %w", *workDir, err)
		}
	default:
		return nil, fmt.Errorf("stat: %s: %w", *workDir, err)
	}

	return &Args{
		codePath: *codePath,
		workDir:  *workDir,
		language: *language,
	}, nil
}

func prepareProgram(workDir string, language string) (string, string, error) {
	switch language {
	case "javascript", "typescript":
		entryPath := filepath.Join(workDir, "gram-start.mjs")

		if err := os.WriteFile(entryPath, javascript.Entrypoint, 0644); err != nil {
			return "", "", fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		return "node", entryPath, nil
	case "python":
		entryPath := filepath.Join(workDir, "gram_start.py")

		if err := os.WriteFile(entryPath, python.Entrypoint, 0644); err != nil {
			return "", "", fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		return "python", entryPath, nil
	default:
		return "", "", fmt.Errorf("unsupported language: %s", language)
	}
}

func mkfifo() (string, func() error, error) {
	if runtime.GOOS == "windows" {
		return "", nil, fmt.Errorf("named pipes on Windows are not supported in this implementation")
	}

	suffix, err := alphanum(8)
	if err != nil {
		return "", nil, fmt.Errorf("generate fifo suffix: %w", err)
	}

	tmpDir := os.TempDir()
	path := filepath.Join(tmpDir, fmt.Sprintf("fifo-%s", suffix))

	err = syscall.Mkfifo(path, 0666)
	if err != nil {
		return "", nil, fmt.Errorf("make fifo %s: %w", path, err)
	}

	cleanup := func() error {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove fifo %s: %w", path, err)
		}
		return nil
	}

	return path, cleanup, nil
}

func alphanum(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	maxnum := big.NewInt(int64(len(charset)))

	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, maxnum)
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}

	return string(result), nil
}
