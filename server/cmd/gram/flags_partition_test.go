package gram

import (
	"fmt"
	"os"
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

func TestPartitionFlag_RequiredWhenUnset(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

	_, err := runPartitionApp(t, "probe")
	require.ErrorContains(t, err, "--partition (GRAM_PARTITION) is required: set it to one of us, eu")
}

func TestPartitionFlag_ReachesSubcommand(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

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

func TestPartitionFlag_RejectsUnknownPartition(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

	_, err := runPartitionApp(t, "--partition", "apac", "probe")
	require.ErrorContains(t, err, `invalid --partition: unknown partition "apac"`)
}

func TestApp_RequiresPartitionBeforeAnyCommand(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

	err := newApp().RunContext(t.Context(), []string{"gram", "start"})
	require.ErrorContains(t, err, "--partition (GRAM_PARTITION) is required")
}

func TestApp_RequiresPartitionWithHelpFlagAfterTerminator(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

	err := newApp().RunContext(t.Context(), []string{"gram", "start", "--", "--help"})
	require.ErrorContains(t, err, "--partition (GRAM_PARTITION) is required")
}

func TestApp_RejectsUnknownPartitionBeforeAnyCommand(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

	err := newApp().RunContext(t.Context(), []string{"gram", "--partition", "apac", "version"})
	require.ErrorContains(t, err, `invalid --partition: unknown partition "apac"`)
}

func TestApp_HelpAndVersionRunWithoutPartition(t *testing.T) {
	// Unset rather than empty: a value from the shell or mise.toml must not
	// change the outcome. t.Setenv restores the original when the test ends.
	t.Setenv("GRAM_PARTITION", "")
	require.NoError(t, os.Unsetenv("GRAM_PARTITION"))

	tests := []struct {
		name string
		args []string
	}{
		{name: "version", args: []string{"gram", "version"}},
		{name: "help command", args: []string{"gram", "help"}},
		{name: "root help flag", args: []string{"gram", "--help"}},
		{name: "command help flag", args: []string{"gram", "start", "--help"}},
	}
	for _, tc := range tests { //nolint:paralleltest // subtests share the parent's environment and urfave's help command
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, newApp().RunContext(t.Context(), tc.args))
		})
	}
}

func TestPrintsHelpOrVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no command", args: nil, want: true},
		{name: "version", args: []string{"version"}, want: true},
		{name: "help command", args: []string{"help", "start"}, want: true},
		{name: "help flag after command", args: []string{"start", "--help"}, want: true},
		{name: "short help flag", args: []string{"worker", "-h"}, want: true},
		{name: "help flag before terminator", args: []string{"start", "--help", "--", "x"}, want: true},
		{name: "help flag after terminator", args: []string{"start", "--", "--help"}, want: false},
		{name: "short help flag after terminator", args: []string{"demo-seed", "--", "-h"}, want: false},
		{name: "service command", args: []string{"start"}, want: false},
		{name: "service command with flags", args: []string{"worker", "--environment", "prod"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.want, printsHelpOrVersion(tc.args))
		})
	}
}

func TestPullPartition_PanicsWithoutRootCommand(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t, "PullPartition: partition not found in context", func() {
		PullPartition(t.Context())
	})
}
