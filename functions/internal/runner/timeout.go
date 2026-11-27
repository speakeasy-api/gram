package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/svc"
)

const (
	// defaultExecutionTimeout applies when a request names no timeout, which
	// is what a server built before per-organization timeouts sends.
	defaultExecutionTimeout = 5 * time.Minute

	// maxExecutionTimeout caps whatever a request asks for. The server
	// validates the organization's setting against the same ceiling; this is
	// the runner's own backstop.
	maxExecutionTimeout = 10 * time.Minute
)

// executionTimeout turns a request's timeout_seconds into the budget one
// invocation gets. Zero or negative means the request named none.
func executionTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultExecutionTimeout
	}
	if seconds >= int(maxExecutionTimeout/time.Second) {
		return maxExecutionTimeout
	}
	return time.Duration(seconds) * time.Second
}

// timeoutError reports an invocation that ran out of its own budget, as
// opposed to one whose caller went away. It returns nil in every other case so
// the caller keeps its original error.
func timeoutError(ctx, timeoutCtx context.Context, limit time.Duration) error {
	if ctx.Err() != nil || !errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
		return nil
	}
	return svc.NewTimeoutError(
		fmt.Errorf("function exceeded its time limit of %s", limit),
		http.StatusRequestTimeout,
	)
}

// openFIFOReader opens the response FIFO for reading in its own goroutine,
// because the open blocks until the subprocess opens the other end. Opening
// with O_NONBLOCK instead would race the subprocess and read a premature EOF.
func openFIFOReader(fifoPath string) (<-chan *os.File, <-chan error) {
	pipech := make(chan *os.File, 1)
	errch := make(chan error, 1)
	go func() {
		resultReader, err := os.OpenFile(filepath.Clean(fifoPath), os.O_RDONLY, os.ModeNamedPipe)
		if err != nil {
			errch <- err
			return
		}
		pipech <- resultReader
	}()
	return pipech, errch
}

// fifoReleaseBudget bounds how long releaseFIFOReader waits for the reader
// goroutine to reach its open call.
const fifoReleaseBudget = time.Second

// releaseFIFOReader unblocks a reader from openFIFOReader that the subprocess
// will never meet, for example because it timed out or crashed first. Opening
// the write end without blocking completes the reader's open; both ends are
// then closed. It returns at once if the reader already finished, and reports
// whether the reader goroutine has finished.
func releaseFIFOReader(fifoPath string, pipech <-chan *os.File, errch <-chan error) bool {
	deadline := time.Now().Add(fifoReleaseBudget)
	for {
		select {
		case f := <-pipech:
			_ = f.Close()
			return true
		case openErr := <-errch:
			// The reader failed on its own; there is nothing left to release.
			_ = openErr
			return true
		default:
		}

		writer, err := os.OpenFile(filepath.Clean(fifoPath), os.O_WRONLY|syscall.O_NONBLOCK, os.ModeNamedPipe)
		if err == nil {
			_ = writer.Close()
			select {
			case f := <-pipech:
				_ = f.Close()
			case openErr := <-errch:
				_ = openErr
			}
			return true
		}
		// ENXIO means no reader has the FIFO open yet: the goroutine has not
		// reached its open call. Anything else leaves nothing to release.
		if !errors.Is(err, syscall.ENXIO) || time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}
