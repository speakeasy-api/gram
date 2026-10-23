package gram

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestCodeRuntimePreviewTokenMustBeSeparate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		preview bool
		token   string
		allowed bool
	}{
		{"development", false, "", true}, {"preview-missing", true, "", false},
		{"preview-reused", true, strings.Repeat("d", 32), false}, {"preview-isolated", true, strings.Repeat("p", 32), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("code", flag.ContinueOnError)
			flags.String("code-runtime-token", strings.Repeat("d", 32), "")
			flags.String("code-runtime-preview-token", tc.token, "")
			flags.Bool("code-runtime-is-preview", tc.preview, "")
			token, err := codeRuntimeToken(cli.NewContext(cli.NewApp(), flags, nil))
			if !tc.allowed {
				require.ErrorContains(t, err, "distinct preview token")
				return
			}
			require.NoError(t, err)
			if tc.preview {
				require.Equal(t, tc.token, token)
			} else {
				require.Equal(t, strings.Repeat("d", 32), token)
			}
		})
	}
}

func TestCodeRuntimeLimitsRejectInvalidValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		organization int
		caller       int
		wait         time.Duration
		allowed      bool
	}{
		{"defaults", 0, 0, 0, true}, {"configured", 8, 2, time.Second, true},
		{"negative-organization", -1, 0, 0, false}, {"negative-caller", 0, -1, 0, false},
		{"negative-wait", 0, 0, -time.Second, false}, {"wait-covers-wall-time", 0, 0, codemode.MaxWallTime, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flags := flag.NewFlagSet("code", flag.ContinueOnError)
			flags.Int("code-runtime-max-organization-executions", tc.organization, "")
			flags.Int("code-runtime-max-caller-executions", tc.caller, "")
			flags.Duration("code-runtime-admission-wait", tc.wait, "")
			limits, err := codeRuntimeLimits(cli.NewContext(cli.NewApp(), flags, nil))
			if !tc.allowed {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, codemode.Limits{OrganizationExecutions: tc.organization, CallerExecutions: tc.caller, AdmissionWait: tc.wait}, limits)
		})
	}
}

type stoppableCodeExecutor struct {
	codemode.Disabled
	stopped int
	err     error
}

func (e *stoppableCodeExecutor) Shutdown(context.Context) error {
	e.stopped++
	return e.err
}

// A service that fails to start never reaches Service.Shutdown, so startup
// releases the runtime it created.
func TestReleaseCodeExecutorStopsUnownedRuntime(t *testing.T) {
	t.Parallel()
	require.NoError(t, releaseCodeExecutor(t.Context(), codemode.Disabled{}))
	runtime := &stoppableCodeExecutor{Disabled: codemode.Disabled{}, stopped: 0, err: nil}
	require.NoError(t, releaseCodeExecutor(t.Context(), runtime))
	require.Equal(t, 1, runtime.stopped)
	failing := &stoppableCodeExecutor{Disabled: codemode.Disabled{}, stopped: 0, err: errors.New("still draining")}
	require.ErrorContains(t, releaseCodeExecutor(t.Context(), failing), "release code runtime: still draining")
}
