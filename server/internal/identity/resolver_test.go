package identity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The identifier shapes that reach expansion are a Gram user id or an address,
// and telling them apart decides which set of rows the fold matches. A bare
// "@" test would send a malformed value down the email path.
func TestIsEmailIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		identifier string
		want       bool
	}{
		{identifier: "dev@example.com", want: true},
		{identifier: "Dev@Example.com", want: true},
		{identifier: "user_01HXYZ", want: false},
		{identifier: "", want: false},
		{identifier: "@", want: false},
		{identifier: "a@b@c", want: false},
		{identifier: "dev@", want: false},
		{identifier: "Dev User <dev@example.com>", want: false},
		{identifier: `"dev user"@example.com`, want: false},
		{identifier: " dev@example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, isEmailIdentifier(tt.identifier))
		})
	}
}

// An empty identifier expands to an empty subject, which every filter built
// from it reads as "match everything". That is a caller bug, not a subject.
func TestExpandIdentifier_EmptyIsAnError(t *testing.T) {
	t.Parallel()

	resolver := &Resolver{logger: nil, users: nil, hooks: nil, directory: nil}

	subject, err := resolver.ExpandIdentifier(context.Background(), "org_1", "")
	require.Error(t, err)
	require.Empty(t, subject.UserIDs)
	require.Empty(t, subject.Emails)
}
