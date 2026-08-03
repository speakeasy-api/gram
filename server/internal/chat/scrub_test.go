package chat

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestRedactSensitiveKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "redacts a credential no scanner would recognize",
			content: `{"user":"adam","password":"hunter2"}`,
			want:    `{"user":"adam","password":"[redacted]"}`,
		},
		{
			name:    "matches key names case-insensitively and by substring",
			content: `{"X-Api-Key":"abc","authToken":"def"}`,
			want:    `{"X-Api-Key":"[redacted]","authToken":"[redacted]"}`,
		},
		{
			name:    "leaves descriptive values alone",
			content: `{"query":"deploy failed","repo":"gram"}`,
			want:    `{"query":"deploy failed","repo":"gram"}`,
		},
		{
			name:    "handles escaped quotes inside a redacted value",
			content: `{"secret":"a\"b","note":"keep"}`,
			want:    `{"secret":"[redacted]","note":"keep"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, redactSensitiveKeys(tt.content))
		})
	}
}

func TestRedactFindings(t *testing.T) {
	t.Parallel()

	const content = `{"a":"secret-one","b":"secret-two"}`

	tests := []struct {
		name     string
		findings []scanners.Finding
		want     string
	}{
		{
			name:     "no findings passes content through",
			findings: nil,
			want:     content,
		},
		{
			name: "redacts each span",
			findings: []scanners.Finding{
				{StartPos: 6, EndPos: 16},
				{StartPos: 23, EndPos: 33},
			},
			want: `{"a":"[redacted]","b":"[redacted]"}`,
		},
		{
			name: "handles out-of-order findings",
			findings: []scanners.Finding{
				{StartPos: 23, EndPos: 33},
				{StartPos: 6, EndPos: 16},
			},
			want: `{"a":"[redacted]","b":"[redacted]"}`,
		},
		{
			name: "merges overlapping spans from different rules",
			findings: []scanners.Finding{
				{StartPos: 6, EndPos: 16},
				{StartPos: 12, EndPos: 23},
			},
			want: `{"a":"[redacted]secret-two"}`,
		},
		{
			name: "clamps spans that run past the content",
			findings: []scanners.Finding{
				{StartPos: 23, EndPos: 9999},
			},
			want: `{"a":"secret-one","b":"[redacted]`,
		},
		{
			name: "ignores empty and negative spans",
			findings: []scanners.Finding{
				{StartPos: 5, EndPos: 5},
				{StartPos: -3, EndPos: -1},
			},
			want: content,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, redactFindings(content, tt.findings))
		})
	}
}
