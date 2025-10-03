package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
	"github.com/speakeasy-api/gram/functions/internal/javascript"
	"github.com/speakeasy-api/gram/functions/internal/middleware"
	"github.com/speakeasy-api/gram/functions/internal/o11y"
	"github.com/speakeasy-api/gram/functions/internal/python"
	"github.com/speakeasy-api/gram/functions/internal/runner"
	"github.com/speakeasy-api/gram/functions/internal/svc"
)

var (
	language = flag.String("language", "javascript", "Programming language for the function. Can be 'javascript', 'typescript', or 'python'.")
	codePath = flag.String("codePath", "/data/code.zip", "Path to the code zip file")
	workDir  = flag.String("workDir", "/srv/app", "Working directory for the application")
)

type ToolCallRequest struct {
	ToolName    string            `json:"name"`
	Input       json.RawMessage   `json:"input"`
	Environment map[string]string `json:"environment,omitempty,omitzero"`
}

type runnerArgs struct {
	codePath string
	workDir  string
	language string
}

func main() {
	ctx := context.Background()

	pretty, _ := strconv.ParseBool(os.Getenv("GRAM_LOG_PRETTY"))
	logger := enrichLogger(o11y.NewLogger(os.Stderr, o11y.LoggerOptions{
		Pretty:      pretty,
		DataDogAttr: false,
	}))

	flag.CommandLine.SetOutput(os.Stderr)
	flag.Parse()

	if err := run(ctx, logger); err != nil {
		logger.ErrorContext(ctx, "fatal error", attr.SlogError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	o11y.SetupOTelSDK(ctx, logger)

	args, err := validateArgs()
	if err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	if err := unzipCode(ctx, logger, args.codePath, args.workDir); err != nil {
		return fmt.Errorf("unzip code: %w", err)
	}

	command, program, err := prepareProgram(args.workDir, args.language)
	if err != nil {
		return fmt.Errorf("prepare program: %w", err)
	}

	authSecret := os.Getenv("GRAM_FUNCTIONS_SECRET")
	if authSecret == "" {
		return fmt.Errorf("GRAM_FUNCTIONS_SECRET is required")
	}

	enc, err := encryption.New(authSecret)
	if err != nil {
		return fmt.Errorf("create encryption client: %w", err)
	}

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigch
		cancel(svc.ErrTerminated)
	}()

	mux := http.NewServeMux()

	mux.Handle("GET /healthz", otelhttp.WithRouteTag("http.healthCheck", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))

	runner.NewService(logger, enc, args.workDir, command, program).Attach(mux)

	idle := svc.NewIdleTracker(time.Minute)
	srv := &http.Server{
		Addr:              ":8888",
		Handler:           middleware.NewRecovery(logger, otelhttp.NewHandler(mux, "http.server")),
		ReadHeaderTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		ConnState: idle.ConnState,
	}

	go func() {
		<-idle.Done()
		cancel(svc.ErrIdleServerTimeout)
	}()

	go func() {
		<-ctx.Done()

		var cancellation *svc.CancellationError
		if errors.As(context.Cause(ctx), &cancellation) {
			logger.ErrorContext(ctx, "shutting down", attr.SlogError(cancellation))
		} else {
			logger.InfoContext(ctx, "shutting down")
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(ctx, "shutdown error", attr.SlogError(err))
		}
	}()

	logger.InfoContext(ctx, "starting server", attr.SlogServerAddress(srv.Addr))
	err = srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func unzipCode(ctx context.Context, logger *slog.Logger, zipPath string, dest string) error {
	zipFile, err := zip.OpenReader(zipPath)
	if err != nil {
		if zipFile != nil {
			_ = zipFile.Close()
		}
		return fmt.Errorf("%s: open zip file: %w", zipPath, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return zipFile.Close() })

	for _, file := range zipFile.File {
		if err := handleZipFile(ctx, logger, file, dest); err != nil {
			return err
		}
	}

	return nil
}

func handleZipFile(ctx context.Context, logger *slog.Logger, file *zip.File, dest string) error {
	path := filepath.Clean(filepath.Join(dest, filepath.Clean(file.Name)))

	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(path, 0750); err != nil {
			return fmt.Errorf("%s: create directory: %w", path, err)
		}
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("%s: failed to create directory: %w", dir, err)
	}

	fileReader, err := file.Open()
	if err != nil {
		return fmt.Errorf("%s: open file in zip: %w", file.Name, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return fileReader.Close() })

	targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("%s: create target file: %w", path, err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return targetFile.Close() })

	// Limit extraction to 10MiB per file to prevent decompression bombs
	const maxFileSize = 10 * 1024 * 1024
	written, err := io.Copy(targetFile, io.LimitReader(fileReader, maxFileSize))
	if err != nil {
		return fmt.Errorf("%s: extract file: %w", file.Name, err)
	}

	if written < 0 || file.UncompressedSize64 > uint64(written) {
		return fmt.Errorf("%s: file too large (>%d bytes, wrote %d bytes)", file.Name, maxFileSize, written)
	}

	return nil
}

func validateArgs() (*runnerArgs, error) {
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
		if err := os.MkdirAll(*workDir, 0750); err != nil {
			return nil, fmt.Errorf("create dir: %s: %w", *workDir, err)
		}
	default:
		return nil, fmt.Errorf("stat: %s: %w", *workDir, err)
	}

	return &runnerArgs{
		codePath: *codePath,
		workDir:  *workDir,
		language: *language,
	}, nil
}

func prepareProgram(workDir string, language string) (string, string, error) {
	switch language {
	case "javascript", "typescript":
		entryPath := filepath.Join(workDir, "gram-start.js")
		if err := os.WriteFile(entryPath, javascript.Entrypoint, 0600); err != nil {
			return "", "", fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		functionsPath := filepath.Join(workDir, "functions.js")
		stat, err := os.Stat(functionsPath)
		switch {
		case errors.Is(err, os.ErrNotExist), err == nil && stat.Size() == 0:
			if err := os.WriteFile(functionsPath, javascript.DefaultFunctions, 0600); err != nil {
				return "", "", fmt.Errorf("write %s default functions (%s): %w", language, functionsPath, err)
			}
		case err != nil:
			return "", "", fmt.Errorf("stat %s: %w", functionsPath, err)
		}

		return "node", entryPath, nil
	case "python":
		entryPath := filepath.Join(workDir, "gram_start.py")
		if err := os.WriteFile(entryPath, python.Entrypoint, 0600); err != nil {
			return "", "", fmt.Errorf("write %s entrypoint (%s): %w", language, entryPath, err)
		}

		functionsPath := filepath.Join(workDir, "functions.py")
		stat, err := os.Stat(functionsPath)
		switch {
		case errors.Is(err, os.ErrNotExist) || (err == nil && stat.Size() == 0):
			if err := os.WriteFile(functionsPath, python.DefaultFunctions, 0600); err != nil {
				return "", "", fmt.Errorf("write %s default functions (%s): %w", language, functionsPath, err)
			}
		case err != nil:
			return "", "", fmt.Errorf("stat %s: %w", functionsPath, err)
		}

		return "python", entryPath, nil
	default:
		return "", "", fmt.Errorf("unsupported language: %s", language)
	}
}

func enrichLogger(logger *slog.Logger) *slog.Logger {
	attrs := make([]any, 0, 4)
	projectID := os.Getenv("GRAM_PROJECT_ID")
	if projectID != "" {
		attrs = append(attrs, attr.SlogProjectID(projectID))
	}
	projectSlug := os.Getenv("GRAM_PROJECT_SLUG")
	if projectSlug != "" {
		attrs = append(attrs, attr.SlogProjectSlug(projectSlug))
	}
	deploymentID := os.Getenv("GRAM_DEPLOYMENT_ID")
	if deploymentID != "" {
		attrs = append(attrs, attr.SlogDeploymentID(deploymentID))
	}
	functionID := os.Getenv("GRAM_FUNCTIONS_ID")
	if functionID != "" {
		attrs = append(attrs, slog.String("function_id", functionID))
	}

	return logger.With(attrs...)
}
