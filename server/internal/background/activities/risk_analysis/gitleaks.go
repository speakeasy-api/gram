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
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
	Tags        []string
}

// ScanWithGitleaks scans the given content string using a fresh gitleaks
// detector and returns any findings. Each call creates its own detector
// so it is safe for concurrent use.
func ScanWithGitleaks(content string) ([]Finding, error) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}

	findings := d.DetectString(content)
	return convertFindings(findings), nil
}

// messageResult holds the scan output for a single message.
type messageResult struct {
	index    int
	findings []Finding
	err      error
}

// ScanBatchParallel scans multiple messages concurrently using a worker pool.
// Detectors are created sequentially (gitleaks uses global viper state that
// races on concurrent init), then used in parallel for scanning.
func ScanBatchParallel(messages []string) ([][]Finding, error) {
	n := len(messages)
	if n == 0 {
		return nil, nil
	}

	workers := min(runtime.NumCPU(), n)

	// Create detectors sequentially — viper's global config map isn't
	// safe for concurrent reads during NewDetectorDefaultConfig().
	detectors := make([]*detect.Detector, workers)
	for i := range workers {
		d, err := detect.NewDetectorDefaultConfig()
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
	for i, d := range detectors {
		_ = i
		d := d
		wg.Go(func() {
			for idx := range ch {
				findings := d.DetectString(messages[idx])
				results[idx] = convertFindings(findings)
			}
		})
	}

	wg.Wait()
	return results, nil
}

func convertFindings(raw []report.Finding) []Finding {
	out := make([]Finding, 0, len(raw))
	for _, f := range raw {
		tags := parseTags(f.Tags)
		out = append(out, Finding{
			RuleID:      f.RuleID,
			Description: f.Description,
			Match:       f.Match,
			StartLine:   f.StartLine,
			StartColumn: f.StartColumn,
			EndLine:     f.EndLine,
			EndColumn:   f.EndColumn,
			Tags:        tags,
		})
	}
	return out
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
