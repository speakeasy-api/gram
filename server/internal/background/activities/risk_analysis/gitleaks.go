package risk_analysis

import (
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/gitleaks"
)

// Finding represents a single secret or sensitive data match found in a message.
// DeadLetterReason is populated only on synthetic "could not analyze" markers
// emitted when a scanner exhausts its retry budget for a message; it is empty
// on every real finding and is not considered by dedup/overlap logic.
type Finding struct {
	RuleID           string
	Description      string
	Match            string
	StartPos         int // Byte position in string
	EndPos           int // Byte position in string
	Tags             []string
	Source           string  // Detection source (e.g. "gitleaks", "presidio")
	Confidence       float64 // 0.0-1.0 confidence score
	DeadLetterReason string  // Non-empty => dead-letter sentinel, not a real finding
	// toolCallID is an internal correlation key used by scanShadowMCP to
	// patch the resolved MCP server identifier onto findings via the
	// telemetry CH lookup. Not persisted — converters that map Finding
	// into repo.InsertRiskResultParams ignore it.
	toolCallID string
}

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
		RuleID:           guard(d.RuleID),
		Description:      d.Description,
		Match:            d.Match,
		StartPos:         d.StartPos,
		EndPos:           d.EndPos,
		Tags:             d.Tags,
		Source:           gitleaks.Source,
		Confidence:       d.Confidence,
		DeadLetterReason: "",
		toolCallID:       "",
	}
}
