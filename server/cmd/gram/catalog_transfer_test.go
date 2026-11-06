package gram

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestCatalogTransferHasNoUnsafeFlags(t *testing.T) {
	t.Parallel()
	require.Empty(t, newCatalogTransferCommand().Flags)
	for _, name := range []string{"operation", "file", "database-url", "site-url", "assets-backend", "assets-uri", "operator", "environment", "cookie"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app := cli.NewApp()
			app.Writer = io.Discard
			app.ErrWriter = io.Discard
			app.Commands = []*cli.Command{newCatalogTransferCommand()}
			require.ErrorContains(t, app.RunContext(t.Context(), []string{"test", "catalog-transfer", "--" + name, "unsafe"}), "flag provided but not defined")
		})
	}
}

func TestCatalogTransferRejectsRemoteConfiguration(t *testing.T) {
	t.Setenv("GRAM_ADMIN_SERVER_URL", "https://admin.example.test")
	t.Setenv("GRAM_ADMIN_OIDC_EMULATOR_URL", "http://127.0.0.1:1")
	app := cli.NewApp()
	app.Commands = []*cli.Command{newCatalogTransferCommand()}
	require.ErrorContains(t, app.RunContext(t.Context(), []string{"test", "catalog-transfer"}), "loopback")
}
