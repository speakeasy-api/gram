package risk_analysis

import (
	"fmt"
	"strings"

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

// ScanWithGitleaks scans the given content string using gitleaks default rules
// and returns any findings.
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
	// Flatten comma-separated tags.
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
