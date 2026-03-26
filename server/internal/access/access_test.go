package access

import "testing"

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "two words", in: "Project Manager", want: "org-project-manager"},
		{name: "single word", in: "Admin", want: "org-admin"},
		{name: "three words", in: "MCP Server Editor", want: "org-mcp-server-editor"},
		{name: "already slugged", in: "Already-Slugged", want: "org-already-slugged"},
		{name: "leading and trailing spaces", in: "  Spaces  ", want: "org-spaces"},
		{name: "all uppercase", in: "UPPERCASE", want: "org-uppercase"},
		{name: "special characters stripped", in: "special!@#chars", want: "org-specialchars"},
		{name: "empty string", in: "", want: ""},
		{name: "underscores become dashes", in: "hello_world", want: "org-hello-world"},
		{name: "consecutive separators collapsed", in: "a - - b", want: "org-a-b"},
		{name: "digits preserved", in: "role2admin", want: "org-role2admin"},
		{name: "trailing separator trimmed", in: "trailing-", want: "org-trailing"},
		{name: "already has org- prefix", in: "org-editor", want: "org-editor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := slugify(tt.in)
			if got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
