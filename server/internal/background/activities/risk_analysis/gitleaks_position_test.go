package risk_analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLineColToBytePos(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		line    int
		col     int
		want    int
	}{
		{
			name:    "first character",
			content: "hello world",
			line:    0,
			col:     1,
			want:    0,
		},
		{
			name:    "middle of first line",
			content: "hello world",
			line:    0,
			col:     7,
			want:    6,
		},
		{
			name:    "end of first line",
			content: "hello world",
			line:    0,
			col:     12,
			want:    11,
		},
		{
			name:    "second line start",
			content: "hello\nworld",
			line:    1,
			col:     1,
			want:    6,
		},
		{
			name:    "second line middle",
			content: "hello\nworld",
			line:    1,
			col:     3,
			want:    8,
		},
		{
			name:    "multi-line with various lengths",
			content: "first line\nsecond\nthird line here",
			line:    2,
			col:     7,
			want:    24,
		},
		{
			name:    "empty lines",
			content: "first\n\n\nfourth",
			line:    3,
			col:     1,
			want:    8,
		},
		{
			name:    "beyond end of content",
			content: "hello",
			line:    0,
			col:     10,
			want:    5,
		},
		{
			name:    "invalid line",
			content: "hello",
			line:    -1,
			col:     1,
			want:    0,
		},
		{
			name:    "invalid column",
			content: "hello",
			line:    0,
			col:     0,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := lineColToBytePos(tt.content, tt.line, tt.col)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLineColToBytePosWithActualSecrets(t *testing.T) {
	t.Parallel()
	// Test with actual secret patterns that gitleaks would detect
	tests := []struct {
		name        string
		content     string
		matchString string
		startLine   int
		startCol    int
		endLine     int
		endCol      int
	}{
		{
			name:        "AWS key in single line",
			content:     `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`,
			matchString: "AKIAIOSFODNN7EXAMPLE",
			startLine:   0,
			startCol:    19,
			endLine:     0,
			endCol:      39,
		},
		{
			name: "API key in JSON",
			content: `{
  "api_key": "sk-proj-1234567890abcdef",
  "other": "value"
}`,
			matchString: "sk-proj-1234567890abcdef",
			startLine:   1,
			startCol:    15,
			endLine:     1,
			endCol:      39,
		},
		{
			name: "Database URL spanning line",
			content: `const db =
  "postgresql://user:password123@localhost/db";`,
			matchString: "password123",
			startLine:   1,
			startCol:    22,
			endLine:     1,
			endCol:      33,
		},
		{
			name: "GitHub token in config",
			content: `# Configuration
github_token: ghp_1234567890abcdefghijklmnopqrstuvwxyz
environment: production`,
			matchString: "ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			startLine:   1,
			startCol:    15,
			endLine:     1,
			endCol:      55,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Calculate byte positions
			startPos := lineColToBytePos(tt.content, tt.startLine, tt.startCol)
			endPos := lineColToBytePos(tt.content, tt.endLine, tt.endCol)

			// Extract the substring using byte positions
			extracted := tt.content[startPos:endPos]

			// Verify we extracted the correct match
			assert.Equal(t, tt.matchString, extracted,
				"Extracted string should match the expected secret")

			// Also verify the positions are correct
			assert.Contains(t, tt.content, tt.matchString,
				"Content should contain the match string")
			assert.Equal(t, startPos, strings.Index(tt.content, tt.matchString),
				"Start position should match string index")
		})
	}
}

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
			findings, err := ScanWithGitleaks(tc.content)
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

func TestBytePosEdgeCases(t *testing.T) {
	t.Parallel()
	// Test edge cases with Unicode and special characters
	tests := []struct {
		name    string
		content string
		line    int
		col     int
	}{
		{
			name:    "Unicode characters",
			content: "Hello 世界\nNext line",
			line:    0,
			col:     7, // Position of 世
		},
		{
			name:    "Emoji in content",
			content: "🔒 API_KEY=secret123 🚀",
			line:    0,
			col:     4, // After emoji and space
		},
		{
			name:    "Multiple emojis",
			content: "Line 1 ✅\n🔑 token=abc123def\n❌ Invalid",
			line:    1,
			col:     9, // Position of 'a' in abc123def
		},
		{
			name:    "Tabs and spaces",
			content: "\t\tAPI_KEY=secret123",
			line:    0,
			col:     11, // Position of 's' in secret
		},
		{
			name:    "Windows line endings",
			content: "First line\r\nSecond line",
			line:    1,
			col:     1,
		},
		{
			name:    "Mixed emoji and secrets",
			content: "⚠️ WARNING: password=SuperSecret!123 🛑",
			line:    0,
			col:     22, // Position of 'S' in SuperSecret
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := lineColToBytePos(tt.content, tt.line, tt.col)
			// Just ensure it doesn't panic and returns a valid position
			assert.True(t, pos >= 0 && pos <= len(tt.content))
		})
	}
}
