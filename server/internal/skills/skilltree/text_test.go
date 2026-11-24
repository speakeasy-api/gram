package skilltree_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

func TestIsText(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		contentType string
		content     []byte
		want        bool
	}{
		{name: "markdown", contentType: "text/markdown; charset=utf-8", content: []byte("# hi"), want: true},
		{name: "yaml", contentType: "application/yaml", content: []byte("a: 1"), want: true},
		{name: "json", contentType: "application/json", content: []byte("{}"), want: true},
		{name: "png", contentType: "image/png", content: []byte{0x89, 'P', 'N', 'G'}, want: false},
		{name: "text type with invalid utf-8", contentType: "text/plain", content: []byte{0xff, 0xfe}, want: false},
		{name: "octet stream", contentType: "application/octet-stream", content: []byte("abc"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, skilltree.IsText(tc.contentType, tc.content))
		})
	}
}
