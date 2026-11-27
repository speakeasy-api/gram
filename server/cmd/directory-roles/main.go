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
	"github.com/speakeasy-api/gram/tunnel/redisconn"
	"golang.org/x/oauth2"
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
	authAddress := strings.TrimSpace(getenv("GRAM_REDIS_AUTH_ADDR"))
	fallbackAddress := strings.TrimSpace(getenv("GRAM_REDIS_CACHE_ADDR"))
	redisURL := getenv("GRAM_DIRECTORY_ROLES_REDIS_URL")
	if getenv("GRAM_DATABASE_URL") == "" || authAddress == "" && fallbackAddress == "" && redisURL == "" || getenv("GRAM_SUPPORT_SESSION_TOKEN") == "" {
		return errors.New("database, Redis connection and support-session credentials are required")
	}
	databaseConfig, err := parseDatabaseConnection(getenv("GRAM_DATABASE_URL"))
	if err != nil {
		return err
	}
	redisConfig, err := supportRedisOptions(ctx, getenv, authAddress, fallbackAddress, redisURL)
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
	databaseConfig, err := parseDatabaseConnection(databaseURL)
	if err != nil {
		return nil, nil, err
	}
	redisConfig, err := parseRedisConnection(redisAddress, redisPassword, redisURL)
	if err != nil {
		return nil, nil, err
	}
	return databaseConfig, redisConfig, nil
}

func parseDatabaseConnection(databaseURL string) (*pgxpool.Config, error) {
	databaseConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	if !localHost(databaseConfig.ConnConfig.Host) && !verifiedTLS(databaseConfig.ConnConfig.TLSConfig) {
		return nil, errors.New("remote PostgreSQL requires sslmode=verify-full")
	}
	// A fallback may only retry the same server, for example with different
	// TLS settings, so an outage can never redirect writes to another host.
	for _, fallback := range databaseConfig.ConnConfig.Fallbacks {
		if fallback.Host != databaseConfig.ConnConfig.Host || fallback.Port != databaseConfig.ConnConfig.Port {
			return nil, errors.New("PostgreSQL must name a single host; fallbacks to other hosts are not allowed")
		}
		if !localHost(fallback.Host) && !verifiedTLS(fallback.TLSConfig) {
			return nil, errors.New("remote PostgreSQL cannot fall back to unverified or plaintext connections")
		}
	}
	return databaseConfig, nil
}

func supportRedisOptions(ctx context.Context, getenv func(string) string, authAddress, fallbackAddress, redisURL string) (*redis.Options, error) {
	effectiveAddress := authAddress
	if effectiveAddress == "" {
		effectiveAddress = fallbackAddress
	}
	// A direct URL names the instance the command will write to, so the move
	// check compares the Auth target against it.
	if redisURL != "" {
		parsed, err := redis.ParseURL(redisURL)
		if err != nil {
			return nil, errors.New("invalid Redis configuration")
		}
		effectiveAddress = parsed.Addr
	}
	if next := strings.TrimSpace(getenv("GRAM_REDIS_AUTH_NEXT_ADDR")); next != "" && next != effectiveAddress {
		return nil, errors.New("finish the Auth Redis move before running directory roles")
	}
	if redisURL != "" || authAddress == "" || authAddress == fallbackAddress {
		return parseRedisConnection(fallbackAddress, getenv("GRAM_REDIS_CACHE_PASSWORD"), redisURL)
	}
	endpoint := redisconn.PasswordEndpoint(authAddress, getenv("GRAM_REDIS_AUTH_PASSWORD"))
	if mode := strings.TrimSpace(getenv("GRAM_REDIS_AUTH_AUTH_MODE")); mode != "" {
		endpoint.Auth = redisconn.AuthMode(mode)
	}
	endpoint.CACertPEM = getenv("GRAM_REDIS_AUTH_CA_CERT")
	if err := endpoint.Validate(); err != nil {
		return nil, fmt.Errorf("configure Auth Redis: %w", err)
	}
	host, _, _ := net.SplitHostPort(authAddress)
	if !localHost(host) && !endpoint.TLS() {
		return nil, errors.New("remote Auth Redis requires GRAM_REDIS_AUTH_CA_CERT")
	}
	var tokens oauth2.TokenSource
	if endpoint.Auth == redisconn.AuthIAM {
		var err error
		tokens, err = redisconn.NewIAMTokenSource(ctx)
		if err != nil {
			return nil, fmt.Errorf("initialize Auth Redis credentials: %w", err)
		}
	}
	connector, err := redisconn.NewConnector(endpoint, tokens)
	if err != nil {
		return nil, fmt.Errorf("configure Auth Redis connector: %w", err)
	}
	return connector.Options(&redis.Options{}), nil
}

func parseRedisConnection(redisAddress, redisPassword, redisURL string) (*redis.Options, error) {
	redisConfig := &redis.Options{Addr: redisAddress, Password: redisPassword}
	if redisURL != "" {
		var err error
		redisConfig, err = redis.ParseURL(redisURL)
		if err != nil {
			return nil, errors.New("invalid Redis URL")
		}
	}
	host, _, err := net.SplitHostPort(redisConfig.Addr)
	if redisConfig.Network != "unix" && (err != nil || !localHost(host)) && !verifiedTLS(redisConfig.TLSConfig) {
		return nil, errors.New("remote Redis requires GRAM_DIRECTORY_ROLES_REDIS_URL with rediss:// and certificate verification")
	}
	return redisConfig, nil
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
