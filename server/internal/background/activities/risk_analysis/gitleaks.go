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

// DetectorPool holds pre-created gitleaks detectors for concurrent scanning.
// Detectors must be created sequentially because gitleaks uses viper which
// has global state that races on concurrent initialization.
type DetectorPool struct {
	detectors []*detect.Detector
}

// NewDetectorPool creates a pool of gitleaks detectors. Must be called from
// a single goroutine (e.g., at startup).
func NewDetectorPool() (*DetectorPool, error) {
	n := runtime.NumCPU()
	detectors := make([]*detect.Detector, n)
	for i := range n {
		d, err := detect.NewDetectorDefaultConfig()
		if err != nil {
			return nil, fmt.Errorf("create gitleaks detector %d: %w", i, err)
		}
		detectors[i] = d
	}
	return &DetectorPool{detectors: detectors}, nil
}

// ScanBatch scans multiple messages concurrently using the pre-created detectors.
func (p *DetectorPool) ScanBatch(messages []string) [][]Finding {
	n := len(messages)
	if n == 0 {
		return nil
	}

	results := make([][]Finding, n)
	workers := min(len(p.detectors), n)

	ch := make(chan int, n)
	for i := range n {
		ch <- i
	}
	close(ch)

	var wg sync.WaitGroup
	for i := range workers {
		d := p.detectors[i]
		wg.Go(func() {
			for idx := range ch {
				findings := d.DetectString(messages[idx])
				results[idx] = convertFindings(findings)
			}
		})
	}

	wg.Wait()
	return results
}

// ScanWithGitleaks scans a single message. Creates a fresh detector each time
// so it's safe for use in tests but not ideal for production hot paths.
func ScanWithGitleaks(content string) ([]Finding, error) {
	d, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}
	findings := d.DetectString(content)
	return convertFindings(findings), nil
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
