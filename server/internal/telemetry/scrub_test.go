package telemetry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// The tool IO scrub deletes gen_ai.tool.call.arguments, but ClickHouse
// materializes skill_name from that JSON — Skill rows keep the minimal
// {"skill": name} and nothing else.
func TestScrubbedSkillArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		toolName string
		raw      string
		want     string
	}{
		{
			name:     "skill row keeps only the skill name",
			toolName: "Skill",
			raw:      `{"skill":"repo-review","args":"sensitive free text"}`,
			want:     `{"skill":"repo-review"}`,
		},
		{
			name:     "non-skill rows keep nothing",
			toolName: "Bash",
			raw:      `{"skill":"repo-review"}`,
			want:     "",
		},
		{
			name:     "unparsable arguments keep nothing",
			toolName: "Skill",
			raw:      `not-json`,
			want:     "",
		},
		{
			name:     "empty skill keeps nothing",
			toolName: "Skill",
			raw:      `{"skill":"   "}`,
			want:     "",
		},
		{
			name:     "implausibly long names keep nothing",
			toolName: "Skill",
			raw:      `{"skill":"` + strings.Repeat("x", maxScrubbedSkillNameLen+1) + `"}`,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			attrs := map[attr.Key]any{
				attr.ToolNameKey:               tt.toolName,
				attr.GenAIToolCallArgumentsKey: tt.raw,
			}
			require.Equal(t, tt.want, scrubbedSkillArguments(attrs))
		})
	}
}
