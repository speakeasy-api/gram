package risk_analysis

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// detectorInitMu serializes calls to NewDetectorDefaultConfig because
// gitleaks uses viper internally which has global state that races on
// concurrent initialization. Scanning (DetectString) is safe for
// concurrent use on separate detector instances.
var detectorInitMu sync.Mutex

// Finding represents a single secret or sensitive data match found in a message.
type Finding struct {
	RuleID      string
	Description string
	Match       string
	StartPos    int // Byte position in string
	EndPos      int // Byte position in string
	Tags        []string
}

// newDetector creates a gitleaks detector, serialized by detectorInitMu.
func newDetector() (*detect.Detector, error) {
	detectorInitMu.Lock()
	defer detectorInitMu.Unlock()
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	return detector, nil
}

// ScanBatchParallel scans multiple messages concurrently. Creates NumCPU
// detectors (serialized to avoid viper race), then fans out scanning.
func ScanBatchParallel(messages []string) ([][]Finding, error) {
	n := len(messages)
	if n == 0 {
		return nil, nil
	}

	workers := min(runtime.NumCPU(), n)

	// Create detectors one at a time (serialized by mutex).
	detectors := make([]*detect.Detector, workers)
	for i := range workers {
		d, err := newDetector()
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
				results[idx] = convertFindings(messages[idx], findings)
			}
		})
	}

	wg.Wait()
	return results, nil
}

// ScanWithGitleaks scans a single message. Safe for tests and low-throughput use.
func ScanWithGitleaks(content string) ([]Finding, error) {
	d, err := newDetector()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	findings := d.DetectString(content)
	return convertFindings(content, findings), nil
}

func convertFindings(content string, raw []report.Finding) []Finding {
	out := make([]Finding, 0, len(raw))
	for _, f := range raw {
		tags := parseTags(f.Tags)
		// Calculate byte positions from line/column
		startPos := lineColToBytePos(content, f.StartLine, f.StartColumn)
		endPos := min(lineColToBytePos(content, f.EndLine, f.EndColumn)+1, len(content))
		out = append(out, Finding{
			RuleID:      f.RuleID,
			Description: f.Description,
			Match:       f.Match,
			StartPos:    startPos,
			EndPos:      endPos,
			Tags:        tags,
		})
	}
	return out
}

// lineColToBytePos converts line and column numbers to byte position in string.
// Gitleaks uses 0-indexed lines and 0-indexed columns for StartLine/EndLine
// and 1-indexed columns for StartColumn/EndColumn. We treat lines as 0-indexed
// and columns as 1-indexed here.
func lineColToBytePos(content string, line, col int) int {
	if line < 0 || col <= 0 {
		return 0
	}

	currentLine := 0
	currentCol := 1

	for i, ch := range content {
		if currentLine == line && currentCol == col {
			return i
		}

		if ch == '\n' {
			currentLine++
			currentCol = 1
		} else {
			currentCol++
		}
	}

	// If we reach here, the position is beyond the end of content
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
