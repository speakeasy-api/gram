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

// LaunchCodeRunner starts a real pinned Rust runner and Monty binary on an ephemeral port.
// The dedicated integration task supplies both binaries; missing binaries fail the test.
func LaunchCodeRunner(t *testing.T) (endpoint, token string) {
	t.Helper()
	runner, monty := os.Getenv("GRAM_CODE_RUNNER_TEST_BIN"), os.Getenv("GRAM_MONTY_TEST_BIN")
	require.NotEmpty(t, runner, "run mise run test:code-mode")
	require.NotEmpty(t, monty, "run mise run test:code-mode")
	token = uuid.NewString() + uuid.NewString()
	cmd := exec.Command(runner, "--listen", "127.0.0.1:0", "--monty-bin", monty, "--capacity", "4") //nolint:gosec // Test binaries are supplied by the local build task.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "GRAM_CODE_RUNNER_TOKEN=" + token}
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
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
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
