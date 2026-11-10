package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSuggestWorkspaceName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		email       string
		displayName string
		want        string
	}{
		{name: "work domain", email: "jane@acme.com", displayName: "Jane Doe", want: "Acme"},
		{name: "work subdomain", email: "jane@eng.acme.co.uk", displayName: "Jane Doe", want: "Acme"},
		{name: "free mail uses name", email: "jane@gmail.com", displayName: "Jane Doe", want: "Jane Doe's organization"},
		{name: "free mail without name uses mailbox", email: "jane.doe@outlook.com", displayName: "", want: "jane.doe's organization"},
		{name: "nothing usable", email: "", displayName: "", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, suggestWorkspaceName(tc.email, tc.displayName))
		})
	}
}
