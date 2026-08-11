package gitleaks_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

func TestPrime(t *testing.T) {
	t.Parallel()
	s := gitleaks.NewScanner()
	// Prime materializes a detector up front; a successful call means the
	// first scan is warm and any init failure surfaces here at startup.
	require.NoError(t, s.Prime())
	// Priming again warms an extra slot (not a no-op) and still succeeds; the
	// scanner remains usable afterward.
	require.NoError(t, s.Prime())
	findings, err := s.Scan(t.Context(), "hello world, this is a normal message")
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestScan_NoSecrets(t *testing.T) {
	t.Parallel()
	findings, err := gitleaks.NewScanner().Scan(t.Context(), "hello world, this is a normal message")
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestScan_DetectsAWSKey(t *testing.T) {
	t.Parallel()
	// The access key id anchors detection but is not reported on its own; the
	// secret access key is the flagged finding. ("EXAMPLE" values are globally
	// allowlisted by gitleaks, so the fixtures avoid them.)
	content := `AccessKeyId: ` + fakeAccessKeyID + `, SecretAccessKey: ` + fakeSecret
	findings, err := gitleaks.NewScanner().Scan(t.Context(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, findings, "expected at least one finding for AWS credentials")
	for _, f := range findings {
		assert.NotEmpty(t, f.RuleID)
		assert.NotEmpty(t, f.Description)
		assert.NotEqual(t, gitleaks.AccessKeyIDRuleID, f.RuleID,
			"the access key id must not be reported as a finding")
	}
}

func TestScan_DetectsGitHubToken(t *testing.T) {
	t.Parallel()
	content := `export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab`
	findings, err := gitleaks.NewScanner().Scan(t.Context(), content)
	require.NoError(t, err)
	assert.NotEmpty(t, findings, "expected at least one finding for GitHub token")
}

func TestScan_EmptyContent(t *testing.T) {
	t.Parallel()
	findings, err := gitleaks.NewScanner().Scan(t.Context(), "")
	require.NoError(t, err)
	assert.Empty(t, findings)
}

// TestScan_BytePositions verifies the line/column-to-byte-offset conversion
// lines up with actual gitleaks output: the reported StartPos:EndPos slice of
// the content must equal the finding's Match. Byte-position conversion is also
// unit-tested directly via lineColToBytePos.
func TestScan_BytePositions(t *testing.T) {
	t.Parallel()
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
		{
			// Secret on a line after the first: exercises the newline column
			// accounting. Before lineColToBytePos matched gitleaks' "first byte
			// after a newline is column 2" rule, the extracted span dropped the
			// leading byte of the token (and the Equal assertion below failed).
			name:            "secret after newline",
			content:         "clean first line here\nexport GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab",
			expectedMatches: []string{"ghp_R2D2C3POLuk3Skywalker1234567890ab"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			findings, err := gitleaks.NewScanner().Scan(t.Context(), tc.content)
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

				// Verify the match corresponds to one of the expected secrets.
				// gitleaks may report a superset of the secret value (e.g. it
				// includes the "API_KEY=" assignment prefix), so we require the
				// full expected secret to appear within the match rather than an
				// exact equality. We deliberately do not accept the match being a
				// substring of an expected secret: that reverse direction would
				// let a partial hit like "KEY" pass against "EXAMPLEKEY" and mask
				// a false positive.
				found := false
				for _, expected := range tc.expectedMatches {
					if strings.Contains(extracted, expected) {
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
