package github

import "testing"

// TestValidGitHubUsername guards the regex used by AddCollaborator before
// inserting the username into the request URL. url.JoinPath resolves ../
// segments, so anything other than a strict GitHub-format username could
// redirect the PUT to a different API endpoint.
func TestValidGitHubUsername(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		username string
		want     bool
	}{
		{"simple", "octocat", true},
		{"with hyphen", "bradcypert-sk", true},
		{"alphanumeric", "user123", true},
		{"max length 39", "abcdefghijklmnopqrstuvwxyz0123456789-ab", true},

		{"empty", "", false},
		{"path traversal dotdot", "../../actions/permissions", false},
		{"path traversal relative", "bad/../../actions", false},
		{"contains slash", "foo/bar", false},
		{"contains dot", "user.name", false},
		{"leading hyphen", "-foo", false},
		{"contains space", "foo bar", false},
		{"url-encoded slash", "user%2fbad", false},
		{"too long 40", "abcdefghijklmnopqrstuvwxyz0123456789-abc", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := validGitHubUsername.MatchString(tc.username)
			if got != tc.want {
				t.Fatalf("validGitHubUsername(%q) = %v, want %v", tc.username, got, tc.want)
			}
		})
	}
}
