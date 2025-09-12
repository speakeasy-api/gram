package gram

import (
	"context"
	"crypto/tls"
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/multitracer"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"github.com/pgx-contrib/pgxotel"
	polargo "github.com/polarsource/polar-go"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"

	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
)

func loadConfigFromFile(c *cli.Context, flags []cli.Flag) error {
	var cfgLoader cli.BeforeFunc = func(ctx *cli.Context) error { return nil }
	switch filepath.Ext(c.Path("config-file")) {
	case ".yaml", ".yml":
		cfgLoader = altsrc.InitInputSourceWithContext(flags, altsrc.NewYamlSourceFromFlagFunc("config-file"))
	case ".json":
		cfgLoader = altsrc.InitInputSourceWithContext(flags, altsrc.NewJSONSourceFromFlagFunc("config-file"))
	case ".toml":
		cfgLoader = altsrc.InitInputSourceWithContext(flags, altsrc.NewTomlSourceFromFlagFunc("config-file"))
	}
	return cfgLoader(c)
}

type dbClientOptions struct {
	enableUnsafeLogging bool
}

func newDBClient(ctx context.Context, logger *slog.Logger, meterProvider metric.MeterProvider, connstring string, opts dbClientOptions) (*pgxpool.Pool, error) {
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

	pool, err := pgxpool.NewWithConfig(ctx, poolcfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgx pool: %w", err)
	}

	if err := otelpgx.RecordStats(pool, otelpgx.WithStatsMeterProvider(meterProvider)); err != nil {
		return nil, fmt.Errorf("unable to record pgx metrics: %w", err)
	}

	return pool, nil
}

type assetStorageOptions struct {
	assetsBackend string
	assetsURI     string
}

func newAssetStorage(ctx context.Context, logger *slog.Logger, opts assetStorageOptions) (assets.BlobStore, func(context.Context) error, error) {
	shutdown := func(ctx context.Context) error { return nil }
	switch opts.assetsBackend {
	case "fs":
		assetsURI := filepath.Clean(opts.assetsURI)
		if err := os.MkdirAll(assetsURI, 0750); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, shutdown, fmt.Errorf("create assets directory: %w", err)
		}

		root, err := os.OpenRoot(assetsURI)
		if err != nil {
			return nil, shutdown, fmt.Errorf("open fs assets root: %w", err)
		}

		shutdown = func(context.Context) error {
			return root.Close()
		}

		return assets.NewFSBlobStore(logger, root), shutdown, nil
	case "gcs":
		gcsStore, err := assets.NewGCSBlobStore(ctx, logger, opts.assetsURI)
		if err != nil {
			return nil, nil, fmt.Errorf("create gcs blob store: %w", err)
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
		Logger:            logger.With(attr.SlogComponent("temporal")),
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

func newLocalFeatureFlags(ctx context.Context, logger *slog.Logger, csvPath string) *feature.InMemory {
	inmem := &feature.InMemory{}

	if csvPath == "" {
		logger.DebugContext(ctx, "newLocalFeatureFlags: no csv path provided, using empty in-memory feature flag provider")
		return inmem
	}

	wd, err := os.Getwd()
	if err != nil {
		logger.ErrorContext(ctx, "newLocalFeatureFlags: error reading current directory", attr.SlogError(err))
		return inmem
	}

	p := filepath.Clean(csvPath)
	if !strings.HasPrefix(p, wd) {
		logger.ErrorContext(ctx, "newLocalFeatureFlags: csv path is not within the current working directory", attr.SlogFilePath(csvPath))
		return inmem
	}

	file, err := os.Open(p)
	if err != nil {
		logger.ErrorContext(ctx, "newLocalFeatureFlags: error opening local feature flags csv file", attr.SlogError(err), attr.SlogFilePath(csvPath))
		return inmem
	}
	defer o11y.LogDefer(ctx, logger, func() error { return file.Close() })

	rdr := csv.NewReader(file)
	records, err := rdr.ReadAll()
	if err != nil {
		logger.ErrorContext(ctx, "newLocalFeatureFlags: failed to read local feature flags csv file", attr.SlogError(err))
		return inmem
	}

	for i, record := range records {
		rowid := fmt.Sprint(i + 1)

		if i == 0 {
			// Skip header row
			continue
		}

		if len(record) != 3 {
			logger.ErrorContext(ctx, "newLocalFeatureFlags: invalid record in local feature flags csv file at row "+rowid)
			continue
		}

		enabled, err := strconv.ParseBool(record[2])
		if err != nil {
			logger.ErrorContext(ctx, "newLocalFeatureFlags: invalid boolean value in local feature flags csv file at row "+rowid, attr.SlogError(err))
			continue
		}

		inmem.SetFlag(feature.Flag(record[1]), record[0], enabled)
	}

	return inmem
}

func newBillingProvider(
	ctx context.Context,
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	redisClient *redis.Client,
	c *cli.Context,
) (billing.Repository, billing.Tracker, error) {
	switch {
	case c.String("polar-api-key") != "":
		catalog := &polar.Catalog{
			ProductIDFree:    c.String("polar-product-id-free"),
			ProductIDPro:     c.String("polar-product-id-pro"),
			MeterIDToolCalls: c.String("polar-meter-id-tool-calls"),
			MeterIDServers:   c.String("polar-meter-id-servers"),
		}
		if err := catalog.Validate(); err != nil {
			return nil, nil, fmt.Errorf("invalid polar catalog configuration: %w", err)
		}
		polarAPIKey := c.String("polar-api-key")
		polarsdk := polargo.New(polargo.WithSecurity(polarAPIKey), polargo.WithTimeout(30*time.Second)) // Shouldn't take this long, but just in case
		pclient := polar.NewClient(polarsdk, polarAPIKey, logger, tracerProvider, redisClient, catalog, c.String("polar-webhook-secret"))
		return pclient, pclient, nil
	case c.String("environment") == "local":
		logger.WarnContext(ctx, "using stub billing client: polar not configured")
		stub := billing.NewStubClient(logger, tracerProvider)
		return stub, stub, nil
	default:
		return nil, nil, fmt.Errorf("billing provider is not configured")
	}
}
