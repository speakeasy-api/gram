//go:build codemodeintegration

package testenv

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// codeRunnerStopGrace is how long cleanup waits for the runner to drain after
// SIGTERM. Tests close their clients first, so a healthy runner exits at once;
// one still running then is stuck and is killed with its workers.
const codeRunnerStopGrace = 5 * time.Second

// LaunchCodeRunner starts a real pinned Rust runner and Monty binary on an ephemeral port.
// The dedicated integration task supplies both binaries; missing binaries fail the test.
func LaunchCodeRunner(t *testing.T) (endpoint, token string) {
	t.Helper()
	runner, monty := os.Getenv("GRAM_CODE_RUNNER_TEST_BIN"), os.Getenv("GRAM_MONTY_TEST_BIN")
	require.NotEmpty(t, runner, "run mise run test:code-mode")
	require.NotEmpty(t, monty, "run mise run test:code-mode")
	token = uuid.NewString() + uuid.NewString()
	cmd := exec.Command(runner, "--listen", "127.0.0.1:0", "--monty-bin", monty, "--capacity", "12") //nolint:gosec // Test binaries are supplied by the local build task.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GRAM_CODE_RUNNER_TOKEN=" + token}
	// Monty workers join the runner's process group, so a forced stop reaches
	// them too and no orphan keeps the stderr pipe open. The group is not the
	// terminal's, so an interrupt that kills the test binary before cleanup
	// would not reach it; guardParentDeath stops the runner then where the
	// platform supports it (Linux), and only a local macOS run can leak it.
	cmd.SysProcAttr = new(syscall.SysProcAttr)
	cmd.SysProcAttr.Setpgid = true
	guardParentDeath(cmd.SysProcAttr)
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	ready := make(chan string, 1)
	done := make(chan error, 1)
	require.NoError(t, cmd.Start())
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if address, ok := strings.CutPrefix(scanner.Text(), "gram-code-runner protocol 1 listening on "); ok {
				ready <- address
			}
		}
		done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(codeRunnerStopGrace):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	})
	select {
	case address := <-ready:
		return "http://" + address, token
	case err := <-done:
		done <- err
		t.Fatalf("code runner exited during startup: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("code runner startup timed out")
	}
	return "", ""
}
