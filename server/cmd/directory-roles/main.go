// Command directory-roles imports inventoried WorkOS group rules or reports the
// effect of retiring their direct role assignments. Output is private operator
// data, never a public artifact. No WorkOS API is called by this command.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
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
	if getenv("GRAM_DATABASE_URL") == "" || getenv("GRAM_REDIS_CACHE_ADDR") == "" || getenv("GRAM_SUPPORT_SESSION_TOKEN") == "" {
		return errors.New("GRAM_DATABASE_URL, GRAM_REDIS_CACHE_ADDR and GRAM_SUPPORT_SESSION_TOKEN are required")
	}

	logger := slog.Default()
	db, err := pgxpool.New(ctx, getenv("GRAM_DATABASE_URL"))
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer db.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: getenv("GRAM_REDIS_CACHE_ADDR"), Password: getenv("GRAM_REDIS_CACHE_PASSWORD")})
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
