package gram

import (
	"flag"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"
)

func TestCatalogSourceIdentity(t *testing.T) {
	t.Parallel()
	identity := catalogSourceIdentity("https://production.example.test/admin/catalog.export", "catalog-audience")
	require.Len(t, identity, 64)
	require.Equal(t, identity, catalogSourceIdentity("https://production.example.test/admin/catalog.export", "catalog-audience"))
	require.NotEqual(t, identity, catalogSourceIdentity("https://another.example.test/admin/catalog.export", "catalog-audience"))
	require.NotEqual(t, identity, catalogSourceIdentity("https://production.example.test/admin/catalog.export", "other-audience"))
	require.Empty(t, catalogSourceIdentity("", ""))
}

func TestCatalogSyncStartupConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, environment, url, audience, reader string
		wantError                                bool
	}{
		{"disabled local has no cloud dependency", "local", "", "", "", false},
		{"production destination refuses source", "prod", "https://production.example.test/admin/catalog.export", "aud", "", true},
		{"unknown destination refuses source", "preview", "https://production.example.test/admin/catalog.export", "aud", "", true},
		{"local requires dedicated reader", "local", "https://production.example.test/admin/catalog.export", "aud", "", true},
		{"local config is lazy", "local", "https://production.example.test/admin/catalog.export", "aud", "reader@example.iam.gserviceaccount.com", false},
		{"dev uses workload identity lazily", "dev", "https://production.example.test/admin/catalog.export", "aud", "", false},
		{"no plaintext source", "dev", "http://production.example.test/admin/catalog.export", "aud", "", true},
		{"partial source fails closed", "dev", "", "aud", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			flags.String("environment", tt.environment, "")
			flags.String("admin-server-url", "https://admin.example.test", "")
			flags.Bool("catalog-source-managed", tt.url != "" || tt.audience != "" || tt.reader != "", "")
			flags.String("catalog-source-url", tt.url, "")
			flags.String("catalog-source-audience", tt.audience, "")
			flags.String("catalog-source-reader", tt.reader, "")
			c := cli.NewContext(cli.NewApp(), flags, nil)
			var store *mcpregistry.CatalogAssetStore
			if tt.url != "" || tt.audience != "" || tt.reader != "" {
				store = &mcpregistry.CatalogAssetStore{}
			}
			err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, store)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCatalogExportStartupRequiresAllowlist(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	flags.String("environment", "prod", "")
	flags.String("catalog-export-audience", "aud", "")
	c := cli.NewContext(cli.NewApp(), flags, nil)
	err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, &mcpregistry.CatalogAssetStore{})
	require.ErrorContains(t, err, "subjects are required")
}

func TestCatalogSyncStartupRequiresPlatformAssetStorage(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "dev"} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			flags.String("environment", environment, "")
			flags.Bool("catalog-source-managed", true, "")
			flags.String("catalog-source-url", "https://production.example.test/admin/catalog.export", "")
			flags.String("catalog-source-audience", "aud", "")
			flags.String("catalog-source-reader", "reader@example.iam.gserviceaccount.com", "")
			c := cli.NewContext(cli.NewApp(), flags, nil)
			err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, nil)
			require.EqualError(t, err, "catalog sync requires configured platform asset storage")
		})
	}
}

func TestCatalogSyncRequiresExplicitSourceManagedMode(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"local", "dev"} {
		t.Run(environment, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
			flags.String("environment", environment, "")
			flags.Bool("catalog-source-managed", false, "")
			flags.String("catalog-source-url", "https://production.example.test/admin/catalog.export", "")
			flags.String("catalog-source-audience", "aud", "")
			flags.String("catalog-source-reader", "reader@example.iam.gserviceaccount.com", "")
			c := cli.NewContext(cli.NewApp(), flags, nil)
			err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, &mcpregistry.CatalogAssetStore{})
			require.ErrorContains(t, err, "GRAM_CATALOG_SOURCE_MANAGED=true")
			require.ErrorContains(t, err, "restart")
		})
	}
}

func TestCatalogSourceManagedModeRequiresSourceConfiguration(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	flags.String("environment", "local", "")
	flags.Bool("catalog-source-managed", true, "")
	c := cli.NewContext(cli.NewApp(), flags, nil)
	err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, &mcpregistry.CatalogAssetStore{})
	require.ErrorContains(t, err, "source URL and audience")
}

func TestCatalogResetDestination(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "relative", "ftp://admin.example.test", "https:///missing-host", "https://operator:secret@admin.example.test", "https://admin.example.test?secret=value", "https://admin.example.test#secret"} {
		_, err := catalogResetDestination("local", raw)
		require.EqualError(t, err, "catalog reset requires a valid admin-server-url without credentials, query, or fragment")
	}
	destination, err := catalogResetDestination("dev", "HTTPS://ADMIN.EXAMPLE.TEST:443/")
	require.NoError(t, err)
	require.Equal(t, "dev https://admin.example.test", destination)
	local, err := catalogResetDestination("local", "http://localhost:8080/")
	require.NoError(t, err)
	require.Equal(t, "local http://localhost:8080", local)
}

func TestCatalogResetStartupRequiresDestination(t *testing.T) {
	t.Parallel()
	flags := flag.NewFlagSet("catalog", flag.ContinueOnError)
	flags.String("environment", "dev", "")
	flags.Bool("catalog-source-managed", true, "")
	flags.String("catalog-source-url", "https://production.example.test/admin/catalog.export", "")
	flags.String("catalog-source-audience", "aud", "")
	c := cli.NewContext(cli.NewApp(), flags, nil)
	err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, &mcpregistry.CatalogAssetStore{})
	require.ErrorContains(t, err, "admin-server-url")
}
