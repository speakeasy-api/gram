package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisteredTemplates_AllHaveTransactionalIDs(t *testing.T) {
	t.Parallel()

	require.NotEmpty(t, RegisteredTemplates, "RegisteredTemplates should not be empty")

	for _, tmpl := range RegisteredTemplates {
		require.NotEmpty(t, tmpl.TransactionalID(),
			"template %T must have a non-empty TransactionalID — register an ID in templates.go",
			tmpl,
		)
	}
}

func TestRegisteredTemplates_TransactionalIDsAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[TransactionalID]Template, len(RegisteredTemplates))
	for _, tmpl := range RegisteredTemplates {
		id := tmpl.TransactionalID()
		existing, dup := seen[id]
		require.Falsef(t, dup,
			"templates %T and %T share transactional ID %q",
			existing, tmpl, id,
		)
		seen[id] = tmpl
	}
}

func TestTeamInvite_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDTeamInvite, TeamInvite{}.TransactionalID())
}

func TestTeamInvite_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := TeamInvite{
		InviteLink:    "https://app.gram.sh/invite?token=abc",
		InviterName:   "Alice",
		InviterEmail:  "alice@example.com",
		WorkspaceName: "Acme Inc",
	}

	require.Equal(t, map[string]string{
		"invite_link":    "https://app.gram.sh/invite?token=abc",
		"teammate_fn":    "Alice",
		"teammate_email": "alice@example.com",
		"workspace_name": "Acme Inc",
	}, tmpl.Variables())
}

func TestTeamInvite_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	tmpl := TeamInvite{
		InviteLink:    "",
		InviterName:   "",
		InviterEmail:  "",
		WorkspaceName: "",
	}

	vars := tmpl.Variables()
	require.Len(t, vars, 4, "all merge keys should still be present even if values are empty")
	require.Empty(t, vars["invite_link"])
	require.Empty(t, vars["teammate_fn"])
	require.Empty(t, vars["teammate_email"])
	require.Empty(t, vars["workspace_name"])
}

func TestTeamInvite_AddToAudience(t *testing.T) {
	t.Parallel()

	require.True(t, TeamInvite{}.AddToAudience(),
		"team invite recipients should be added to the Loops audience by default",
	)
}
