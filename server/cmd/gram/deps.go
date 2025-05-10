package gram

import (
	"context"
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
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/internal/assets"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/o11y"
)

type dbClientOptions struct {
	enableTracing       bool
	enableUnsafeLogging bool
}

func newDBClient(ctx context.Context, logger *slog.Logger, connstring string, opts dbClientOptions) (*pgxpool.Pool, error) {
	poolcfg := must.Value(pgxpool.ParseConfig(connstring))
	if opts.enableTracing {
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
	}

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
	enableTracing bool
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

	if opts.enableTracing {
		attrs := redisotel.WithAttributes(
			semconv.DBSystemRedis,
			semconv.DBRedisDBIndex(db),
		)
		if err := redisotel.InstrumentTracing(redisClient, redisotel.WithDBStatement(false), attrs); err != nil {
			return nil, fmt.Errorf("failed to instrument redis client: %w", err)
		}
	}

	return redisClient, nil
}
