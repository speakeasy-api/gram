package gram

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

//nolint:paralleltest // cli.App.RunContext mutates urfave's shared help flags.
func TestCatalogTransferHasNoUnsafeFlags(t *testing.T) {
	require.Len(t, newCatalogTransferCommand().Flags, 2)
	for _, name := range []string{"operation", "file", "database-url", "site-url", "assets-backend", "assets-uri", "operator", "environment", "cookie", "yes"} {
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

//nolint:paralleltest // CLI shares help flags.
func TestCatalogTransferValidatesExactPlan(t *testing.T) {
	for _, args := range [][]string{
		{"--action", "apply"},
		{"--action", "apply", "--plan-id", "not-a-uuid"},
		{"--action", "apply", "--plan-id", "00000000-0000-0000-0000-000000000000"},
		{"--action", "status", "--plan-id", "11111111-1111-4111-8111-111111111111"},
		{"--action", "sync"},
	} {
		app := cli.NewApp()
		app.Commands = []*cli.Command{newCatalogTransferCommand()}
		err := app.RunContext(t.Context(), append([]string{"test", "catalog-transfer"}, args...))
		require.Error(t, err)
		require.NotContains(t, err.Error(), "must be configured")
	}
}
