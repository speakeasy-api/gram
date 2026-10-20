package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactResourceForLog(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		resource string
		want     string
	}{
		{name: "canonical", resource: "https://gram.example/mcp/billing", want: "https://gram.example/mcp/billing"},
		{name: "userinfo", resource: "https://user:secret@gram.example/mcp/billing", want: "https://gram.example/mcp/billing"},
		{name: "query and fragment", resource: "https://gram.example/mcp/billing?token=secret#frag", want: "https://gram.example/mcp/billing"},
		{name: "unparseable", resource: "https://gram.example/%zz", want: unparseableResourceLogValue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, redactResourceForLog(tc.resource))
		})
	}
}
