package guardian_test

import (
	"bytes"
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/functions/internal/guardian"
)

func TestNewCommand(t *testing.T) {
	t.Parallel()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd := guardian.NewCommand(t.Context(), "echo", "hello")
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	require.NoError(t, err)

	require.Equal(t, "hello\n", stdout.String())
	require.Empty(t, stderr.String())
}

func TestCommandNoEnvInheritance(t *testing.T) {
	t.Parallel()

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	t.Setenv("AUTH_SECRET", "fancy-secret")
	cmd := guardian.NewCommand(t.Context(), "printenv", "AUTH_SECRET")
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	var exiterr *exec.ExitError
	require.ErrorAs(t, err, &exiterr)
	require.Equal(t, 1, exiterr.ExitCode())

	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())
}

// TestCommandNoEnvInheritance_Negative proves that TestCommandNoEnvInheritance
// is working as intended by showing that a normal exec.Command does inherit
// the environment variables.
func TestCommandNoEnvInheritance_Negative(t *testing.T) {
	t.Parallel()

	out := new(bytes.Buffer)
	ctx := context.Background()
	t.Setenv("AUTH_SECRET", "fancy-secret")
	cmd := exec.CommandContext(ctx, "printenv", "AUTH_SECRET")
	cmd.Stdout = out

	err := cmd.Run()
	require.NoError(t, err)

	require.Equal(t, "fancy-secret\n", out.String())
}
