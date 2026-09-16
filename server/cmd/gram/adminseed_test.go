package gram

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestAdminSeedCommandOutput(t *testing.T) {
	t.Parallel()
	for _, fails := range []bool{false, true} {
		name := "success"
		if fails {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			seedError := errors.New("seed failed")
			command := newAdminSeedCommand()
			command.Action = adminSeedAction(func(_ context.Context, environment, databaseURL string) error {
				require.Equal(t, "local", environment)
				require.Equal(t, "postgres://gram@127.0.0.1/gram", databaseURL)
				require.Empty(t, output.String(), "must not report success before seeding completes")
				if fails {
					return seedError
				}
				return nil
			})
			app := &cli.App{Writer: &output, Commands: []*cli.Command{command}}
			err := app.Run([]string{"gram", "admin-seed", "--environment=local", "--database-url=postgres://gram@127.0.0.1/gram"})
			if fails {
				require.ErrorIs(t, err, seedError)
				require.Empty(t, output.String())
			} else {
				require.NoError(t, err)
				require.Equal(t, "Seeded 120 fictional organizations (60 active, 60 disabled). Search: Fictional Admin Lab\n", output.String())
			}
		})
	}
}

// adminSeedFailingWriter exercises the CLI's output error path after seeding.
type adminSeedFailingWriter struct{ err error }

func (w adminSeedFailingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestAdminSeedCommandOutputError(t *testing.T) {
	t.Parallel()
	outputError := errors.New("output unavailable")
	command := newAdminSeedCommand()
	command.Action = adminSeedAction(func(context.Context, string, string) error { return nil })
	app := &cli.App{Writer: adminSeedFailingWriter{err: outputError}, Commands: []*cli.Command{command}}
	err := app.Run([]string{"gram", "admin-seed", "--environment=local", "--database-url=postgres://gram@127.0.0.1/gram"})
	require.ErrorIs(t, err, outputError)
	require.ErrorContains(t, err, "report admin seed result")
}
