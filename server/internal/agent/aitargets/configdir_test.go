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

		// A Windows agent reports a native path. Its separator has to become
		// `/` or the whole path is one segment and a wildcard stops being
		// bounded by directories.
		{
			"a windows native path is matched by a signature authored with slashes",
			"C:/Users/dev/.claude",
			`C:\Users\dev\.claude`,
			true,
		},
		{
			"a wildcard does not cross a windows separator either",
			"C:/Users/dev/*",
			`C:\Users\dev\.vscode\extensions`,
			false,
		},
		// A UNC path converts to a doubled leading separator, which splits
		// into one more segment than the signature does after cleaning. Both
		// sides have to be cleaned the same way or no share path can match.
		{
			"a unc share path is matched by a signature authored with slashes",
			"//host/share/dev/.claude",
			`\\host\share\dev\.claude`,
			true,
		},
		{
			"a wildcard on a unc share stays inside its segment",
			"//host/share/dev/*",
			`\\host\share\dev\.vscode\extensions`,
			false,
		},

		// ...but only a Windows path. `\` is a legal character in a Unix
		// filename, so canonicalizing it away there both loses matches a
		// wildcard should make and makes two unrelated directories look like
		// the same one.
		{
			"a backslash in a unix name stays inside its segment",
			"/home/dev/*",
			`/home/dev/we\ird`,
			true,
		},
		{
			"a unix name holding a backslash is not the nested path it resembles",
			"/home/dev/we/ird",
			`/home/dev/we\ird`,
			false,
		},

		// A config dir is stored exactly as it was written, trailing separator
		// and all, so matching has to tolerate what a person would type.
		{"a trailing separator on a signature is not a segment", "/home/dev/.claude/", "/home/dev/.claude", true},
		{"a doubled separator in a signature is not a segment", "/home//dev/.claude", "/home/dev/.claude", true},
		{
			"a trailing separator does not loosen the signature",
			"/home/dev/.claude/",
			"/home/dev/.claude/projects",
			false,
		},
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
