package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
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

	"github.com/speakeasy-api/gram/functions/buildinfo"
	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/bootstrap"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
	"github.com/speakeasy-api/gram/functions/internal/middleware"
	"github.com/speakeasy-api/gram/functions/internal/o11y"
	"github.com/speakeasy-api/gram/functions/internal/runner"
	"github.com/speakeasy-api/gram/functions/internal/svc"
)

var (
	language = flag.String("language", "javascript", "Programming language for the function. Can be 'javascript', 'typescript', or 'python'.")
	codePath = flag.String("codePath", "/data/code.zip", "Path to the code zip file")
	workDir  = flag.String("workDir", "/var/task", "Working directory for the application")
	version  = flag.Bool("version", false, "Print version information and exit")
	doinit   = flag.Bool("init", false, "Initialize the filesystem and exit")
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
	if *version {
		fmt.Printf("version: %s\ncommit: %s\ndate: %s\n",
			buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	o11y.SetupOTelSDK(ctx, logger)

	args, err := sanitizeArgs()
	if err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	if *doinit {
		logger.InfoContext(ctx, "initializing function runtime")
		if _, _, err := bootstrap.InitializeMachine(ctx, logger, args.language, args.codePath, args.workDir); err != nil {
			return fmt.Errorf("initialize machine: %w", err)
		}
		logger.InfoContext(ctx, "initialized function runtime")
		return nil
	}

	if os.Geteuid() == 0 {
		return fmt.Errorf("refusing to run as root")
	}

	command, program, err := bootstrap.ResolveProgram(args.language, args.workDir)
	if err != nil {
		return fmt.Errorf("resolve program: %w", err)
	}

	authSecret := os.Getenv("GRAM_FUNCTION_AUTH_SECRET")
	if authSecret == "" {
		return fmt.Errorf("GRAM_FUNCTION_AUTH_SECRET is required")
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

	var handler http.Handler = mux
	handler = middleware.NewVersion(handler)
	handler = middleware.NewRecovery(logger, handler)
	handler = otelhttp.NewHandler(handler, "http.server")

	idle := svc.NewIdleTracker(time.Minute)
	srv := &http.Server{
		Addr:              ":8888",
		Handler:           handler,
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

func sanitizeArgs() (*runnerArgs, error) {
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

	wd, err := filepath.Abs(*workDir)
	if err != nil {
		return nil, fmt.Errorf("get absolute path: %s: %w", *workDir, err)
	}

	wdStat, err := os.Stat(wd)
	switch {
	case err == nil:
		if !wdStat.IsDir() {
			return nil, fmt.Errorf("stat: %s: not a directory", wd)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(wd, 0750); err != nil {
			return nil, fmt.Errorf("create dir: %s: %w", wd, err)
		}
	default:
		return nil, fmt.Errorf("stat: %s: %w", wd, err)
	}

	return &runnerArgs{
		codePath: *codePath,
		workDir:  wd,
		language: *language,
	}, nil
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
