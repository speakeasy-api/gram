package risk_analysis_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

// Byte-position conversion is unit-tested in the gitleaks package
// (lineColToBytePos). This integration test verifies the conversion lines up
// with actual gitleaks output as surfaced through the risk_analysis adapter.
func TestScanWithGitleaksIntegration(t *testing.T) {
	t.Parallel()
	// Test that our byte position conversion works correctly with actual gitleaks output
	testCases := []struct {
		name            string
		content         string
		expectedMatches []string
	}{
		{
			name:            "AWS credentials",
			content:         "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			expectedMatches: []string{"wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},
		},
		{
			name: "Multiple secrets",
			content: `export API_KEY=sk-proj-abc123def456
database_url="postgresql://admin:SuperSecret123!@db.example.com/prod"
GITHUB_TOKEN=ghp_1234567890abcdefghijklmnopqrstuvwxyz`,
			expectedMatches: []string{
				"sk-proj-abc123def456",
				"SuperSecret123!",
				"ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings, err := risk_analysis.ScanWithGitleaks(tc.content)
			require.NoError(t, err)

			// For each finding, verify we can extract the correct match using byte positions
			for _, finding := range findings {
				if finding.StartPos < 0 || finding.EndPos > len(tc.content) {
					t.Errorf("Invalid byte positions: start=%d, end=%d, content_len=%d",
						finding.StartPos, finding.EndPos, len(tc.content))
					continue
				}

				extracted := tc.content[finding.StartPos:finding.EndPos]
				assert.Equal(t, finding.Match, extracted,
					"Byte positions should extract the exact match")

				// Verify the match is in our expected list
				found := false
				for _, expected := range tc.expectedMatches {
					if strings.Contains(extracted, expected) || strings.Contains(expected, extracted) {
						found = true
						break
					}
				}
				assert.True(t, found,
					"Extracted match '%s' should be in expected matches", extracted)
			}
		})
	}
}
