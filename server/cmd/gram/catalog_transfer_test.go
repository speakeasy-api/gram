package gram

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

//nolint:paralleltest // cli.App.RunContext mutates urfave's shared help flags.
func TestCatalogTransferHasNoUnsafeFlags(t *testing.T) {
	require.Len(t, newCatalogTransferCommand().Flags, 4)
	for _, name := range []string{"file", "database-url", "site-url", "assets-backend", "assets-uri", "operator", "environment", "cookie", "yes"} {
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
	const id = "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--action", "apply"}, "apply requires --plan-id with the exact nonzero preview UUID"},
		{[]string{"--action", "apply", "--plan-id", "not-a-uuid"}, "apply requires --plan-id with the exact nonzero preview UUID"},
		{[]string{"--action", "apply", "--plan-id", "00000000-0000-0000-0000-000000000000"}, "apply requires --plan-id with the exact nonzero preview UUID"},
		{[]string{"--action", "status", "--plan-id", id}, "--plan-id is accepted only with --action apply"},
		{[]string{"--action", "preview", "--plan-id", id}, "--plan-id is accepted only with --action apply"},
		{[]string{"--action", "bootstrap", "--plan-id", id}, "--plan-id is accepted only with --action apply"},
		{[]string{"--action", "sync"}, "--action must be bootstrap, preview, apply, or status"},
	} {
		app := cli.NewApp()
		app.Commands = []*cli.Command{newCatalogTransferCommand()}
		err := app.RunContext(t.Context(), append([]string{"test", "catalog-transfer"}, tc.args...))
		require.ErrorContains(t, err, tc.want)
	}
}

//nolint:paralleltest // cli.App.RunContext mutates shared help flags.
func TestCatalogTransferResetRequiresExplicitPlanAndConfirmation(t *testing.T) {
	for _, flags := range [][]string{
		{"--operation", "reset", "--action", "bootstrap"},
		{"--operation", "reset", "--action", "apply", "--plan-id", "11111111-1111-4111-8111-111111111111"},
		{"--operation", "refresh", "--confirmation", "RESET local"},
		{"--operation", "unknown"},
	} {
		app := cli.NewApp()
		app.Writer = io.Discard
		app.ErrWriter = io.Discard
		app.Commands = []*cli.Command{newCatalogTransferCommand()}
		require.Error(t, app.RunContext(t.Context(), append([]string{"test", "catalog-transfer"}, flags...)))
	}
}
