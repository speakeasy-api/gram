package risk_analysis

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/gitleaks"
)

// GitleaksScanner is a long-lived, concurrency-safe gitleaks scanner for the
// real-time enforcement path. It reuses a single detector (avoiding per-scan
// rule compilation) and returns domain Findings, keeping the gitleaks detector
// and raw finding types out of the caller's sight.
type GitleaksScanner struct {
	inner *gitleaks.ReusableScanner
}

// NewGitleaksScanner pre-creates the underlying gitleaks scanner.
func NewGitleaksScanner() (*GitleaksScanner, error) {
	inner, err := gitleaks.NewReusableScanner()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks scanner: %w", err)
	}
	return &GitleaksScanner{inner: inner}, nil
}

// Scan scans a single message and returns domain Findings.
func (s *GitleaksScanner) Scan(text string) []Finding {
	return fromDetections(s.inner.Scan(text))
}

// ScanWithGitleaks scans a single message and returns domain Findings.
func ScanWithGitleaks(content string) ([]Finding, error) {
	detections, err := gitleaks.NewScanner().Scan(content)
	if err != nil {
		return nil, fmt.Errorf("scan with gitleaks: %w", err)
	}
	return fromDetections(detections), nil
}

// fromDetections maps gitleaks detections onto domain Findings.
func fromDetections(detections []gitleaks.Detection) []Finding {
	out := make([]Finding, 0, len(detections))
	for _, d := range detections {
		out = append(out, fromDetection(d))
	}
	return out
}

// fromDetection maps a single gitleaks detection onto a domain Finding. The
// detection's rule id is already canonical; guard re-validates the format in
// dev/test so writer drift is caught early.
func fromDetection(d gitleaks.Detection) Finding {
	return Finding{
		RuleID:              guard(d.RuleID),
		Description:         d.Description,
		Match:               d.Match,
		StartPos:            d.StartPos,
		EndPos:              d.EndPos,
		Tags:                d.Tags,
		Source:              SourceGitleaks,
		Stage:               StageHeuristic,
		Confidence:          d.Confidence,
		DeadLetterReason:    "",
		mcpLookupToolCallID: "",
		spanGroupKey:        "",
		field:               "",
		path:                "",
	}
}
