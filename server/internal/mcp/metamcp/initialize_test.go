package metamcp

import "testing"

func TestResolveInstructions(t *testing.T) {
	t.Parallel()

	empty := ""
	custom := "  Follow these instructions.\nThen list_servers.  "
	for _, tc := range []struct {
		name   string
		custom *string
		want   string
	}{
		{name: "unset", want: Instructions},
		{name: "empty", custom: &empty, want: Instructions},
		{name: "custom preserved verbatim", custom: &custom, want: custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveInstructions(tc.custom); got != tc.want {
				t.Errorf("ResolveInstructions() = %q, want %q", got, tc.want)
			}
		})
	}
}
