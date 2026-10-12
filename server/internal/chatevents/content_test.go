package chatevents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRawText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "json string", raw: `"plain words"`, want: "plain words"},
		{name: "text blocks join with a blank line", raw: `[{"type":"text","text":"first"},{"type":"tool_use","name":"Read","input":{}},{"type":"text","text":"second"}]`, want: "first\n\nsecond"},
		{name: "blocks without text have none", raw: `[{"type":"tool_use","name":"Read","input":{}}]`, want: ""},
		{name: "other shapes have none", raw: `{"content_type":"text","parts":["hello"]}`, want: ""},
		{name: "invalid json has none", raw: `{not json`, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, rawText([]byte(tc.raw)))
		})
	}
}
