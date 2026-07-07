package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessRequestCreated_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDAccessRequestCreated, AccessRequestCreated{}.TransactionalID())
}

func TestAccessRequestCreated_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := AccessRequestCreated{
		RequesterEmail: "alice@example.com",
		DisplayName:    "GitHub MCP",
		ApprovalURL:    "https://app.gram.sh/security/ApprovalRequests",
	}

	require.Equal(t, map[string]string{
		"requester_email": "alice@example.com",
		"display_name":    "GitHub MCP",
		"approval_url":    "https://app.gram.sh/security/ApprovalRequests",
	}, tmpl.Variables())
}

func TestAccessRequestCreated_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	vars := AccessRequestCreated{}.Variables()
	require.Len(t, vars, 3, "all three merge keys must be present even when empty")
}

func TestAccessRequestCreated_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, AccessRequestCreated{}.AddToAudience(),
		"admin alerts should not add recipients to the Loops audience")
}
