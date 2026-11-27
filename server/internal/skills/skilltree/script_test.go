package skilltree_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func TestIsScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want bool
	}{
		{path: "scripts/run.py", want: true},
		{path: "scripts/nested/run", want: true},
		{path: "references/scripts/run.py", want: false},
		{path: "scripts", want: false},
		{path: "scriptsx/run.py", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, skilltree.IsScript(tc.path))
		})
	}
}

func TestIsPackagedScript(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want bool
	}{
		{path: "acme/skills/pdf/scripts/run.py", want: true},
		{path: "skills/pdf/scripts/nested/run", want: true},
		{path: "acme/skills/pdf/references/run.py", want: false},
		{path: "acme/skills/pdf/scripts", want: false},
		{path: "acme/skills/scripts/run.py", want: false},
		{path: "acme/hooks/pdf/scripts/run.py", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, skilltree.IsPackagedScript(tc.path))
		})
	}
}
