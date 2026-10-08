package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/url"
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

	endpoint := os.Getenv("WORKOS_API_URL")
	policy, err := workOSPolicy(endpoint, os.Getenv("GRAM_ENVIRONMENT"))
	if err != nil {
		return err
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

	client := workos.NewClient(policy, apiKey, workos.ClientOpts{Endpoint: endpoint, ClientID: ""})
	report, err := directory.AttributeDirectorySources(ctx, pool, client, *organizationID, org.WorkosID.String, !*apply)
	if err != nil {
		return fmt.Errorf("attribute directory sources: %w", err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		if !report.DryRun {
			return fmt.Errorf("encode attribution report (attribution writes already committed): %w", err)
		}
		return fmt.Errorf("encode attribution report (dry-run rolled back): %w", err)
	}
	return nil
}

func workOSPolicy(endpoint, environment string) (*guardian.Policy, error) {
	tracer := noop.NewTracerProvider()
	if endpoint == "" {
		return guardian.NewDefaultPolicy(tracer), nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("WORKOS_API_URL must be an absolute URL without credentials, query or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if loopback && environment == "local" && (u.Scheme == "http" || u.Scheme == "https") {
		policy, err := guardian.NewUnsafePolicy(tracer, []string{})
		if err != nil {
			return nil, fmt.Errorf("create local WorkOS policy: %w", err)
		}
		return policy, nil
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("WORKOS_API_URL requires HTTPS; HTTP is only allowed for a loopback stub with GRAM_ENVIRONMENT=local")
	}
	return guardian.NewDefaultPolicy(tracer), nil
}
