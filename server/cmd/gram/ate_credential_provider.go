package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
	credprovider "go.ate.dev/substrate/api/credprovider/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/speakeasy-api/gram/server/internal/atecred"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// ateCredProviderDrainGrace bounds graceful gRPC stop before hard stop on SIGTERM.
	ateCredProviderDrainGrace = 5 * time.Second

	// ateCredProviderHealthReadHeaderTimeout bounds HTTP header reads on the health listener.
	ateCredProviderHealthReadHeaderTimeout = 10 * time.Second

	// ateCredProviderShutdownTimeout bounds OTel and health server shutdown.
	ateCredProviderShutdownTimeout = 15 * time.Second
)

func newATECredentialProviderCommand() *cli.Command {
	flags := ateCredentialProviderFlags()
	return &cli.Command{
		Name:  "ate-credential-provider",
		Usage: "Serve the ATE egress CredentialProvider gRPC API backed by Speakeasy turn credentials",
		Flags: flags,
		Before: func(c *cli.Context) error {
			return loadConfigFromFile(c, flags)
		},
		Action: runATECredentialProvider,
	}
}

func ateCredentialProviderFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "environment",
			Value:   "prod",
			Usage:   "Deployment environment: local, dev, prod",
			EnvVars: []string{"GRAM_ENVIRONMENT"},
		},
		&cli.StringFlag{
			Name:    "listen-address",
			Value:   atecred.DefaultListenAddress,
			Usage:   "gRPC mTLS listen address for ATE CredentialProvider.FetchSecret",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_LISTEN_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "health-address",
			Value:   atecred.DefaultHealthAddress,
			Usage:   "HTTP /healthz, /livez, and /readyz listen address",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_HEALTH_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "server-cred-bundle",
			Usage:   "PEM credential bundle (key + cert chain) presented for serving mTLS",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_SERVER_CRED_BUNDLE"},
		},
		&cli.StringFlag{
			Name:    "client-ca-file",
			Usage:   "PEM CA bundle that ATE egress gateway client certificates must chain to",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_CLIENT_CA_FILE"},
		},
		&cli.StringFlag{
			Name:    "injector-identity",
			Value:   atecred.DefaultInjectorIdentity,
			Usage:   "SPIFFE URI SAN of the ATE egress gateway permitted to fetch credentials",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_INJECTOR_IDENTITY"},
		},
		&cli.StringFlag{
			Name:    "atespace",
			Usage:   "Single ATE atespace whose actors this credential provider serves",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_ATESPACE"},
		},
		&cli.StringFlag{
			Name:    "speakeasy-server-url",
			Usage:   "HTTPS base URL of the Speakeasy API server hosting the internal mint endpoint",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_SERVER_URL"},
		},
		&cli.DurationFlag{
			Name:    "cache-ttl",
			Value:   atecred.DefaultCacheTTL,
			Usage:   "How long minted actor credentials are cached in memory",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_CACHE_TTL"},
		},
		&cli.DurationFlag{
			Name:    "request-timeout",
			Value:   atecred.DefaultRequestTimeout,
			Usage:   "Per-request timeout when calling the Speakeasy mint endpoint",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_REQUEST_TIMEOUT"},
		},
		&cli.StringFlag{
			Name:    "local-auth-token",
			Usage:   "Local development only: static bearer token for the mint endpoint (rejected outside local)",
			EnvVars: []string{"GRAM_ATE_CRED_PROVIDER_LOCAL_AUTH_TOKEN"},
		},
		&cli.BoolFlag{
			Name:    "with-otel-tracing",
			Usage:   "Enable OpenTelemetry traces",
			EnvVars: []string{"GRAM_ENABLE_OTEL_TRACES"},
		},
		&cli.BoolFlag{
			Name:    "with-otel-metrics",
			Usage:   "Enable OpenTelemetry metrics",
			EnvVars: []string{"GRAM_ENABLE_OTEL_METRICS"},
		},
		&cli.PathFlag{
			Name:    "config-file",
			Usage:   "Path to a JSON, TOML, or YAML config file",
			EnvVars: []string{"GRAM_CONFIG_FILE"},
		},
	}
}

func validateATECredentialProviderCLI(c *cli.Context) (*url.URL, string) {
	env := strings.TrimSpace(c.String("environment"))
	rawServerURL := strings.TrimSpace(c.String("speakeasy-server-url"))
	parsedURL, parseErr := url.Parse(rawServerURL)
	endpointURL, endpointErr := atecred.MintEndpointURL(parsedURL)
	schemeValid := parseErr == nil && parsedURL != nil && (parsedURL.Scheme == "https" || (env == "local" && parsedURL.Scheme == "http"))
	localToken := strings.TrimSpace(c.String("local-auth-token"))

	inv.Require("ATE credential provider configuration",
		"listen-address is set", strings.TrimSpace(c.String("listen-address")) != "",
		"health-address is set", strings.TrimSpace(c.String("health-address")) != "",
		"server-cred-bundle is set", strings.TrimSpace(c.String("server-cred-bundle")) != "",
		"client-ca-file is set", strings.TrimSpace(c.String("client-ca-file")) != "",
		"injector-identity is set", strings.TrimSpace(c.String("injector-identity")) != "",
		"atespace is valid", atecred.ValidATEName(strings.TrimSpace(c.String("atespace"))),
		"speakeasy-server-url is valid", schemeValid && endpointErr == nil,
		"cache-ttl is positive", c.Duration("cache-ttl") > 0,
		"request-timeout is positive", c.Duration("request-timeout") > 0,
		"local-auth-token is only set in local environment", localToken == "" || env == "local",
	)
	return parsedURL, endpointURL
}

func runATECredentialProvider(c *cli.Context) error {
	_, endpointURL := validateATECredentialProviderCLI(c)

	env := strings.TrimSpace(c.String("environment"))
	serviceName := "gram-ate-credential-provider"
	o11y.PullAppInfo(c.Context).Command = "ate-credential-provider"

	logger := PullLogger(c.Context).With(
		attr.SlogComponent("ate_credential_provider"),
		attr.SlogServiceName(serviceName),
		attr.SlogServiceVersion(shortGitSHA()),
		attr.SlogServiceEnv(env),
	)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(c.Context)
	defer cancel()

	shutdownOTel, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
		ServiceName: serviceName, ServiceVersion: shortGitSHA(), GitSHA: GitSHA, Partition: PullPartition(ctx),
		EnableTracing: c.Bool("with-otel-tracing"), EnableMetrics: c.Bool("with-otel-metrics"),
	})
	if err != nil {
		return fmt.Errorf("setup ATE credential provider telemetry: %w", err)
	}
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.WithoutCancel(ctx), ateCredProviderShutdownTimeout)
		defer shutCancel()
		_ = shutdownOTel(shutCtx)
	}()

	tokenSource, err := atecred.NewClientTokenSource(ctx, env, endpointURL, c.String("local-auth-token"))
	if err != nil {
		return fmt.Errorf("configure ATE credential provider auth token source: %w", err)
	}

	tlsCreds, err := atecred.NewServerTLSCredentials(
		strings.TrimSpace(c.String("server-cred-bundle")),
		strings.TrimSpace(c.String("client-ca-file")),
		strings.TrimSpace(c.String("injector-identity")),
	)
	if err != nil {
		return fmt.Errorf("configure ATE credential provider mTLS: %w", err)
	}

	policy := guardian.NewDefaultPolicy(otel.GetTracerProvider())
	var httpOpts []guardian.ClientOption
	if env == "local" {
		httpOpts = append(httpOpts, guardian.WithAllowedCIDRBlocks("127.0.0.0/8", "::1/128"))
	}
	mintClient := atecred.NewHTTPMintClient(atecred.HTTPMintClientConfig{
		EndpointURL:    endpointURL,
		TokenSource:    tokenSource,
		HTTPClient:     policy.PooledClient(httpOpts...),
		RequestTimeout: c.Duration("request-timeout"),
	})
	provider := atecred.NewServer(atecred.ServerConfig{
		Logger:   logger,
		Atespace: strings.TrimSpace(c.String("atespace")),
		CacheTTL: c.Duration("cache-ttl"),
		Client:   mintClient,
		Now:      nil,
	})

	grpcSrv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.Creds(tlsCreds),
	)
	reflection.Register(grpcSrv)
	credprovider.RegisterCredentialProviderServer(grpcSrv, provider)

	lis, err := (&net.ListenConfig{Control: nil, KeepAlive: 0, KeepAliveConfig: net.KeepAliveConfig{Enable: false, Idle: 0, Interval: 0, Count: 0}}).Listen(ctx, "tcp", strings.TrimSpace(c.String("listen-address")))
	if err != nil {
		return fmt.Errorf("listen for ATE credential provider gRPC: %w", err)
	}

	var ready atomic.Bool
	ready.Store(true)

	healthLis, err := (&net.ListenConfig{Control: nil, KeepAlive: 0, KeepAliveConfig: net.KeepAliveConfig{Enable: false, Idle: 0, Interval: 0, Count: 0}}).Listen(ctx, "tcp", strings.TrimSpace(c.String("health-address")))
	if err != nil {
		_ = lis.Close()
		return fmt.Errorf("listen for ATE credential provider health: %w", err)
	}
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	healthMux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	healthMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	healthSrv := &http.Server{
		Handler:           healthMux,
		ReadHeaderTimeout: ateCredProviderHealthReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		if serveErr := healthSrv.Serve(healthLis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "ATE credential provider health server exited", attr.SlogError(serveErr))
		}
	}()
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.WithoutCancel(ctx), ateCredProviderShutdownTimeout)
		defer shutCancel()
		_ = healthSrv.Shutdown(shutCtx)
	}()

	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-sigCtx.Done()
		logger.InfoContext(ctx, "shutting down ATE credential provider")
		ready.Store(false)
		done := make(chan struct{})
		go func() {
			grpcSrv.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(ateCredProviderDrainGrace):
			logger.WarnContext(ctx, "ATE credential provider graceful shutdown timed out; forcing stop")
			grpcSrv.Stop()
		}
	}()

	logger.InfoContext(ctx, "ATE credential provider listening", attr.SlogServerAddress(lis.Addr().String()))
	if err := grpcSrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serve ATE credential provider gRPC: %w", err)
	}
	return nil
}
