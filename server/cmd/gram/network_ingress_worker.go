package gram

import (
	"context"
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
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	networkIngressWorkerShutdownTimeout = 30 * time.Second
	networkIngressWorkerStartupTimeout  = 30 * time.Second
)

func validateNetworkIngressWorkerTemporalTLS(environment, cert, key string) error {
	if (cert == "") != (key == "") {
		return errors.New("private ingress Temporal client certificate and key must be configured together")
	}
	if environment != "local" && cert == "" {
		return errors.New("private ingress Temporal mTLS is required outside local development")
	}
	return nil
}

func checkNetworkIngressWorkerKubernetes(ctx context.Context, clientset kubernetes.Interface) error {
	if _, err := clientset.Discovery().ServerVersion(); err != nil {
		return fmt.Errorf("check Kubernetes API: %w", err)
	}
	review, err := clientset.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{ResourceAttributes: &authorizationv1.ResourceAttributes{Verb: "list", Resource: "namespaces"}},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("check Kubernetes RBAC: %w", err)
	}
	if !review.Status.Allowed {
		return errors.New("check Kubernetes RBAC: namespace inventory is not allowed")
	}
	return nil
}

func validateNetworkIngressWorkerQueue(queue, sharedQueue string) error {
	if queue == "" {
		return errors.New("private ingress reconciliation task queue is required")
	}
	if queue == sharedQueue {
		return errors.New("private ingress reconciliation task queue must differ from the shared worker queue")
	}
	return nil
}

func newNetworkIngressWorkerCommand() *cli.Command {
	flags := append(workerRuntimeFlags(),
		&cli.StringFlag{
			Name:    "shared-worker-task-queue",
			Usage:   "Shared worker queue reserved for ordinary application work",
			EnvVars: []string{"TEMPORAL_TASK_QUEUE"},
			Value:   "main",
			Hidden:  true,
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8081",
			Usage:   "Pod-local health address",
			EnvVars: []string{"GRAM_NETWORK_INGRESS_WORKER_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "encryption-key",
			Usage:   "Application AES encryption key; required when provider mutations are enabled",
			EnvVars: []string{"GRAM_ENCRYPTION_KEY"},
		},
		&cli.PathFlag{
			Name:    "config-file",
			Usage:   "Path to a JSON, TOML, or YAML config file",
			EnvVars: []string{"GRAM_CONFIG_FILE"},
		},
	)
	flags = append(flags, networkIngressQueueFlags()...)
	flags = append(flags, networkIngressProviderFlags()...)

	return &cli.Command{
		Name:  "network-ingress-worker",
		Usage: "Start the dedicated private network ingress reconciler",
		Flags: flags,
		Before: func(c *cli.Context) error {
			return loadConfigFromFile(c, flags)
		},
		Action: func(c *cli.Context) error {
			queue := c.String(networkIngressQueueFlag)
			if err := validateNetworkIngressWorkerQueue(queue, c.String("shared-worker-task-queue")); err != nil {
				return err
			}
			if err := validateNetworkIngressWorkerTemporalTLS(c.String("environment"), c.String("temporal-client-cert"), c.String("temporal-client-key")); err != nil {
				return err
			}
			if c.String("network-ingress-operator-namespace") == "" {
				return errors.New("private ingress operator namespace is required for observation and cleanup")
			}

			serviceName := "gram-network-ingress-worker"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "network-ingress-worker"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("network_ingress_worker"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)
			slog.SetDefault(logger)

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()
			shutdownOTel, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName: serviceName, ServiceVersion: shortGitSHA(), GitSHA: GitSHA,
				EnableTracing: c.Bool("with-otel-tracing"), EnableMetrics: c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("setup opentelemetry sdk: %w", err)
			}
			defer func() {
				shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), networkIngressWorkerShutdownTimeout)
				defer shutdownCancel()
				_ = shutdownOTel(shutdownCtx)
			}()

			meterProvider := otel.GetMeterProvider()
			temporalEnv, shutdownTemporal, err := newTemporalClient(logger, meterProvider, temporalClientOptions{
				address: c.String("temporal-address"), namespace: c.String("temporal-namespace"), taskQueue: queue,
				certPEMBlock: []byte(c.String("temporal-client-cert")), keyPEMBlock: []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return err
			}
			if temporalEnv == nil {
				return errors.New("insufficient options to create Temporal client")
			}
			defer func() { _ = shutdownTemporal(context.WithoutCancel(ctx)) }()

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{enableUnsafeLogging: c.Bool("unsafe-db-log")})
			if err != nil {
				return err
			}
			defer db.Close()

			config, err := networkIngressConfigFromCLI(c)
			if err != nil {
				return err
			}
			if config.ProviderMutationsEnabled && !config.MutationReady() {
				return errors.New("private ingress provider mutation configuration is incomplete")
			}
			var encryptionClient *encryption.Client
			if key := c.String("encryption-key"); key != "" {
				encryptionClient, err = encryption.New(key)
				if err != nil {
					return fmt.Errorf("create encryption client: %w", err)
				}
			}
			if config.ProviderMutationsEnabled && encryptionClient == nil {
				return errors.New("private ingress encryption key is required when provider mutations are enabled")
			}
			k8sClient, err := k8s.InitializeK8sClient(ctx, logger, serviceEnv, "", "")
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %w", err)
			}
			if k8sClient.Clientset == nil || k8sClient.DynamicClient == nil {
				return errors.New("private ingress reconciler requires in-cluster Kubernetes clients")
			}
			executor, err := newNetworkIngressExecutor(logger, meterProvider, db, encryptionClient, k8sClient, config)
			if err != nil {
				return err
			}
			worker, err := background.NewNetworkIngressWorker(temporalEnv, db, executor)
			if err != nil {
				return fmt.Errorf("configure private ingress worker: %w", err)
			}

			healthListener, err := net.Listen("tcp", c.String("control-address"))
			if err != nil {
				return fmt.Errorf("listen for private ingress worker health checks: %w", err)
			}
			defer func() { _ = healthListener.Close() }()
			healthHandler := o11y.NewHealthCheckHandler(nil,
				[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}}, nil,
				[]*o11y.NamedResource[client.Client]{{Name: "network-ingress", Resource: temporalEnv.Client()}},
			)
			healthMux := http.NewServeMux()
			healthMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
				if err := checkNetworkIngressWorkerKubernetes(r.Context(), k8sClient.Clientset); err != nil {
					http.Error(w, "kubernetes dependency unavailable", http.StatusServiceUnavailable)
					return
				}
				healthHandler.ServeHTTP(w, r)
			})
			healthMux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			healthServer := &http.Server{
				Handler: healthMux, ReadHeaderTimeout: 10 * time.Second,
				BaseContext: func(net.Listener) context.Context { return ctx },
			}

			startupCtx, startupCancel := context.WithTimeout(ctx, networkIngressWorkerStartupTimeout)
			defer startupCancel()
			if err := worker.EnsureSchedule(startupCtx); err != nil {
				return fmt.Errorf("register network ingress sweep: %w", err)
			}
			if err := worker.Start(); err != nil {
				return fmt.Errorf("start private ingress worker: %w", err)
			}
			logger.InfoContext(ctx, "private network ingress worker started", attr.SlogServerAddress(healthListener.Addr().String()))

			sigctx, stop := signal.NotifyContext(c.Context, os.Interrupt, syscall.SIGTERM)
			defer stop()
			serveErrors := make(chan error, 1)
			go func() { serveErrors <- healthServer.Serve(healthListener) }()
			var serveErr error
			select {
			case <-sigctx.Done():
			case err := <-serveErrors:
				if !errors.Is(err, http.ErrServerClosed) {
					serveErr = fmt.Errorf("serve private ingress worker health checks: %w", err)
				}
			}

			shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), networkIngressWorkerShutdownTimeout)
			defer shutdownCancel()
			healthErr := healthServer.Shutdown(shutdownCtx)
			worker.Stop()
			return errors.Join(serveErr, healthErr)
		},
	}
}
