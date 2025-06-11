package gram

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/pgx-contrib/pgxotel"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/internal/chat"
	"github.com/speakeasy-api/gram/internal/k8s"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	slack_client "github.com/speakeasy-api/gram/internal/thirdparty/slack/client"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/worker"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/background"
	"github.com/speakeasy-api/gram/internal/background/interceptors"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/o11y"
)

type dbClientOptions struct {
	enableUnsafeLogging bool
}

func newDBClient(ctx context.Context, logger *slog.Logger, connstring string, opts dbClientOptions) (*pgxpool.Pool, error) {
	poolcfg := must.Value(pgxpool.ParseConfig(connstring))
	consoleLogLevel := tracelog.LogLevelNone
	if opts.enableUnsafeLogging {
		consoleLogLevel = tracelog.LogLevelDebug
	}

	poolcfg.ConnConfig.Tracer = multitracer.New(
		&pgxotel.QueryTracer{
			Name:    "pgx",
			Options: []trace.TracerOption{},
		},
		o11y.NewPGXLogger(logger, consoleLogLevel),
	)

	return pgxpool.NewWithConfig(ctx, poolcfg)
}

type assetStorageOptions struct {
	assetsBackend string
	assetsURI     string
}

func newAssetStorage(ctx context.Context, opts assetStorageOptions) (assets.BlobStore, func(context.Context) error, error) {
	shutdown := func(ctx context.Context) error { return nil }
	switch opts.assetsBackend {
	case "fs":
		assetsURI := filepath.Clean(opts.assetsURI)
		if err := os.MkdirAll(assetsURI, 0750); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, shutdown, err
		}

		root, err := os.OpenRoot(assetsURI)
		if err != nil {
			return nil, shutdown, err
		}

		shutdown = func(context.Context) error {
			return root.Close()
		}

		return &assets.FSBlobStore{Root: root}, shutdown, nil
	case "gcs":
		gcsStore, err := assets.NewGCSBlobStore(ctx, opts.assetsURI)
		if err != nil {
			return nil, nil, err
		}

		return gcsStore, shutdown, nil
	default:
		return nil, shutdown, fmt.Errorf("invalid assets backend: %s", opts.assetsBackend)
	}
}

type redisClientOptions struct {
	redisAddr     string
	redisPassword string
}

func newRedisClient(ctx context.Context, opts redisClientOptions) (*redis.Client, error) {
	db := 0
	redisClient := redis.NewClient(&redis.Options{
		Addr:         opts.redisAddr,
		Password:     opts.redisPassword,
		DB:           db,
		DialTimeout:  1 * time.Second,
		ReadTimeout:  300 * time.Millisecond,
		WriteTimeout: 1 * time.Second,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	attrs := redisotel.WithAttributes(
		semconv.DBSystemRedis,
		semconv.DBRedisDBIndex(db),
	)
	if err := redisotel.InstrumentTracing(redisClient, redisotel.WithDBStatement(false), attrs); err != nil {
		return nil, fmt.Errorf("failed to instrument redis client: %w", err)
	}

	return redisClient, nil
}

type temporalClientOptions struct {
	address      string
	namespace    string
	certPEMBlock []byte
	keyPEMBlock  []byte
}

func newTemporalClient(logger *slog.Logger, opts temporalClientOptions) (client.Client, func(context.Context) error, error) {
	var nilShutdownFunc = func(context.Context) error { return nil }
	if opts.address == "" || opts.namespace == "" {
		return nil, nilShutdownFunc, nil
	}

	var connOpts client.ConnectionOptions
	if len(opts.certPEMBlock) > 0 && len(opts.keyPEMBlock) > 0 {
		cert, err := tls.X509KeyPair(opts.certPEMBlock, opts.keyPEMBlock)
		if err != nil {
			return nil, nilShutdownFunc, fmt.Errorf("failed to create temporal client: %w", err)
		}

		connOpts.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	clientOptions := client.Options{
		HostPort:          opts.address,
		Namespace:         opts.namespace,
		ConnectionOptions: connOpts,
		Logger:            logger.With(slog.String("component", "temporal")),
	}

	interceptors := []interceptor.ClientInterceptor{}

	tracingInterceptor, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{
		TextMapPropagator: otel.GetTextMapPropagator(),
	})
	if err != nil {
		return nil, nilShutdownFunc, fmt.Errorf("failed to create temporal tracing interceptor: %w", err)
	}

	interceptors = append(interceptors, tracingInterceptor)
	clientOptions.MetricsHandler = opentelemetry.NewMetricsHandler(opentelemetry.MetricsHandlerOptions{})

	clientOptions.Interceptors = interceptors

	temporalClient, err := client.Dial(clientOptions)
	if err != nil {
		return nil, nilShutdownFunc, fmt.Errorf("failed to create temporal client: %w", err)
	}

	return temporalClient, func(context.Context) error {
		temporalClient.Close()
		return nil
	}, nil
}

func newTemporalWorker(client client.Client, logger *slog.Logger, db *pgxpool.Pool, assetStorage assets.BlobStore, slackClient *slack_client.SlackClient, chatClient *chat.ChatClient, openRouter openrouter.Provisioner, k8sClient *k8s.KubernetesClients, expectedTargetCNAME string) worker.Worker {
	temporalWorker := worker.New(client, string(background.TaskQueueMain), worker.Options{
		Interceptors: []interceptor.WorkerInterceptor{
			&interceptors.Recovery{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
			&interceptors.InjectExecutionInfo{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
			&interceptors.Logging{WorkerInterceptorBase: interceptor.WorkerInterceptorBase{}},
		},
	})

	activities := background.NewActivities(logger, db, assetStorage, slackClient, chatClient, openRouter, k8sClient, expectedTargetCNAME)
	temporalWorker.RegisterActivity(activities.ProcessDeployment)
	temporalWorker.RegisterActivity(activities.TransitionDeployment)
	temporalWorker.RegisterActivity(activities.GetSlackProjectContext)
	temporalWorker.RegisterActivity(activities.PostSlackMessage)
	temporalWorker.RegisterActivity(activities.SlackChatCompletion)
	temporalWorker.RegisterActivity(activities.RefreshOpenRouterKey)
	temporalWorker.RegisterActivity(activities.VerifyCustomDomain)
	temporalWorker.RegisterActivity(activities.CustomDomainIngress)

	temporalWorker.RegisterWorkflow(background.ProcessDeploymentWorkflow)
	temporalWorker.RegisterWorkflow(background.SlackEventWorkflow)
	temporalWorker.RegisterWorkflow(background.OpenrouterKeyRefreshWorkflow)
	temporalWorker.RegisterWorkflow(background.CustomDomainRegistrationWorkflow)

	return temporalWorker
}
