package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/speakeasy-api/gram/functions/buildinfo"
	"github.com/speakeasy-api/gram/functions/internal/attr"
	"github.com/speakeasy-api/gram/functions/internal/auth"
	"github.com/speakeasy-api/gram/functions/internal/bootstrap"
	"github.com/speakeasy-api/gram/functions/internal/encryption"
	"github.com/speakeasy-api/gram/functions/internal/middleware"
	"github.com/speakeasy-api/gram/functions/internal/o11y"
	"github.com/speakeasy-api/gram/functions/internal/runner"
	"github.com/speakeasy-api/gram/functions/internal/svc"
	funcclient "github.com/speakeasy-api/gram/server/gen/functions"
	"github.com/speakeasy-api/gram/server/gen/http/functions/client"
	goahttp "goa.design/goa/v3/http"
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
	ident, err := identityFromEnv()
	logger := enrichLogger(o11y.NewLogger(os.Stderr, o11y.LoggerOptions{
		Pretty:      pretty,
		DataDogAttr: false,
	}), ident)
	if err != nil {
		logger.ErrorContext(ctx, "invalid environment", attr.SlogError(err))
		os.Exit(1)
	}

	flag.CommandLine.SetOutput(os.Stderr)
	flag.Parse()
	if err := run(ctx, logger, ident); err != nil {
		logger.ErrorContext(ctx, "fatal error", attr.SlogError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, ident auth.RunnerIdentity) error {
	if *version {
		fmt.Printf("version: %s\ncommit: %s\ndate: %s\n",
			buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return nil
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigch
		cancel(svc.ErrTerminated)
	}()

	o11y.SetupOTelSDK(ctx, logger)

	args, err := sanitizeArgs()
	if err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	serverClient, err := newServerClient()
	if err != nil {
		return fmt.Errorf("create server client: %w", err)
	}

	if *doinit {
		logger.InfoContext(ctx, "initializing function runtime")
		if _, _, err := bootstrap.InitializeMachine(ctx, logger, bootstrap.InitializeMachineConfig{
			Ident:        ident,
			ServerClient: serverClient,
			Language:     args.language,
			CodePath:     args.codePath,
			WorkDir:      args.workDir,
		}); err != nil {
			return fmt.Errorf("initialize machine: %w", err)
		}
		logger.InfoContext(ctx, "initialized function runtime")
		return nil
	}

	if os.Geteuid() == 0 {
		return fmt.Errorf("refusing to run as root")
	}

	cmd, cmdArgs, err := bootstrap.ResolveProgram(args.language, args.workDir)
	if err != nil {
		return fmt.Errorf("resolve program: %w", err)
	}

	enc, err := encryption.New(ident.AuthSecret.Reveal())
	if err != nil {
		return fmt.Errorf("create encryption client: %w", err)
	}

	mux := http.NewServeMux()

	mux.Handle("GET /healthz", otelhttp.WithRouteTag("http.healthCheck", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))

	runner.NewService(logger, enc, args.workDir, cmd, cmdArgs).Attach(mux)

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

// identityFromEnv constructs a RunnerIdentity from environment variables. If
// any required variable is missing, it returns an error _AND_ a partially
// filled RunnerIdentity that is useful for logging.
func identityFromEnv() (auth.RunnerIdentity, error) {
	var err error

	as := os.Getenv("GRAM_FUNCTION_AUTH_SECRET")
	if as == "" {
		err = errors.Join(err, fmt.Errorf("GRAM_FUNCTION_AUTH_SECRET is required"))
	}

	authSecret, decodeErr := base64.StdEncoding.DecodeString(as)
	if decodeErr != nil {
		err = errors.Join(err, fmt.Errorf("decode base64 secret: %w", decodeErr))
	}

	projectID := os.Getenv("GRAM_PROJECT_ID")
	if projectID == "" {
		err = errors.Join(err, fmt.Errorf("GRAM_PROJECT_ID is required"))
	}

	deploymentID := os.Getenv("GRAM_DEPLOYMENT_ID")
	if deploymentID == "" {
		err = errors.Join(err, fmt.Errorf("GRAM_DEPLOYMENT_ID is required"))
	}

	functionID := os.Getenv("GRAM_FUNCTION_ID")
	if functionID == "" {
		err = errors.Join(err, fmt.Errorf("GRAM_FUNCTION_ID is required"))
	}

	version := buildinfo.Version

	return auth.RunnerIdentity{
		AuthSecret:   svc.NewSecret(authSecret),
		ProjectID:    projectID,
		DeploymentID: deploymentID,
		FunctionID:   functionID,
		Version:      version,
	}, err
}

func enrichLogger(logger *slog.Logger, ident auth.RunnerIdentity) *slog.Logger {
	attrs := make([]any, 0, 5)
	attrs = append(attrs, attr.SlogServiceName("gram-function-runner"))
	attrs = append(attrs, attr.SlogServiceVersion(ident.Version))
	attrs = append(attrs, attr.SlogProjectID(ident.ProjectID))
	attrs = append(attrs, attr.SlogDeploymentID(ident.DeploymentID))
	attrs = append(attrs, attr.SlogFunctionID(ident.FunctionID))

	return logger.With(attrs...)
}

func newServerClient() (*funcclient.Client, error) {
	su := os.Getenv("GRAM_SERVER_URL")
	if su == "" {
		return nil, fmt.Errorf("GRAM_SERVER_URL is required")
	}

	serverURL, err := url.Parse(su)
	if err != nil {
		return nil, fmt.Errorf("parse GRAM_SERVER_URL: %w", err)
	}

	httpClient := client.NewClient(
		serverURL.Scheme,
		serverURL.Host,
		http.DefaultClient,
		goahttp.RequestEncoder,
		goahttp.ResponseDecoder,
		false, // Don't restore response body
	)

	return funcclient.NewClient(
		httpClient.GetSignedAssetURL(),
	), nil
}
