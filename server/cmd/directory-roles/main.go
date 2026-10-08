// Command directory-roles imports inventoried WorkOS group rules or reports the
// effect of retiring their direct role assignments. Output is private operator
// data, never a public artifact. No WorkOS API is called by this command.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type commandOptions struct {
	shadow              bool
	apply               bool
	confirmOrganization string
}

func parseOptions(args []string) (commandOptions, error) {
	options := commandOptions{shadow: false, apply: false, confirmOrganization: ""}
	if len(args) == 0 || args[0] != "import" && args[0] != "shadow" {
		return options, errors.New("usage: directory-roles import [-apply -confirm-organization <ORG_ID>] | shadow; read inventory JSON from stdin")
	}
	options.shadow = args[0] == "shadow"
	flags := flag.NewFlagSet("directory-roles", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.apply, "apply", false, "commit mappings (default: dry run)")
	flags.StringVar(&options.confirmOrganization, "confirm-organization", "", "must match the inventory for an import write")
	if err := flags.Parse(args[1:]); err != nil {
		return options, errors.New("invalid directory-roles flags")
	}
	if flags.NArg() != 0 || options.shadow && (options.apply || options.confirmOrganization != "") {
		return options, errors.New("shadow accepts no write flags or positional arguments")
	}
	return options, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Getenv)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, input io.Reader, output io.Writer, getenv func(string) string) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	inventory, err := access.DecodeDirectoryRoleInventory(input)
	if err != nil {
		return fmt.Errorf("decode inventory: %w", err)
	}
	if options.apply && options.confirmOrganization != inventory.OrganizationID {
		return errors.New("writes require -confirm-organization to match the inventory")
	}
	if getenv("GRAM_DATABASE_URL") == "" || getenv("GRAM_REDIS_CACHE_ADDR") == "" && getenv("GRAM_DIRECTORY_ROLES_REDIS_URL") == "" || getenv("GRAM_SUPPORT_SESSION_TOKEN") == "" {
		return errors.New("database, Redis connection and support-session credentials are required")
	}
	databaseConfig, redisConfig, err := parseConnections(getenv("GRAM_DATABASE_URL"), getenv("GRAM_REDIS_CACHE_ADDR"), getenv("GRAM_REDIS_CACHE_PASSWORD"), getenv("GRAM_DIRECTORY_ROLES_REDIS_URL"))
	if err != nil {
		return err
	}

	logger := slog.Default()
	db, err := pgxpool.NewWithConfig(ctx, databaseConfig)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer db.Close()

	redisClient := redis.NewClient(redisConfig)
	defer o11y.LogDefer(ctx, logger, "close directory role session cache", redisClient.Close)
	manager := sessions.NewManager(logger, noop.NewTracerProvider(), db, redisClient, cache.SuffixNone, nil, nil, nil)
	ctx, err = manager.AuthenticateSupportCommand(ctx, getenv("GRAM_SUPPORT_SESSION_TOKEN"), inventory.OrganizationID, time.Now())
	if err != nil {
		return errors.New("could not authenticate a current support session for the inventory organization")
	}
	engine := authz.NewEngine(logger, db, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	ctx, err = engine.PrepareContext(ctx)
	if err != nil {
		return errors.New("could not prepare support session authorization")
	}
	report, runErr := access.RunDirectoryRoleInventory(ctx, logger, db, engine, audit.NewLogger(), inventory, options.shadow, options.apply)
	if runErr != nil {
		runErr = fmt.Errorf("directory role operation: %w", runErr)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.Join(runErr, errors.New("could not write private report; check committed mapping state before retrying"))
	}
	return runErr
}

// Plaintext is permitted only over loopback or local sockets, where the operator
// owns the endpoint or a protected tunnel. Direct remote connections verify TLS.
func parseConnections(databaseURL, redisAddress, redisPassword, redisURL string) (*pgxpool.Config, *redis.Options, error) {
	databaseConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, nil, errors.New("invalid database configuration")
	}
	if !localHost(databaseConfig.ConnConfig.Host) && !verifiedTLS(databaseConfig.ConnConfig.TLSConfig) {
		return nil, nil, errors.New("remote PostgreSQL requires sslmode=verify-full")
	}
	for _, fallback := range databaseConfig.ConnConfig.Fallbacks {
		if !localHost(fallback.Host) && !verifiedTLS(fallback.TLSConfig) {
			return nil, nil, errors.New("remote PostgreSQL cannot fall back to unverified or plaintext connections")
		}
	}
	redisConfig := &redis.Options{Addr: redisAddress, Password: redisPassword}
	if redisURL != "" {
		redisConfig, err = redis.ParseURL(redisURL)
		if err != nil {
			return nil, nil, errors.New("invalid Redis URL")
		}
	}
	host, _, err := net.SplitHostPort(redisConfig.Addr)
	if redisConfig.Network != "unix" && (err != nil || !localHost(host)) && !verifiedTLS(redisConfig.TLSConfig) {
		return nil, nil, errors.New("remote Redis requires GRAM_DIRECTORY_ROLES_REDIS_URL with rediss:// and certificate verification")
	}
	return databaseConfig, redisConfig, nil
}

func verifiedTLS(config *tls.Config) bool {
	return config != nil && !config.InsecureSkipVerify && config.ServerName != ""
}

func localHost(host string) bool {
	if strings.HasPrefix(host, "/") || strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}
