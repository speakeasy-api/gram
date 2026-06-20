// Package gitleaks is the single home for gitleaks-based secret scanning. It
// owns the gitleaks detector lifecycle, batch/single scanning, and conversion
// of raw gitleaks findings into a neutral Detection type. Both the streams
// subscriber (this package's Handler) and the risk_analysis activity consume
// it; risk_analysis adapts Detection into its richer domain Finding.
//
// The detector and the raw gitleaks finding type are deliberately kept
// unexported: callers scan strings and receive Detections, never touching
// *detect.Detector or report.Finding.
package gitleaks

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// Source labels findings produced by this scanner. The shared Finding topic
// distinguishes detection sources by this value.
const Source = "gitleaks"

// prefixSecret is prepended to canonical gitleaks rule ids.
const prefixSecret = "secret."

// detectorInitMu serializes gitleaks detector creation process-wide. Gitleaks
// uses viper internally which has global state that races on concurrent
// initialization.
var detectorInitMu sync.Mutex

// newDetector creates a gitleaks detector using the default config, serialized
// by detectorInitMu to avoid viper's init-time data race.
func newDetector() (*detect.Detector, error) {
	detectorInitMu.Lock()
	defer detectorInitMu.Unlock()
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	return detector, nil
}

// Detection is a single secret match, kept decoupled from any domain or proto
// schema so the scanning logic stays independent of its consumers.
type Detection struct {
	RuleID      string
	Description string
	Match       string
	StartPos    int // Byte position in string
	EndPos      int // Byte position in string
	Tags        []string
	Confidence  float64 // gitleaks is rule-based, always 1.0
}

// Scanner runs gitleaks secret scanning over a batch of messages, building a
// fresh pool of detectors per call. Use it for one-shot batch work (e.g. the
// risk-analysis activity). For a long-lived scanner that reuses a single
// detector across many calls, use ReusableScanner.
type Scanner struct{}

// NewScanner creates a new Scanner.
func NewScanner() *Scanner {
	return &Scanner{}
}

// ScanBatchParallel scans multiple messages concurrently. Creates NumCPU
// detectors (serialized to avoid viper race), then fans out scanning. Each
// goroutine gets its own detector because DetectString is not safe for
// concurrent use on a single instance.
func (s *Scanner) ScanBatchParallel(messages []string) ([][]Detection, error) {
	n := len(messages)
	if n == 0 {
		return nil, nil
	}

	workers := min(runtime.NumCPU(), n)

	detectors := make([]*detect.Detector, workers)
	for i := range workers {
		d, err := newDetector()
		if err != nil {
			return nil, fmt.Errorf("create gitleaks detector %d: %w", i, err)
		}
		detectors[i] = d
	}

	results := make([][]Detection, n)

	ch := make(chan int, n)
	for i := range n {
		ch <- i
	}
	close(ch)

	var wg sync.WaitGroup
	for i := range workers {
		d := detectors[i]
		wg.Go(func() {
			for idx := range ch {
				findings := d.DetectString(messages[idx])
				results[idx] = convertFindings(messages[idx], findings)
			}
		})
	}

	wg.Wait()
	return results, nil
}

// Scan scans a single message with a fresh detector. Safe for tests and
// low-throughput, one-off use.
func (s *Scanner) Scan(content string) ([]Detection, error) {
	d, err := newDetector()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	return convertFindings(content, d.DetectString(content)), nil
}

// ReusableScanner wraps a single pre-created gitleaks detector for callers that
// scan many messages over a long lifetime (the streams Handler, the real-time
// enforcement path). Creating the detector compiles every rule, so reusing one
// avoids that cost per scan. DetectString mutates detector state and is not
// concurrency-safe, so Scan serializes calls; ReusableScanner is therefore safe
// for concurrent use.
type ReusableScanner struct {
	mu  sync.Mutex
	det *detect.Detector
}

// NewReusableScanner pre-creates the detector and returns a scanner ready for
// repeated, concurrent Scan calls.
func NewReusableScanner() (*ReusableScanner, error) {
	det, err := newDetector()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	return &ReusableScanner{mu: sync.Mutex{}, det: det}, nil
}

// Scan scans a single message on the reused detector under the mutex.
func (r *ReusableScanner) Scan(content string) []Detection {
	r.mu.Lock()
	raw := r.det.DetectString(content)
	r.mu.Unlock()
	return convertFindings(content, raw)
}

// convertFindings converts raw gitleaks findings to detections. Rule ids are
// canonicalized to the shared snake_case-with-dots form and line/column
// positions are mapped to byte offsets. Gitleaks ships human-readable
// descriptions that never echo the matched secret, so they pass through.
func convertFindings(content string, raw []report.Finding) []Detection {
	out := make([]Detection, 0, len(raw))
	for _, f := range raw {
		startPos := lineColToBytePos(content, f.StartLine, f.StartColumn)
		endPos := min(lineColToBytePos(content, f.EndLine, f.EndColumn)+1, len(content))
		out = append(out, Detection{
			RuleID:      CanonicalRuleID(f.RuleID),
			Description: f.Description,
			Match:       f.Match,
			StartPos:    startPos,
			EndPos:      endPos,
			Tags:        parseTags(f.Tags),
			Confidence:  1.0,
		})
	}
	return out
}

// CanonicalRuleID prepends the `secret.` prefix to a gitleaks rule id and
// converts its kebab-case to snake_case so the result conforms to the
// canonical rule-id grammar shared across scanners.
func CanonicalRuleID(raw string) string {
	return prefixSecret + strings.ReplaceAll(strings.ToLower(raw), "-", "_")
}

// lineColToBytePos converts line and column numbers to a byte position in
// content. Gitleaks columns are byte offsets from the start of the line
// (1-indexed for StartColumn, 0-indexed for EndColumn) — see
// github.com/zricethezav/gitleaks/v8/detect/location.go. We iterate by byte
// (not rune) to match gitleaks' accounting.
func lineColToBytePos(content string, line, col int) int {
	if line < 0 || col <= 0 {
		return 0
	}

	currentLine := 0
	currentCol := 1

	for i := 0; i < len(content); i++ {
		if currentLine == line && currentCol == col {
			return i
		}

		if content[i] == '\n' {
			currentLine++
			currentCol = 1
		} else {
			currentCol++
		}
	}

	return len(content)
}

func parseTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	var out []string
	for _, t := range tags {
		for part := range strings.SplitSeq(t, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
