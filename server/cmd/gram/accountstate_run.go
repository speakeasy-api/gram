package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/localaccounts"
	localrepo "github.com/speakeasy-api/gram/server/internal/localaccounts/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"go.opentelemetry.io/otel/trace/noop"
)

// Configuration comes only from this mise worktree, never a user/org flag.
// Values carrying credentials are consumed in memory and are never reported.
func localAccountConfig() (localaccounts.Config, error) {
	root, err := os.Getwd()
	if err != nil {
		return localaccounts.Config{}, fmt.Errorf("locate account worktree: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return localaccounts.Config{}, fmt.Errorf("resolve account worktree: %w", err)
	}
	port := func(name string) uint16 {
		v, e := strconv.ParseUint(os.Getenv(name), 10, 16)
		if e != nil {
			return 0
		}
		return uint16(v)
	}
	external := stripeclient.IsConfigured(os.Getenv("STRIPE_API_KEY"))
	for _, name := range []string{"POLAR_API_KEY", "TEMPORAL_CLIENT_CERT", "TEMPORAL_CLIENT_KEY"} {
		if os.Getenv(name) != "" {
			external = true
		}
	}
	return localaccounts.Config{
		Root: root, Environment: os.Getenv("GRAM_ENVIRONMENT"), IDPBackend: os.Getenv("GRAM_DEVIDP_BACKEND"),
		BillingProvider: "local", ExternalProvidersConfigured: external,
		PGService: os.Getenv("PGSERVICE"), DockerHost: os.Getenv("DOCKER_HOST"), DockerContext: os.Getenv("DOCKER_CONTEXT"),
		DatabaseURL: os.Getenv("GRAM_DATABASE_URL"), IDPDatabase: os.Getenv("GRAM_DEVIDP_DB"), IDPURL: os.Getenv("GRAM_IDP_BASE_URL"),
		ExpectedDatabasePort: port("DB_PORT"), ExpectedIDPPort: port("GRAM_DEVIDP_PORT"),
		RedisAddress: os.Getenv("GRAM_REDIS_CACHE_ADDR"), ExpectedRedisPort: port("GRAM_REDIS_CACHE_PORT"),
		ComposeProject: os.Getenv("COMPOSE_PROJECT_NAME"), TemporalAddress: os.Getenv("TEMPORAL_ADDRESS"),
		TemporalNamespace: os.Getenv("TEMPORAL_NAMESPACE"), TemporalTaskQueue: os.Getenv("TEMPORAL_TASK_QUEUE"),
	}, nil
}

func runAccountState(ctx context.Context, r accountStateRequest) (any, error) {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if r.Action != "status" && r.Action != "apply" && r.Action != "repair" {
		return nil, errors.New("unknown account-state action")
	}
	if r.Action != "status" {
		if err := r.Profile.Validate(); err != nil {
			return nil, fmt.Errorf("validate requested profile: %w", err)
		}
	}
	preflight, preflightCancel := context.WithTimeout(ctx, 45*time.Second)
	defer preflightCancel()
	config, err := localAccountConfig()
	if err != nil {
		return nil, err
	}
	poolConfig, err := localaccounts.Validate(preflight, config)
	if err != nil {
		return nil, fmt.Errorf("validate local account configuration: %w", err)
	}
	db, err := pgxpool.NewWithConfig(preflight, poolConfig)
	if err != nil {
		return nil, errors.New("cannot open local account database")
	}
	defer db.Close()
	// A session advisory lock spans snapshot, stop, transaction and recovery.
	// Fail rather than wait behind another lifecycle operation. Status and plans
	// remain read-only and never acquire this lifecycle lock.
	if r.Action != "status" && !r.DryRun {
		conn, e := db.Acquire(preflight)
		if e != nil {
			return nil, fmt.Errorf("acquire lifecycle session: %w", e)
		}
		defer conn.Release()
		defer func() {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			// Close the dedicated session: even a failed unlock cannot leak a pool lock.
			_ = conn.Conn().Close(cleanup)
		}()
		locked, e := localrepo.New(conn).TryLockAccountLifecycle(preflight)
		if e != nil {
			return nil, fmt.Errorf("lock account lifecycle: %w", e)
		}
		if !locked {
			return nil, errors.New("another local account command is managing writers; retry after it exits")
		}

	}
	target, err := localaccounts.Resolve(preflight, db, config)
	if err != nil {
		return nil, fmt.Errorf("resolve local account target: %w", err)
	}
	if r.Action == "status" {
		state, err := localaccounts.Status(preflight, db, target.OrganizationID)
		if err != nil {
			return accountStateStatus{target, state}, fmt.Errorf("read local account status: %w", err)
		}
		return accountStateStatus{target, state}, nil
	}
	if err = localaccounts.CheckWorkflows(preflight, config); err != nil {
		return nil, fmt.Errorf("check account workflows: %w", err)
	}
	cacheAddress := config.RedisAddress
	cacheHost, cachePort, _ := net.SplitHostPort(cacheAddress)
	if cacheHost == "localhost" {
		cacheAddress = net.JoinHostPort("127.0.0.1", cachePort)
	}
	cache := redis.NewClient(&redis.Options{Addr: cacheAddress, Password: os.Getenv("GRAM_REDIS_CACHE_PASSWORD"), DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxRetries: -1})
	defer o11y.NoLogDefer(cache.Close)
	features := productfeatures.NewClient(slog.Default(), noop.NewTracerProvider(), db, cache)
	action := func(ctx context.Context) (localaccounts.Result, error) {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if !r.DryRun {
			if err := localaccounts.CheckQuiescent(ctx, config); err != nil {
				return localaccounts.Result{}, fmt.Errorf("check account writers: %w", err)
			}
		}
		if r.DryRun {
			r.progress("Previewing " + readableProfile(r.Profile) + "...")
		} else {
			r.progress("Applying " + readableProfile(r.Profile) + "...")
		}
		return localaccounts.Apply(ctx, db, features, target.OrganizationID, r.Profile, r.DryRun, time.Now(), func(ctx context.Context, tx pgx.Tx) error {
			current, err := localaccounts.Resolve(ctx, tx, config)
			if err != nil {
				return fmt.Errorf("recheck account target: %w", err)
			}
			if current != target {
				return errors.New("selected identity or sole organization changed; retry from status")
			}
			if r.DryRun {
				return localaccounts.CheckWorkflows(ctx, config)
			}
			return localaccounts.CheckQuiescent(ctx, config)
		})
	}
	if r.DryRun {
		result, err := action(ctx)
		result.Prerequisites = []string{"A write automatically stops current-worktree server and worker, then restores only originally running daemons", "Target and absence of open trial-demotion workflows are rechecked before writes"}
		return result, err
	}
	runDaemon := func(ctx context.Context, root string, args ...string) ([]byte, error) {
		return localaccounts.RunDaemon(ctx, root, os.Getenv("PITCHFORK_STATE_DIR"), os.Getenv("XDG_STATE_HOME"), args...)
	}
	result, err := localaccounts.WithStoppedWriters(ctx, config.Root, r.daemonProgress(runDaemon), action)
	if err != nil {
		return result, fmt.Errorf("manage account writers: %w", err)
	}
	return result, nil
}

func (r accountStateRequest) daemonProgress(run localaccounts.DaemonRunner) localaccounts.DaemonRunner {
	lastAction := ""
	return func(ctx context.Context, root string, args ...string) ([]byte, error) {
		if args[0] != lastAction {
			switch args[0] {
			case "stop":
				r.progress("Stopping services...")
			case "start":
				r.progress("Restarting services...")
			}
			lastAction = args[0]
		}
		return run(ctx, root, args...)
	}
}
