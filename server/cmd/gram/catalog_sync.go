package gram

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/catalogsource"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"
)

// catalogSourceIdentity binds baselines to the configured authenticated source
// without exposing its internal address or audience in plans and receipts.
func catalogSourceIdentity(sourceURL, audience string) string {
	if sourceURL == "" || audience == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(sourceURL+"\n"+audience)))
}

func catalogSyncFlags() []cli.Flag {
	return []cli.Flag{
		catalogSourceManagedFlag(),
		&cli.StringFlag{Name: "catalog-export-audience", EnvVars: []string{"GRAM_CATALOG_EXPORT_AUDIENCE"}, Usage: "Exact Google ID-token audience for the production-only internal catalog export"},
		&cli.StringSliceFlag{Name: "catalog-export-subjects", EnvVars: []string{"GRAM_CATALOG_EXPORT_SUBJECTS"}, Usage: "Allowlisted immutable Google service-account subject IDs for read-only catalog export"},
		&cli.StringFlag{Name: "catalog-source-url", EnvVars: []string{"GRAM_CATALOG_SOURCE_URL"}, Usage: "Fixed internal HTTPS production catalog export endpoint for local/dev sync"},
		&cli.StringFlag{Name: "catalog-source-audience", EnvVars: []string{"GRAM_CATALOG_SOURCE_AUDIENCE"}, Usage: "Exact Google ID-token audience expected by the configured catalog source"},
		&cli.StringFlag{Name: "catalog-source-reader", EnvVars: []string{"GRAM_CATALOG_SOURCE_READER"}, Usage: "Dedicated catalog-reader service account to impersonate using ADC; required locally, optional for dev workload identity"},
	}
}

// configureCatalogSync binds deployment identity and all storage/source authority
// once at Admin startup. Neither CLI nor HTTP payloads can supply a target DB.
func configureCatalogSync(ctx context.Context, c *cli.Context, mux goahttp.Muxer, service *admin.Service, registry *mcpregistry.Service, store *mcpregistry.CatalogAssetStore) error {
	environment := c.String("environment")
	var source admin.CatalogSource
	var destination string
	sourceURL, audience, reader := c.String("catalog-source-url"), c.String("catalog-source-audience"), c.String("catalog-source-reader")
	if c.Bool("catalog-source-managed") || sourceURL != "" || audience != "" || reader != "" {
		if environment != "local" && environment != "dev" {
			return fmt.Errorf("catalog destination configuration is allowed only in local/dev")
		}
		// Sync and registry seeding must share one explicit authority choice.
		// Source settings alone cannot enable writes while workers retain the
		// legacy seed that would recreate removals or enrich testing edits.
		if !c.Bool("catalog-source-managed") {
			return fmt.Errorf("catalog sync requires GRAM_CATALOG_SOURCE_MANAGED=true for Admin, server, worker, and app-seed; configure the mode consistently and restart running services before syncing")
		}
		if sourceURL == "" || audience == "" {
			return fmt.Errorf("catalog source URL and audience must both be configured")
		}
		if environment == "local" && reader == "" {
			return fmt.Errorf("local catalog sync requires a dedicated source reader identity")
		}
		if store == nil {
			return fmt.Errorf("catalog sync requires configured platform asset storage")
		}
		var err error
		destination, err = catalogResetDestination(environment, c.String("admin-server-url"))
		if err != nil {
			return err
		}
		tokens, err := catalogsource.NewGoogleTokenSource(ctx, audience, reader)
		if err != nil {
			return fmt.Errorf("configure catalog source identity: %w", err)
		}
		client, err := catalogsource.NewClient(sourceURL, tokens, nil)
		if err != nil {
			return fmt.Errorf("configure production catalog source: %w", err)
		}
		source = client
	}
	// Always register the authenticated destination guard, including production.
	// A disabled source does not prevent normal local Admin startup without ADC.
	sourceIdentity := catalogSourceIdentity(sourceURL, audience)
	mux.Handle(http.MethodPost, admin.CatalogSyncPath, service.CatalogSyncHandler(environment, source, store, sourceIdentity).ServeHTTP)
	mux.Handle(http.MethodPost, admin.CatalogRefreshPath, service.CatalogRefreshHandler(environment, sourceIdentity, source, store).ServeHTTP)
	mux.Handle(http.MethodPost, admin.CatalogResetPath, service.CatalogResetHandler(environment, sourceIdentity, destination, source, store).ServeHTTP)
	exportAudience, subjects := c.String("catalog-export-audience"), c.StringSlice("catalog-export-subjects")
	if exportAudience != "" || len(subjects) > 0 {
		if environment != "prod" {
			return fmt.Errorf("canonical catalog export is allowed only in prod")
		}
		if store == nil {
			return fmt.Errorf("catalog export requires configured platform asset storage")
		}
		verifier, err := catalogsource.NewGoogleVerifier(ctx, exportAudience, subjects, nil)
		if err != nil {
			return fmt.Errorf("configure catalog export verification: %w", err)
		}
		handler := catalogsource.NewExportHandler(func(ctx context.Context) (mcpregistry.CatalogBundle, error) {
			return registry.ExportCatalog(ctx, store)
		}, verifier, PullLogger(ctx))
		mux.Handle(http.MethodGet, catalogsource.ExportPath, handler.ServeHTTP)
	}
	return nil
}

// catalogResetDestination is derived exclusively from trusted startup settings.
// Reject sensitive URL components rather than exposing them in confirmation text.
func catalogResetDestination(environment, serverURL string) (string, error) {
	u, err := url.Parse(serverURL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(serverURL, "#") {
		return "", fmt.Errorf("catalog reset requires a valid admin-server-url without credentials, query, or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+u.Port())
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return environment + " " + u.String(), nil
}
