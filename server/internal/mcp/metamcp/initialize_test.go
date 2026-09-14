package metamcp

import "testing"

func TestResolveInstructions(t *testing.T) {
	t.Parallel()

	empty := ""
	custom := "  Follow these instructions.\nThen list_servers.  "
	for _, tc := range []struct {
		name   string
		custom *string
		mode   string
		want   string
	}{
		{name: "unset", mode: "", want: Instructions},
		{name: "unset replace", mode: ModeReplace, want: Instructions},
		{name: "empty", custom: &empty, mode: ModeAppend, want: Instructions},
		{name: "append default", custom: &custom, mode: "", want: Instructions + "\n\n" + custom},
		{name: "append explicit", custom: &custom, mode: ModeAppend, want: Instructions + "\n\n" + custom},
		{name: "replace preserved verbatim", custom: &custom, mode: ModeReplace, want: custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ResolveInstructions(tc.custom, tc.mode); got != tc.want {
				t.Errorf("ResolveInstructions() = %q, want %q", got, tc.want)
			}
		})
	}
}
