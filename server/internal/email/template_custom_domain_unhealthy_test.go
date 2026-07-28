package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomDomainUnhealthy_TransactionalID(t *testing.T) {
	t.Parallel()

	require.Equal(t, transactionalIDCustomDomainUnhealthy, CustomDomainUnhealthy{
		Email:        "",
		Domain:       "",
		IssueMessage: "",
		DomainLink:   "",
	}.TransactionalID())
}

func TestCustomDomainUnhealthy_Variables_RendersExpectedKeys(t *testing.T) {
	t.Parallel()

	tmpl := CustomDomainUnhealthy{
		Email:        "admin@example.com",
		Domain:       "mcp.example.com",
		IssueMessage: "The TLS certificate for the domain has expired.",
		DomainLink:   "https://app.getgram.ai/acme/domains",
	}

	require.Equal(t, map[string]string{
		"email":         "admin@example.com",
		"domain":        "mcp.example.com",
		"issue_message": "The TLS certificate for the domain has expired.",
		"domain_link":   "https://app.getgram.ai/acme/domains",
	}, tmpl.Variables())
}

func TestCustomDomainUnhealthy_Variables_PassesEmptyFieldsThrough(t *testing.T) {
	t.Parallel()

	tmpl := CustomDomainUnhealthy{
		Email:        "",
		Domain:       "",
		IssueMessage: "",
		DomainLink:   "",
	}

	variables := tmpl.Variables()
	require.Len(t, variables, 4)
	for key, value := range variables {
		require.Empty(t, value, "variable %q should pass through empty", key)
	}
}

func TestCustomDomainUnhealthy_AddToAudience(t *testing.T) {
	t.Parallel()

	require.False(t, CustomDomainUnhealthy{
		Email:        "",
		Domain:       "",
		IssueMessage: "",
		DomainLink:   "",
	}.AddToAudience())
}
