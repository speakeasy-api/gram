//go:build fpsplit

// fp_split regenerates testdata/fp-ip.txt from a one-IP-per-line input
// (typically produced from a production risk_events dump via
// `jq -r '.match' | sort -u`). Every IP that the production catalog
// (`nonPIIIPReason`) recognises as a false positive is written to
// testdata/fp-ip.txt, sorted and deduped, one per line. The remainder
// is printed to t.Log so the catalog author can scan what would still
// flow through as PII.
//
// Build-tagged `fpsplit` so it stays out of normal `go test`. Run with:
//
//	go test -tags fpsplit -run TestFPSplit -v \
//	    -fp.input /tmp/risk_unique_ips.txt \
//	    ./server/internal/risk/presidiofp/
package presidiofp

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var fpSplitInput = flag.String("fp.input", "/tmp/risk_unique_ips.txt",
	"one-IP-per-line input file (absolute path)")

func TestFPSplit(t *testing.T) {
	in, err := os.Open(*fpSplitInput)
	if err != nil {
		t.Fatalf("open input %q: %v", *fpSplitInput, err)
	}
	defer func() { _ = in.Close() }()

	outPath := filepath.Join("testdata", "fp-ip.txt")
	var fp []string
	var real []string
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		ip := strings.TrimSpace(sc.Text())
		if ip == "" || strings.HasPrefix(ip, "#") {
			continue
		}
		if nonPIIIPReason(ip) != "" {
			fp = append(fp, ip)
			continue
		}
		real = append(real, ip)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	sort.Strings(fp)
	fp = dedupe(fp)
	sort.Strings(real)
	real = dedupe(real)

	out, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create %q: %v", outPath, err)
	}
	defer func() { _ = out.Close() }()
	w := bufio.NewWriter(out)
	defer func() { _ = w.Flush() }()
	for _, ip := range fp {
		if _, err := fmt.Fprintln(w, ip); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	t.Logf("input:  %s", *fpSplitInput)
	t.Logf("fp-ip:  %s (%d ips)", outPath, len(fp))
	t.Logf("real:   %d ips not in catalog (printed below; spot-check before promoting):", len(real))
	for _, ip := range real {
		t.Logf("  %s", ip)
	}
}

func dedupe(xs []string) []string {
	if len(xs) == 0 {
		return xs
	}
	out := xs[:1]
	for _, x := range xs[1:] {
		if x != out[len(out)-1] {
			out = append(out, x)
		}
	}
	return out
}
