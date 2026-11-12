package authz

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// CoverageDirEnv names the directory test runs write authorization coverage
// records to. When it is set, every decision appends the server methods on
// the call stack to a file there; ci/cmd/authzcoverage turns those files into
// the endpoint coverage report. Production never sets it.
const CoverageDirEnv = "GRAM_AUTHZ_COVERAGE_DIR"

func init() {
	dir := os.Getenv(CoverageDirEnv) //nolint:forbidigo // test instrumentation switch; testenv cannot install it because authz tests import testenv
	if dir == "" {
		return
	}
	recorder, err := newCoverageRecorder(dir)
	if err != nil {
		// A test run that asked for coverage and cannot record it must not
		// pass silently with an empty report.
		panic(fmt.Sprintf("authz coverage: %v", err))
	}
	decisionObserver = recorder.observe
}

// decisionObserver, when set, is told about every decision. Only the coverage
// recorder sets it, once, at init.
var decisionObserver func(ctx context.Context, operation DecisionOperation, mode DecisionMode)

// serviceMethodFrame matches an exported method on a pointer receiver in a
// server package, such as
// github.com/speakeasy-api/gram/server/internal/skills.(*Service).ListSkills.
// Goa services are implemented this way, so any frame of this shape may be the
// endpoint the decision was made for.
var serviceMethodFrame = regexp.MustCompile(`^github\.com/speakeasy-api/gram/server/internal/[A-Za-z0-9_/]+\.\(\*[A-Za-z0-9_]+\)\.[A-Z][A-Za-z0-9_]*$`)

type coverageRecorder struct {
	mu   sync.Mutex
	seen map[string]struct{}
	file *os.File
}

func newCoverageRecorder(dir string) (*coverageRecorder, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	file, err := os.CreateTemp(dir, fmt.Sprintf("authz-coverage-%d-*.txt", os.Getpid()))
	if err != nil {
		return nil, fmt.Errorf("create record file: %w", err)
	}
	return &coverageRecorder{mu: sync.Mutex{}, seen: make(map[string]struct{}), file: file}, nil
}

// observe writes each new server method on the stack to the record file. A
// decision counts for every such method, because a handler may decide in a
// helper method several frames below the endpoint.
func (c *coverageRecorder) observe(_ context.Context, _ DecisionOperation, _ DecisionMode) {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var fresh []string
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		frame, more := frames.Next()
		name := frame.Function
		if serviceMethodFrame.MatchString(name) && !strings.HasPrefix(name, "github.com/speakeasy-api/gram/server/internal/authz.") {
			if _, ok := c.seen[name]; !ok {
				c.seen[name] = struct{}{}
				fresh = append(fresh, name)
			}
		}
		if !more {
			break
		}
	}
	if len(fresh) == 0 {
		return
	}
	// Each process writes its own file under the lock, so records never
	// interleave. A failed write would drop coverage and turn a covered
	// endpoint into a false report, so it stops the run like a failed setup.
	if _, err := c.file.WriteString(strings.Join(fresh, "\n") + "\n"); err != nil {
		panic(fmt.Sprintf("authz coverage: write record: %v", err))
	}
	if err := c.file.Sync(); err != nil {
		panic(fmt.Sprintf("authz coverage: sync record: %v", err))
	}
}
