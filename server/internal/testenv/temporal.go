package testenv

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/durationpb"

	servertemporal "github.com/speakeasy-api/gram/server/internal/temporal"
)

// A single dev server is shared by every test in a package, and each test
// registers its own namespace and task queues against it. Left at its
// defaults the server sizes itself for a production host and its footprint
// grows with the number of namespaces, so it is bounded here from two sides:
// the Go runtime knobs below and the dynamic config in devServerArgs.
const (
	// GOMEMLIMIT is a soft heap ceiling the GC actively works to stay under.
	// It is the only real cap available, since the dev server is a plain
	// subprocess with no cgroup of its own. It must stay above the live set:
	// a limit below it does not bound memory, it just makes the GC run
	// continuously trying to reach a target it cannot hit. 512MiB measured as
	// too tight for a full package run and cost ~70% more dev server CPU.
	devServerMemLimit = "1GiB"
	// Per-P allocator caches and GC workers scale with core count. The dev
	// server is infrastructure for the test, not the thing under test, and
	// should not claim the whole host alongside a `-race` test binary.
	devServerMaxProcs = "2"
)

// devServerArgs bounds the dev server's caches by size rather than by entry
// count, which is what the defaults do.
var devServerArgs = []string{
	// The history cache is count-bounded by default (128k mutable state
	// entries host-wide), which puts no ceiling on bytes. Switching it to a
	// size-based limit is what makes GOMEMLIMIT reachable instead of a source
	// of continuous GC. Server 1.31 dropped the shard-level cache, so
	// hostLevelCacheMaxSizeBytes is the remaining size knob.
	//
	// These are sized for a whole package's worth of concurrent namespaces,
	// not for one workflow: too small and every workflow task misses the cache
	// and rebuilds mutable state from the event history, which costs more CPU
	// than the memory it saves.
	"--dynamic-config-value", "history.cacheSizeBasedLimit=true",
	"--dynamic-config-value", "history.hostLevelCacheMaxSizeBytes=134217728",
	"--dynamic-config-value", "history.eventsCacheMaxSizeBytes=8388608",
	"--dynamic-config-value", `history.cacheTTL="1m"`,
	"--dynamic-config-value", `history.eventsCacheTTL="1m"`,
	// Every test registers a namespace and stands up workers on four task
	// queues, and each partition carries its own manager and buffers. A test
	// worker has no use for the default four partitions per queue, so this
	// cuts the per-test matching overhead fourfold.
	"--dynamic-config-value", "matching.numTaskqueueReadPartitions=1",
	"--dynamic-config-value", "matching.numTaskqueueWritePartitions=1",
}

func nextRandom() string {
	return uuid.NewString()
}

// newTemporalShim writes a shell shim that runs the temporal binary with the
// Go runtime limits above applied. The SDK launches the dev server as a child
// process and never sets cmd.Env, so the child inherits the test process's
// environment verbatim; a shim is the only way to give the child variables the
// test process itself must not have. `exec` replaces the shell, so the dev
// server keeps the PID that DevServer.Stop signals.
func newTemporalShim(dir string) (string, error) {
	exe, err := exec.LookPath("temporal")
	if err != nil {
		return "", fmt.Errorf("locate temporal binary: %w", err)
	}

	script := fmt.Sprintf(
		"#!/bin/sh\nexport GOMEMLIMIT=%s\nexport GOMAXPROCS=%s\nexec '%s' \"$@\"\n",
		devServerMemLimit,
		devServerMaxProcs,
		strings.ReplaceAll(exe, "'", `'\''`),
	)

	// Owner-only: the exec bit is the point of the file, and nothing outside
	// this test process ever runs it. The containing directory is MkdirTemp's
	// 0700 as well.
	path := filepath.Join(dir, "temporal-shim")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write temporal shim: %w", err)
	}

	return path, nil
}

func NewTemporalDevServer(ctx context.Context) (*testsuite.DevServer, func() error, error) {
	var stdout io.Writer
	var stderr io.Writer
	if !isTestingVerbose() {
		stdout = io.Discard
		stderr = io.Discard
	}

	shimDir, err := os.MkdirTemp("", "gram-temporal-*")
	if err != nil {
		return nil, nil, fmt.Errorf("create temporal shim dir: %w", err)
	}
	cleanup := func() error {
		if err := os.RemoveAll(shimDir); err != nil {
			return fmt.Errorf("remove temporal shim dir: %w", err)
		}
		return nil
	}

	shim, err := newTemporalShim(shimDir)
	if err != nil {
		return nil, cleanup, err
	}

	var devserver *testsuite.DevServer
	logger := NewLogger(nil)

	for range 5 {
		devserver, err = testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
			LogLevel:     "error",
			ExistingPath: shim,
			ClientOptions: &client.Options{
				Namespace: "default",
				Logger:    logger,
			},
			ExtraArgs: devServerArgs,
			Stdout:    stdout,
			Stderr:    stderr,
		})
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, cleanup, fmt.Errorf("start temporal dev server: %w", err)
	}

	return devserver, cleanup, nil
}

func NewTemporalEnvironment(t *testing.T, devserver *testsuite.DevServer) (*servertemporal.Environment, error) {
	t.Helper()

	namespace := fmt.Sprintf("test_%s", nextRandom())
	queue := fmt.Sprintf("main_%s", nextRandom())

	request := new(workflowservice.RegisterNamespaceRequest)
	request.Namespace = namespace
	request.WorkflowExecutionRetentionPeriod = durationpb.New(24 * time.Hour)

	_, err := devserver.Client().WorkflowService().RegisterNamespace(t.Context(), request)
	if err != nil {
		return nil, fmt.Errorf("register temporal namespace: %w", err)
	}

	clientOptions := client.Options{}
	clientOptions.HostPort = devserver.FrontendHostPort()
	clientOptions.Namespace = namespace
	clientOptions.Logger = NewLogger(t)

	temporalClient, err := client.DialContext(t.Context(), clientOptions)
	if err != nil {
		return nil, fmt.Errorf("dial temporal client: %w", err)
	}

	t.Cleanup(func() {
		temporalClient.Close()
	})

	return servertemporal.NewEnvironment(temporalClient, servertemporal.NamespaceName(namespace), servertemporal.TaskQueueName(queue)), nil
}
