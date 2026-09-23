package gram

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
)

type mcpServerRuntime struct {
	MCP      *mcp.Service
	XMCP     *xmcp.Service
	Metadata *mcpmetadata.Service
}

type mcpServerRuntimeDependencies struct {
	Logger     *slog.Logger
	DB         *pgxpool.Pool
	Encryption *encryption.Client
	MCP        *mcp.Service
	Metadata   *mcpmetadata.Service
}

func buildMCPServerRuntime(deps mcpServerRuntimeDependencies) (*mcpServerRuntime, error) {
	if deps.Logger == nil || deps.DB == nil || deps.Encryption == nil || deps.MCP == nil || deps.Metadata == nil {
		return nil, errors.New("MCP server runtime dependencies are incomplete")
	}

	return &mcpServerRuntime{
		MCP:      deps.MCP,
		XMCP:     xmcp.NewService(deps.Logger, deps.DB, deps.Encryption, deps.MCP),
		Metadata: deps.Metadata,
	}, nil
}

func newNetworkIngressServerCommand() *cli.Command {
	flags := privateIngressServerFlags()
	return &cli.Command{
		Name:  "network-ingress-server",
		Usage: "Start the dedicated private network ingress server",
		Flags: flags,
		Action: func(c *cli.Context) error {
			if err := validatePrivateServerConfig(
				c.Bool("network-ingress-enabled"),
				c.String("netingress-address"),
				c.String("netingress-tls-cert-file"),
				c.String("netingress-tls-key-file"),
				c.Bool("dev-single-process"),
			); err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("network_ingress_server"),
				attr.SlogServiceName("gram-network-ingress-server"),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(c.String("environment")),
			)
			o11y.PullAppInfo(ctx).Command = "network-ingress-server"
			slog.SetDefault(logger)
			shutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName: "gram-network-ingress-server", ServiceVersion: shortGitSHA(), GitSHA: GitSHA,
				EnableTracing: c.Bool("with-otel-tracing"), EnableMetrics: c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("setup private ingress telemetry: %w", err)
			}
			defer func() { _ = runShutdown(logger, ctx, []func(context.Context) error{shutdown}) }()
			deps, err := newPrivateIngressRuntime(ctx, c, logger)
			if err != nil {
				return err
			}
			defer func() {
				cancel()
				deps.Close(ctx)
			}()
			controlServer := control.Server{Address: c.String("control-address"), Logger: logger, DisableProfiling: true}
			temporalHealth := []*o11y.NamedResource[client.Client]{}
			if deps.Temporal != nil {
				temporalHealth = append(temporalHealth, &o11y.NamedResource[client.Client]{Name: "default", Resource: deps.Temporal.Client()})
			}
			healthHandler := o11y.NewHealthCheckHandler(nil,
				[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: deps.DB}},
				[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: deps.Redis}}, temporalHealth)
			stopControl, err := controlServer.Start(ctx, privateIngressReadinessHandler(
				deps.Kubernetes.Clientset.AuthenticationV1().TokenReviews(),
				"/var/run/secrets/kubernetes.io/serviceaccount/token", healthHandler))
			if err != nil {
				return fmt.Errorf("start private ingress control server: %w", err)
			}
			defer func() { _ = runShutdown(logger, ctx, []func(context.Context) error{stopControl}) }()
			return servePrivateIngress(ctx, privateIngressServerDependencies{
				Logger: logger, MeterProvider: otel.GetMeterProvider(), DB: deps.DB,
				Kubernetes: deps.Kubernetes, Runtime: deps.Runtime,
			}, c.String("netingress-address"), c.String("netingress-tls-cert-file"), c.String("netingress-tls-key-file"))
		},
		Before: func(ctx *cli.Context) error {
			return loadConfigFromFile(ctx, flags)
		},
	}
}

func privateIngressServerFlags() []cli.Flag {
	flags := mcpRuntimeFlags()
	return append(flags,
		&cli.StringFlag{
			Name:    "netingress-address",
			Usage:   "Private network ingress HTTPS address; empty disables the listener",
			EnvVars: []string{"GRAM_NETINGRESS_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "netingress-tls-cert-file",
			Usage:   "TLS certificate file for the private network ingress listener",
			EnvVars: []string{"GRAM_NETINGRESS_TLS_CERT_FILE"},
		},
		&cli.StringFlag{
			Name:    "netingress-tls-key-file",
			Usage:   "TLS private key file for the private network ingress listener",
			EnvVars: []string{"GRAM_NETINGRESS_TLS_KEY_FILE"},
		},
	)
}

func validatePrivateServerConfig(enabled bool, address, certFile, keyFile string, devSingleProcess bool) error {
	if !enabled {
		return errors.New("private network ingress runtime is disabled")
	}
	if address == "" {
		return errors.New("private network ingress address is required")
	}
	if certFile == "" || keyFile == "" {
		return errors.New("private network ingress TLS certificate and key are required")
	}
	if devSingleProcess {
		return errors.New("private network ingress server cannot run the general worker")
	}
	return nil
}

type privateIngressServerDependencies struct {
	Logger        *slog.Logger
	MeterProvider metric.MeterProvider
	DB            *pgxpool.Pool
	Kubernetes    *k8s.KubernetesClients
	Runtime       *mcpServerRuntime
}

func servePrivateIngress(
	ctx context.Context,
	deps privateIngressServerDependencies,
	address string,
	certFile string,
	keyFile string,
) error {
	if deps.Kubernetes == nil || deps.Kubernetes.Clientset == nil {
		return errors.New("private network ingress listener requires an in-cluster Kubernetes client")
	}
	if deps.Runtime == nil {
		return errors.New("private network ingress listener requires an MCP server runtime")
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("load private network ingress TLS certificate: %w", err)
	}

	telemetry := netingress.NewTelemetry(deps.Logger, deps.MeterProvider)
	mux := goahttp.NewMuxer()
	mux.Use(middleware.NetworkServingPolicyVersion)
	mux.Use(mcpauthz.StripMiddleware)
	mux.Use(netingress.RouteGuard)
	mux.Use(middleware.DropInboundOTelBaggage)
	mux.Use(func(h http.Handler) http.Handler {
		return otelhttp.NewHandler(h, "http",
			otelhttp.WithServerName("gram-netingress"),
			otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		)
	})
	mux.Use(middleware.RouteLabelerMiddleware)
	mux.Use(middleware.MCPProtocolVersionTelemetry)
	mux.Use(middleware.NewHTTPLoggingMiddleware(deps.Logger))
	mux.Use(middleware.NewRecovery(deps.Logger))
	mux.Use(netingress.Middleware(
		netingress.NewAttestationVerifier(
			deps.Kubernetes.Clientset.AuthenticationV1().TokenReviews(),
			netingress.NewIngressLookup(deps.DB),
			netingress.DefaultTokenAudience,
			30*time.Second,
			telemetry,
		),
		netingress.IdentityParsers{netingress.ProviderTailscale: netingress.TailscaleIdentityParser{}},
		telemetry,
	))
	mux.Use(middleware.SessionMiddleware)
	mcp.AttachPrivate(mux, deps.Runtime.MCP, deps.Runtime.Metadata)
	xmcp.AttachPrivate(mux, deps.Runtime.XMCP, deps.Runtime.Metadata)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen for private network ingress: %w", err)
	}
	defer func() { _ = listener.Close() }()

	requestCtx, cancelRequests := context.WithCancel(ctx)
	defer cancelRequests()
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       620 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		},
		BaseContext: func(net.Listener) context.Context { return requestCtx },
	}

	sigctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-sigctx.Done()
		shutdownPrivateIngress(ctx, deps.Logger, server, cancelRequests, shutdownDrainTimeout)
	}()

	deps.Logger.InfoContext(ctx, "private network ingress listener started", attr.SlogServerAddress(listener.Addr().String()))
	serveErr := server.ServeTLS(listener, "", "")
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		cancel()
		<-shutdownDone
		return fmt.Errorf("serve private network ingress: %w", serveErr)
	}
	<-shutdownDone
	return nil
}

func shutdownPrivateIngress(ctx context.Context, logger *slog.Logger, server *http.Server, cancelRequests context.CancelFunc, timeout time.Duration) {
	graceCtx, graceCancel := context.WithTimeoutCause(context.WithoutCancel(ctx), timeout, errors.New("graceful shutdown timed out"))
	defer graceCancel()
	defer cancelRequests()
	if err := server.Shutdown(graceCtx); err != nil {
		logger.ErrorContext(ctx, "failed to shutdown private network ingress listener", attr.SlogError(err))
		cancelRequests()
		if err := server.Close(); err != nil {
			logger.ErrorContext(ctx, "close private network ingress connections", attr.SlogError(err))
		}
	}
}
