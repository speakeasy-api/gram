package risk_analysis

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// Finding represents a single secret or sensitive data match found in a message.
type Finding struct {
	RuleID      string
	Description string
	Match       string
	StartPos    int // Byte position in string
	EndPos      int // Byte position in string
	Tags        []string
	Source      string  // Detection source (e.g. "gitleaks", "presidio")
	Confidence  float64 // 0.0-1.0 confidence score
}

// detectorInitMu serializes gitleaks detector creation process-wide.
// Gitleaks uses viper internally which has global state that races on
// concurrent initialization. Must be package-level because multiple Scanner
// instances may exist in the same process.
var detectorInitMu sync.Mutex

// Scanner wraps the gitleaks detector for secret scanning. Scanning
// (DetectString) is safe for concurrent use on separate detector instances,
// but NOT on the same instance — each goroutine must use its own detector.
type Scanner struct{}

// NewScanner creates a new Scanner.
func NewScanner() *Scanner {
	return &Scanner{}
}

// newDetector creates a gitleaks detector, serialized by detectorInitMu.
func (s *Scanner) newDetector() (*detect.Detector, error) {
	return NewDetector()
}

// NewDetector creates a gitleaks detector using the package-level mutex
// to serialize viper initialization. Use this from any package that needs
// to construct a long-lived detector outside the Scanner abstraction.
func NewDetector() (*detect.Detector, error) {
	detectorInitMu.Lock()
	defer detectorInitMu.Unlock()
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	return detector, nil
}

// sharedDetectorOnce / sharedDetector cache a single process-wide detector
// for callers that want one long-lived instance (e.g. the real-time hook
// path). Batch fan-out paths still build their own pool of detectors so
// scans can run in parallel.
var (
	sharedDetectorOnce sync.Once
	sharedDetector     *detect.Detector
	errSharedDetector  error
)

// SharedDetector returns a process-wide cached gitleaks detector. The
// detector is created lazily on first call (under detectorInitMu) and
// reused across all callers. Callers must serialize their own DetectString
// calls because the detector mutates internal state on every call.
func SharedDetector() (*detect.Detector, error) {
	sharedDetectorOnce.Do(func() {
		sharedDetector, errSharedDetector = NewDetector()
	})
	return sharedDetector, errSharedDetector
}

// ScanBatchParallel scans multiple messages concurrently. Creates NumCPU
// detectors (serialized to avoid viper race), then fans out scanning.
// Each goroutine gets its own detector because DetectString is not safe
// for concurrent use on a single instance.
func (s *Scanner) ScanBatchParallel(messages []string) ([][]Finding, error) {
	n := len(messages)
	if n == 0 {
		return nil, nil
	}

	workers := min(runtime.NumCPU(), n)

	detectors := make([]*detect.Detector, workers)
	for i := range workers {
		d, err := s.newDetector()
		if err != nil {
			return nil, fmt.Errorf("create gitleaks detector %d: %w", i, err)
		}
		detectors[i] = d
	}

	results := make([][]Finding, n)

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
				results[idx] = ConvertFindings(messages[idx], findings)
			}
		})
	}

	wg.Wait()
	return results, nil
}

// Scan scans a single message. Safe for tests and low-throughput use.
func (s *Scanner) Scan(content string) ([]Finding, error) {
	d, err := s.newDetector()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	findings := d.DetectString(content)
	return ConvertFindings(content, findings), nil
}

// ScanWithGitleaks is a package-level convenience for scanning a single message.
func ScanWithGitleaks(content string) ([]Finding, error) {
	return NewScanner().Scan(content)
}

// ConvertFindings converts raw gitleaks findings to the internal Finding type.
func ConvertFindings(content string, raw []report.Finding) []Finding {
	out := make([]Finding, 0, len(raw))
	for _, f := range raw {
		tags := parseTags(f.Tags)
		startPos := lineColToBytePos(content, f.StartLine, f.StartColumn)
		endPos := min(lineColToBytePos(content, f.EndLine, f.EndColumn)+1, len(content))
		out = append(out, Finding{
			RuleID:      f.RuleID,
			Description: f.Description,
			Match:       f.Match,
			StartPos:    startPos,
			EndPos:      endPos,
			Tags:        tags,
			Source:      "gitleaks",
			Confidence:  1.0,
		})
	}
	return out
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
