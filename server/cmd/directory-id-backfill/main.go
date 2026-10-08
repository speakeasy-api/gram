package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	organizationrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("directory-id-backfill", flag.ContinueOnError)
	organizationID := flags.String("organization-id", "", "Exact Gram organization ID to process")
	apply := flags.Bool("apply", false, "Commit matched directory ID attribution; default is dry-run")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *organizationID == "" {
		return fmt.Errorf("--organization-id is required")
	}
	databaseURL := os.Getenv("GRAM_DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("GRAM_DATABASE_URL is required")
	}
	apiKey := os.Getenv("WORKOS_API_KEY")
	if apiKey == "" || apiKey == "unset" {
		return fmt.Errorf("WORKOS_API_KEY is required")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	defer pool.Close()

	org, err := organizationrepo.New(pool).GetOrganizationMetadata(ctx, *organizationID)
	if err != nil {
		return fmt.Errorf("load organization %q: %w", *organizationID, err)
	}
	if !org.WorkosID.Valid || org.WorkosID.String == "" {
		return fmt.Errorf("organization %q has no WorkOS organization ID", *organizationID)
	}

	client := workos.NewClient(guardian.NewDefaultPolicy(noop.NewTracerProvider()), apiKey, workos.ClientOpts{Endpoint: os.Getenv("WORKOS_API_URL"), ClientID: ""})
	report, err := directory.AttributeDirectorySources(ctx, pool, client, *organizationID, org.WorkosID.String, !*apply)
	if err != nil {
		return fmt.Errorf("attribute directory sources: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("encode attribution report: %w", err)
	}
	return nil
}
