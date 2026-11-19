package gram

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/partition"
)

// runPartitionApp runs a minimal app with the global partition flag and
// returns the partition the root command recorded in the context.
func runPartitionApp(t *testing.T, args ...string) (partition.Partition, error) {
	t.Helper()

	var got partition.Partition
	app := &cli.App{
		Name:  "gram",
		Flags: []cli.Flag{partitionCLIFlag()},
		Before: func(c *cli.Context) error {
			p, err := partitionFromCLI(c)
			if err != nil {
				return err
			}
			c.Context = PushPartition(c.Context, p)
			return nil
		},
		Commands: []*cli.Command{{
			Name: "probe",
			Action: func(c *cli.Context) error {
				got = PullPartition(c.Context)
				return nil
			},
		}},
	}

	if err := app.RunContext(t.Context(), append([]string{"gram"}, args...)); err != nil {
		return "", fmt.Errorf("run app: %w", err)
	}

	return got, nil
}

func TestPartitionFlag_DefaultsToUS(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	got, err := runPartitionApp(t, "probe")
	require.NoError(t, err)
	require.Equal(t, partition.US, got)
}

func TestPartitionFlag_ReachesSubcommand(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	got, err := runPartitionApp(t, "--partition", "eu", "probe")
	require.NoError(t, err)
	require.Equal(t, partition.EU, got)
}

func TestPartitionFlag_ReadsEnvironmentVariable(t *testing.T) {
	t.Setenv("GRAM_PARTITION", "eu")

	got, err := runPartitionApp(t, "probe")
	require.NoError(t, err)
	require.Equal(t, partition.EU, got)
}

func TestPartitionFlag_RejectsEmptyEnvironmentVariable(t *testing.T) {
	t.Setenv("GRAM_PARTITION", "")

	_, err := runPartitionApp(t, "probe")
	require.ErrorContains(t, err, `invalid --partition: unknown partition ""`)
}

func TestPartitionFlag_RejectsUnknownPartition(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	_, err := runPartitionApp(t, "--partition", "apac", "probe")
	require.ErrorContains(t, err, `invalid --partition: unknown partition "apac"`)
}

func TestApp_RejectsUnknownPartitionBeforeAnyCommand(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	err := newApp().RunContext(t.Context(), []string{"gram", "--partition", "apac", "version"})
	require.ErrorContains(t, err, `invalid --partition: unknown partition "apac"`)
}

func TestPullPartition_PanicsWithoutRootCommand(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "PullPartition: partition not found in context", func() {
		PullPartition(t.Context())
	})
}
