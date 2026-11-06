package gram

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

//nolint:paralleltest // cli.App.RunContext mutates urfave's shared help flags.
func TestCatalogTransferHasNoUnsafeFlags(t *testing.T) {
	require.Empty(t, newCatalogTransferCommand().Flags)
	for _, name := range []string{"operation", "file", "database-url", "site-url", "assets-backend", "assets-uri", "operator", "environment", "cookie"} {
		t.Run(name, func(t *testing.T) {
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

//nolint:paralleltest // Mutates process configuration through t.Setenv.
func TestCatalogTransferNamesMissingConfiguration(t *testing.T) {
	for _, missing := range []string{"GRAM_ADMIN_SERVER_URL", "GRAM_ADMIN_OIDC_EMULATOR_URL"} {
		t.Setenv("GRAM_ADMIN_SERVER_URL", "http://127.0.0.1:1")
		t.Setenv("GRAM_ADMIN_OIDC_EMULATOR_URL", "http://127.0.0.1:2")
		t.Setenv(missing, "")
		app := cli.NewApp()
		app.Commands = []*cli.Command{newCatalogTransferCommand()}
		require.ErrorContains(t, app.RunContext(t.Context(), []string{"test", "catalog-transfer"}), missing+" must be configured")
	}
}
