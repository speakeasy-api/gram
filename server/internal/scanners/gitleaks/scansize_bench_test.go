package gitleaks

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The benchmarks below measure how gitleaks scan cost grows with input size,
// and how that cost changes when one large input is scanned as a sequence of
// overlapping chunks instead of a single string. They back the scan-coverage
// ceilings recorded in docs/research/2026-09-23-large-input-scan-coverage.md;
// re-run them before changing a chunk size or a per-path byte budget.

const benchSecret = "export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab\n"

// benchFiller returns n bytes of keyword-free JSON-ish log lines, the shape of
// a large tool response that carries no secret material.
func benchFiller(n int) string {
	var b strings.Builder
	line := `{"ts":"2026-09-23T12:00:00Z","level":"info","msg":"fetched record","id":"c1a2b3","rows":42}` + "\n"
	for b.Len() < n {
		b.WriteString(line)
	}
	return b.String()[:n]
}

// benchKeywordFiller returns n bytes where every line trips the keyword trie,
// so each rule regex actually runs. This is the worst case for scan cost.
func benchKeywordFiller(n int) string {
	var b strings.Builder
	line := `api_key = "value"; token = "abc"; secret = "xyz"; password = "pw"; client_id = "0"` + "\n"
	for b.Len() < n {
		b.WriteString(line)
	}
	return b.String()[:n]
}

func benchSizes() []int {
	return []int{4 << 10, 50 << 10, 256 << 10, 1 << 20, 4 << 20, 10 << 20}
}

// BenchmarkScanBySize sweeps a single DetectString pass over growing inputs.
// Per-byte cost is flat, so the interesting comparison is against
// BenchmarkScanChunked at the same total size.
func BenchmarkScanBySize(b *testing.B) {
	for _, size := range benchSizes() {
		content := benchFiller(size-len(benchSecret)) + benchSecret
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			s := newBenchScanner(b)
			b.SetBytes(int64(size))
			b.ResetTimer()
			for b.Loop() {
				if _, err := s.Scan(context.Background(), content); err != nil {
					b.Fatalf("scan: %v", err)
				}
			}
		})
	}
}

// BenchmarkScanKeywordDense is the same sweep over content that hits the
// keyword trie on every line.
func BenchmarkScanKeywordDense(b *testing.B) {
	for _, size := range []int{50 << 10, 1 << 20} {
		content := benchKeywordFiller(size)
		b.Run(fmt.Sprintf("bytes=%d", size), func(b *testing.B) {
			s := newBenchScanner(b)
			b.SetBytes(int64(size))
			b.ResetTimer()
			for b.Loop() {
				if _, err := s.Scan(context.Background(), content); err != nil {
					b.Fatalf("scan: %v", err)
				}
			}
		})
	}
}

// BenchmarkScanChunked scans one large input as overlapping chunks. Compare
// against BenchmarkScanBySize at the same total size: chunking lowers total
// CPU as well as wall clock. Gitleaks gates each rule on a keyword trie pass
// over the whole fragment, so one keyword anywhere in a large input runs that
// rule's regex across all of it; chunking confines the regex to the chunk that
// carries the keyword.
func BenchmarkScanChunked(b *testing.B) {
	const overlap = 4 << 10
	for _, size := range []int{1 << 20, 10 << 20} {
		content := benchFiller(size-len(benchSecret)) + benchSecret
		for _, chunk := range []int{64 << 10, 256 << 10} {
			pieces := chunkWithOverlap(content, chunk, overlap)
			b.Run(fmt.Sprintf("bytes=%d/chunk=%d/serial", size, chunk), func(b *testing.B) {
				s := newBenchScanner(b)
				b.SetBytes(int64(size))
				b.ResetTimer()
				for b.Loop() {
					for _, piece := range pieces {
						if _, err := s.Scan(context.Background(), piece); err != nil {
							b.Fatalf("scan: %v", err)
						}
					}
				}
			})
			b.Run(fmt.Sprintf("bytes=%d/chunk=%d/parallel", size, chunk), func(b *testing.B) {
				s := newBenchScanner(b)
				b.SetBytes(int64(size))
				b.ResetTimer()
				for b.Loop() {
					if _, err := s.ScanBatch(context.Background(), pieces); err != nil {
						b.Fatalf("scan batch: %v", err)
					}
				}
			})
		}
	}
}

func newBenchScanner(b *testing.B) *Scanner {
	b.Helper()
	s := NewScanner()
	if err := s.Prime(); err != nil {
		b.Fatalf("prime: %v", err)
	}
	if _, err := s.Scan(context.Background(), benchSecret); err != nil {
		b.Fatalf("warm: %v", err)
	}
	return s
}

// TestChunkOverlapCoversBoundarySecret pins the property any chunked scan has
// to preserve: a secret split across a chunk boundary is missed without
// overlap and found with it. It guards the overlap requirement recorded in the
// scan-coverage decision, ahead of a chunked scanner existing in production.
func TestChunkOverlapCoversBoundarySecret(t *testing.T) {
	t.Parallel()

	s := NewScanner()
	ctx := context.Background()

	const chunk = 64 << 10
	// Split the secret across the first chunk boundary.
	content := benchFiller(chunk-len(benchSecret)/2) + benchSecret + benchFiller(32<<10)

	countFindings := func(pieces []string) int {
		total := 0
		for _, piece := range pieces {
			result, err := s.Scan(ctx, piece)
			if err != nil {
				t.Fatalf("scan chunk: %v", err)
			}
			total += len(result.Findings)
		}
		return total
	}

	whole, err := s.Scan(ctx, content)
	if err != nil {
		t.Fatalf("scan whole: %v", err)
	}
	if len(whole.Findings) != 1 {
		t.Fatalf("single pass: expected 1 finding, got %d", len(whole.Findings))
	}
	if got := countFindings(chunkWithOverlap(content, chunk, 0)); got != 0 {
		t.Fatalf("chunked without overlap: expected the boundary secret to be missed, got %d findings", got)
	}
	if got := countFindings(chunkWithOverlap(content, chunk, 4<<10)); got != 1 {
		t.Fatalf("chunked with overlap: expected 1 finding, got %d", got)
	}
}

// chunkWithOverlap splits s into chunks of size bytes that each repeat the
// previous chunk's trailing overlap bytes, so a match straddling a boundary is
// wholly contained in at least one chunk.
func chunkWithOverlap(s string, size, overlap int) []string {
	if size <= overlap {
		panic("chunk size must exceed overlap")
	}
	var out []string
	for start := 0; start < len(s); start += size - overlap {
		end := min(start+size, len(s))
		out = append(out, s[start:end])
		if end == len(s) {
			break
		}
	}
	return out
}
