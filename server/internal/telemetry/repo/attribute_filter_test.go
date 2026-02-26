package repo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidJSONPath_AcceptsValidPaths(t *testing.T) {
	t.Parallel()

	valid := []string{
		"env",
		"http.route",
		"user.region",
		"gen_ai.response.model",
		"_private.field",
		"a.b.c.d.e",
		"CamelCase.Path",
		"@user.region",
		"@env",
		"@app_name.version",
	}
	for _, p := range valid {
		require.True(t, validJSONPath.MatchString(p), "expected valid: %q", p)
	}
}

func TestValidJSONPath_RejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"1starts.with.digit",
		".leading.dot",
		"path with spaces",
		"semi;colon",
		"slash/path",
		"@@double.at",
		"quote\"mark",
		"back`tick",
		"paren(open",
		"bracket[0]",
	}
	for _, p := range invalid {
		require.False(t, validJSONPath.MatchString(p), "expected invalid: %q", p)
	}
}

func TestResolveAttributeFilterPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"@user.region", "app.user.region"},
		{"@env", "app.env"},
		{"@my_app.version", "app.my_app.version"},
		{"http.route", "http.route"},
		{"gen_ai.response.model", "gen_ai.response.model"},
		{"env", "env"},
	}
	for _, tt := range tests {
		path := tt.input
		if len(path) > 0 && path[0] == '@' {
			path = "app." + path[1:]
		}
		require.Equal(t, tt.expected, path, "input: %q", tt.input)
	}
}
