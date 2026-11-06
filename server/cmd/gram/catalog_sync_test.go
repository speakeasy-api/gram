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
			flags.String("catalog-source-url", tt.url, "")
			flags.String("catalog-source-audience", tt.audience, "")
			flags.String("catalog-source-reader", tt.reader, "")
			c := cli.NewContext(cli.NewApp(), flags, nil)
			err := configureCatalogSync(t.Context(), c, goahttp.NewMuxer(), &admin.Service{}, nil, nil)
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
