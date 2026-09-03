package diagram

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// repoRoot is three levels up from infra/internal/diagram.
const repoRoot = "../../.."

func writeTempGo(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// TestGoImportsUnaliasedUsesDeclaredPackageName guards the bug where an unaliased
// generated-proto import was keyed by the directory basename (v2) instead of the
// package's declared name (pingv2), silently dropping its publishers/consumers.
func TestGoImportsUnaliasedUsesDeclaredPackageName(t *testing.T) {
	path := writeTempGo(t, `package x

import "github.com/speakeasy-api/gram/infra/gen/gram/ping/v2"

var _ = pingv2.Message{}
`)

	imports, err := goImports(path, repoRoot)
	if err != nil {
		t.Fatalf("goImports: %v", err)
	}

	if got := imports["pingv2"]; got != "gram.ping.v2" {
		t.Errorf("declared package name not resolved: imports[%q] = %q, want %q", "pingv2", got, "gram.ping.v2")
	}
	if _, ok := imports["v2"]; ok {
		t.Errorf("import keyed by directory basename %q; should use declared package name", "v2")
	}
}

// TestSubscribeScansBatchVsSingle runs the production Go subscribe scans against
// a sample tree and proves each receiver shape is captured exactly once with its
// handler as the first trailing argument. It guards the ast-grep patterns
// themselves, which a Go-only test cannot. Skips when ast-grep is unavailable.
func TestSubscribeScansBatchVsSingle(t *testing.T) {
	if _, err := exec.LookPath(astGrepBin()); err != nil {
		t.Skipf("ast-grep not available: %v", err)
	}

	root := t.TempDir()
	srcDir := filepath.Join(root, "server")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	src := `package x

func reg(rg receiverGroup) {
	mustReceive(rg, pingv2.Message{}, pingv2.ProcessorSub{}, ping.NewHandler(logger))
	mustReceiveBatch(rg, riskv1.Finding{}, riskv1.FindingBQWriterSub{}, risk.NewFindingBQWriter(logger), gcp.BatchReceiveSettings{})
	mustReceiveBatchWithResult(rg, otelv1.Span{}, otelv1.SpanRelay{}, otel.NewSpanRelayHandler(logger), gcp.BatchReceiveSettings{})
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "streams.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	goScans := scansForLanguage(subscribeScans, "go")
	matches, err := runASTGrepRules(t.Context(), root, "go", goScans)
	if err != nil {
		t.Fatalf("runASTGrepRules: %v", err)
	}

	byRule := map[string][]asgMatch{}
	for _, m := range matches {
		byRule[m.RuleID] = append(byRule[m.RuleID], m)
	}

	single := byRule["subscribe-go"]
	if len(single) != 1 {
		t.Fatalf("subscribe-go matched %d sites, want 1 (only the single-message call)", len(single))
	}
	if got := single[0].meta("MSG"); got != "pingv2.Message{}" {
		t.Errorf("subscribe-go MSG = %q, want %q", got, "pingv2.Message{}")
	}

	batch := byRule["subscribe-batch-go"]
	if len(batch) != 1 {
		t.Fatalf("subscribe-batch-go matched %d sites, want 1", len(batch))
	}
	if got := batch[0].meta("SUB"); got != "riskv1.FindingBQWriterSub{}" {
		t.Errorf("subscribe-batch-go SUB = %q, want %q", got, "riskv1.FindingBQWriterSub{}")
	}
	if got := batch[0].firstMulti("HANDLER"); got != "risk.NewFindingBQWriter(logger)" {
		t.Errorf("subscribe-batch-go handler = %q, want %q", got, "risk.NewFindingBQWriter(logger)")
	}

	batchResult := byRule["subscribe-batch-result-go"]
	if len(batchResult) != 1 {
		t.Fatalf("subscribe-batch-result-go matched %d sites, want 1", len(batchResult))
	}
	if got := batchResult[0].meta("SUB"); got != "otelv1.SpanRelay{}" {
		t.Errorf("subscribe-batch-result-go SUB = %q, want %q", got, "otelv1.SpanRelay{}")
	}
	if got := batchResult[0].firstMulti("HANDLER"); got != "otel.NewSpanRelayHandler(logger)" {
		t.Errorf("subscribe-batch-result-go handler = %q, want %q", got, "otel.NewSpanRelayHandler(logger)")
	}
}

// TestGoImportsAliased confirms an explicit alias is used verbatim as the key.
func TestGoImportsAliased(t *testing.T) {
	path := writeTempGo(t, `package x

import riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"

var _ = riskv1.Finding{}
`)

	imports, err := goImports(path, repoRoot)
	if err != nil {
		t.Fatalf("goImports: %v", err)
	}

	if got := imports["riskv1"]; got != "gram.risk.v1" {
		t.Errorf("imports[%q] = %q, want %q", "riskv1", got, "gram.risk.v1")
	}
}

func TestPyImportsInlineComments(t *testing.T) {
	src := `from gram.ping.v2 import ping_pb2  # trailing comment
from gram.risk.v1 import (
    finding_pb2,  # mid-block comment must not swallow the next item
    presidio_analysis_pb2,
    reply_pb2,  # a ) inside a comment must not close the block
    presidio_enforcement_pb2,
)
from redis import asyncio as redis_asyncio
`
	path := filepath.Join(t.TempDir(), "multi.py")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	imports, err := pyImports(path)
	if err != nil {
		t.Fatalf("pyImports: %v", err)
	}

	want := map[string]string{
		"ping_pb2":                 "gram.ping.v2",
		"finding_pb2":              "gram.risk.v1",
		"presidio_analysis_pb2":    "gram.risk.v1",
		"reply_pb2":                "gram.risk.v1",
		"presidio_enforcement_pb2": "gram.risk.v1",
	}
	for name, pkg := range want {
		if got := imports[name]; got != pkg {
			t.Errorf("imports[%q] = %q, want %q", name, got, pkg)
		}
	}
	if len(imports) != len(want) {
		t.Errorf("imports has %d entries, want %d: %v", len(imports), len(want), imports)
	}
}
