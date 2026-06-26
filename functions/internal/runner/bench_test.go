package runner

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/gram/functions/internal/javascript"
)

// echoBundle is a representative minimal user function: it does no module-level
// work, so its per-call cost is dominated by node process startup + entrypoint
// import. It models the floor of cold-start latency.
const echoBundle = `
export async function handleToolCall({ name, input }) {
  return new Response(JSON.stringify({ ok: true, name, input }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
`

// heavyBundle models a realistic bundled SDK: meaningful module-level evaluation
// (class hierarchy, compiled regexes, a registry) that runs on every import. Its
// per-call cost adds import/init work on top of node startup.
const heavyBundle = `
class Base { constructor() { this.t = 0; } validate(v) { return typeof v; } }
const REGISTRY = new Map();
for (let i = 0; i < 1500; i++) {
  const re = new RegExp("^[a-z0-9_]+" + i + "$", "i");
  REGISTRY.set("k" + i, { re, fn: (a) => a * 2 });
}
export async function handleToolCall({ name, input }) {
  let n = 0;
  for (const [, v] of REGISTRY) { if (v.re.test(name)) n++; }
  return new Response(JSON.stringify({ ok: true, name, n, size: REGISTRY.size }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
`

// benchService wires a runner Service against a real node subprocess executing
// the given user bundle. executeRequest is exercised directly so the benchmark
// measures raw spawn + execute cost without the auth middleware or limiter.
func benchService(tb testing.TB, userCode string) *Service {
	tb.Helper()

	if _, err := exec.LookPath("node"); err != nil {
		tb.Skip("node not installed; skipping runner benchmark")
	}

	dir := tb.TempDir()
	entry := filepath.Join(dir, "gram-start.js")
	if err := os.WriteFile(entry, javascript.Entrypoint, 0600); err != nil {
		tb.Fatalf("write entrypoint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "functions.js"), []byte(userCode), 0600); err != nil {
		tb.Fatalf("write functions.js: %v", err)
	}

	return &Service{
		logger:         slog.New(slog.DiscardHandler),
		encryption:     nil,
		workDir:        dir,
		command:        "node",
		args:           []string{"--experimental-strip-types", entry},
		maxConcurrency: 0,
		slots:          nil,
		inFlight:       atomic.Int64{},
		holdTimeout:    0,
		retryAfter:     0,
	}
}

func benchToolRequest(tb testing.TB) callRequest {
	tb.Helper()

	reqArg, err := json.Marshal(CallToolPayload{
		ToolName:    "bench_tool",
		Input:       json.RawMessage(`{"hello":"world"}`),
		Environment: nil,
	})
	if err != nil {
		tb.Fatalf("marshal request: %v", err)
	}

	return callRequest{
		requestArg:  reqArg,
		environment: map[string]string{"GRAM_USER_EMAIL": "bench@example.com"},
		requestType: "tool",
	}
}

// benchExecute drives b.N tool calls through executeRequest at a fixed
// concurrency. Comparing ns/op across concurrency levels reveals where added
// parallelism stops paying off (latency climbs super-linearly): that knee is the
// machine's real execution capacity N for the runtime, which seeds
// executionSlots in the server's deploy_fly concurrency sizing.
func benchExecute(b *testing.B, workers int, userCode string) {
	b.Helper()

	s := benchService(b, userCode)
	req := benchToolRequest(b)

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()

			rec := httptest.NewRecorder()
			if err := s.executeRequest(b.Context(), s.logger, req, rec); err != nil {
				b.Errorf("executeRequest: %v", err)
				return
			}
			if rec.Code != 200 {
				b.Errorf("unexpected status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}

	wg.Wait()
	b.StopTimer()
}

// Concurrency sweep for the echo (cold-start floor) bundle. Run with:
//
//	go test ./internal/runner -bench 'BenchmarkToolCallEcho' -benchmem
//
// then read the concurrency at which ns/op degrades to set N.
func BenchmarkToolCallEcho_C1(b *testing.B)  { benchExecute(b, 1, echoBundle) }
func BenchmarkToolCallEcho_C4(b *testing.B)  { benchExecute(b, 4, echoBundle) }
func BenchmarkToolCallEcho_C8(b *testing.B)  { benchExecute(b, 8, echoBundle) }
func BenchmarkToolCallEcho_C16(b *testing.B) { benchExecute(b, 16, echoBundle) }
func BenchmarkToolCallEcho_C32(b *testing.B) { benchExecute(b, 32, echoBundle) }

// Concurrency sweep for the heavy (import + module-init) bundle. The knee here
// sits at a lower concurrency than echo because each call also burns CPU on
// import, so this is the more conservative input to N.
func BenchmarkToolCallHeavy_C1(b *testing.B)  { benchExecute(b, 1, heavyBundle) }
func BenchmarkToolCallHeavy_C4(b *testing.B)  { benchExecute(b, 4, heavyBundle) }
func BenchmarkToolCallHeavy_C8(b *testing.B)  { benchExecute(b, 8, heavyBundle) }
func BenchmarkToolCallHeavy_C16(b *testing.B) { benchExecute(b, 16, heavyBundle) }
