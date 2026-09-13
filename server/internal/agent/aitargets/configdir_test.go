package aitargets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
)

func TestMatchConfigDir(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		signature string
		path      string
		want      bool
	}{
		{"literal matches itself", "/home/dev/.claude", "/home/dev/.claude", true},
		{"literal rejects a sibling", "/home/dev/.claude", "/home/dev/.codex", false},

		// The case globs exist for: a version-stamped extension directory.
		{
			"trailing wildcard spans a version",
			"/home/dev/.vscode/extensions/saoudrizwan.claude-dev-*",
			"/home/dev/.vscode/extensions/saoudrizwan.claude-dev-3.42.1",
			true,
		},
		{
			"trailing wildcard still requires its literal prefix",
			"/home/dev/.vscode/extensions/saoudrizwan.claude-dev-*",
			"/home/dev/.vscode/extensions/continue.continue-1.2.7",
			false,
		},
		{
			"globbed root covers the remote and insiders trees",
			"/home/dev/.vscode*/extensions/continue.continue-*",
			"/home/dev/.vscode-server/extensions/continue.continue-1.2.7",
			true,
		},

		// The containment rule: a wildcard widens a name, never the depth.
		{
			"a wildcard never crosses a separator",
			"/home/dev/.vscode/extensions/*",
			"/home/dev/.vscode/extensions/cline/nested",
			false,
		},
		{
			"segment counts must agree",
			"/home/dev/.claude",
			"/home/dev/.claude/projects",
			false,
		},

		{"interior wildcard", "/home/dev/.a*z", "/home/dev/.abcz", true},
		{"interior wildcard rejects a missing suffix", "/home/dev/.a*z", "/home/dev/.abc", false},
		{"wildcard matches empty", "/home/dev/.claude*", "/home/dev/.claude", true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, aitargets.MatchConfigDir(testCase.signature, testCase.path))
		})
	}
}

// No test here pins a rejected glob shape. #6289 removed config-dir shape
// validation on purpose — the agent only checks whether a directory exists,
// and three copies of the rule were three places to keep in step. A wildcard
// is the one shape that arguably reintroduces a reason to validate, because
// a bare "~/*" makes every device enumerate a home directory; that is called
// out in the PR rather than decided here.
