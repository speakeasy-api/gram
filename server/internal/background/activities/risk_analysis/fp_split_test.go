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
//	    ./server/internal/background/activities/risk_analysis/
package risk_analysis

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

	if err := writeLines(outPath, fp); err != nil {
		t.Fatalf("write fp-ip: %v", err)
	}

	// Residual IPs are real production user data, so we never log them or
	// check them into the repo: write to a sibling file under /tmp so the
	// operator can inspect locally and discard.
	residualPath := filepath.Join(os.TempDir(), "fp_split_residual.txt")
	if err := writeLines(residualPath, real); err != nil {
		t.Fatalf("write residual: %v", err)
	}

	t.Logf("input:    %s", *fpSplitInput)
	t.Logf("fp-ip:    %s (%d ips)", outPath, len(fp))
	t.Logf("residual: %s (%d ips, not logged)", residualPath, len(real))
}

func writeLines(path string, lines []string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	for _, line := range lines {
		if _, werr := fmt.Fprintln(w, line); werr != nil {
			return werr
		}
	}
	return w.Flush()
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
