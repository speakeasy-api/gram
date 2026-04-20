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
	tests := []struct {
		name    string
		content string
		line    int
		col     int
	}{
		{
			name:    "Tabs and spaces",
			content: "\t\tAPI_KEY=secret123",
			line:    0,
			col:     11,
		},
		{
			name:    "Windows line endings",
			content: "First line\r\nSecond line",
			line:    1,
			col:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pos := lineColToBytePos(tt.content, tt.line, tt.col)
			assert.True(t, pos >= 0 && pos <= len(tt.content))
		})
	}
}

func TestLineColToBytePos_MultiByte(t *testing.T) {
	t.Parallel()
	// Gitleaks columns are byte offsets, not rune offsets.
	// 🔒 is 4 bytes (U+1F512). With byte-based columns, the character
	// after "🔒 " starts at byte 5, not rune 3.
	tests := []struct {
		name    string
		content string
		line    int
		col     int
		want    int
	}{
		{
			name:    "after 4-byte emoji",
			content: "🔒 SECRET=abc",
			line:    0,
			col:     6, // byte 5 = 'S', col is 1-indexed so col 6
			want:    5, // "🔒" = 4 bytes, " " = 1 byte
		},
		{
			name:    "CJK characters are 3 bytes each",
			content: "世界KEY=val",
			line:    0,
			col:     7, // byte 6 = 'K', col 7
			want:    6, // "世" = 3 bytes, "界" = 3 bytes
		},
		{
			name:    "emoji on second line before secret",
			content: "line1\n🔑 token=abc123",
			line:    1,
			col:     10, // byte offset from line start: 🔑(4) + " "(1) + "token"(5) = 10, col 10
			want:    15, // 6 (line1\n) + 4 (🔑) + 1 ( ) + 5 (token) - 1 = 15
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

func TestScanWithGitleaks_MultiByteBeforeSecret(t *testing.T) {
	t.Parallel()
	// Verify that multi-byte characters before a secret don't cause
	// incorrect byte positions when extracting the match.
	content := "🔒 GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab"
	findings, err := ScanWithGitleaks(content)
	require.NoError(t, err)
	require.NotEmpty(t, findings, "should detect GitHub token after emoji")

	for _, f := range findings {
		require.True(t, f.StartPos >= 0 && f.EndPos <= len(content),
			"positions must be within content bounds")
		extracted := content[f.StartPos:f.EndPos]
		assert.Equal(t, f.Match, extracted,
			"byte positions must extract the exact match even with multi-byte prefix")
	}
}
