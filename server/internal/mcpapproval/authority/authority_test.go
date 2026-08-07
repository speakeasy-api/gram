package authority_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/authority"
)

func empty() authority.Declaration {
	return authority.Declaration{
		Transport: "", RequiresOAuth: false, OAuthVersion: "",
		RegistrationEndpoint: "", Scopes: nil, Credentials: nil, UnauthenticatedTools: nil,
	}
}

// Silence and "needs nothing" are different states. A server that published no
// auth information has told us nothing, and must not read as requiring nothing.
func TestSummarise_NothingPublishedIsUndeclared(t *testing.T) {
	t.Parallel()

	got := authority.Summarise(empty())

	require.True(t, got.Undeclared)
	require.Equal(t, authority.ModeUndeclared, got.Mode)
}

func TestSummarise_DeclaringNoCredentialIsNotUndeclared(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.Transport = "http"
	declaration.OAuthVersion = "none"

	got := authority.Summarise(declaration)

	require.False(t, got.Undeclared)
	require.Equal(t, authority.ModeNone, got.Mode)
	require.Equal(t, "http", got.Transport)
}

// A static key the customer pastes in is long-lived and bounded by nothing,
// which is a materially different ask from a delegated grant.
func TestSummarise_DemandedSecretIsAPIKeyMode(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.Transport = "HTTP"
	declaration.Credentials = []authority.Credential{
		{Name: "ADMIN_API_KEY", Secret: true, Required: true, Description: "admin key"},
		{Name: "REGION", Secret: false, Required: true, Description: ""},
		{Name: "OPTIONAL_TOKEN", Secret: true, Required: false, Description: ""},
	}

	got := authority.Summarise(declaration)

	require.Equal(t, authority.ModeAPIKey, got.Mode)
	require.Len(t, got.DemandedSecrets, 1)
	require.Equal(t, "ADMIN_API_KEY", got.DemandedSecrets[0].Name)
	require.Len(t, got.OptionalSecrets, 1)
	require.Equal(t, "http", got.Transport, "transport is normalised")
}

// A non-secret configuration value is not something the customer is handing
// over, and padding the list with them buries the one that matters.
func TestSummarise_NonSecretCredentialsAreNotDemanded(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.Credentials = []authority.Credential{
		{Name: "REGION", Secret: false, Required: true, Description: ""},
	}

	got := authority.Summarise(declaration)

	require.Empty(t, got.DemandedSecrets)
	require.Empty(t, got.OptionalSecrets)
	require.Equal(t, authority.ModeNone, got.Mode)
}

// A server may want both a delegated grant and an install key. The delegated
// grant is the part carrying the customer's own authority.
func TestSummarise_OAuthWinsOverAnInstallKey(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.RequiresOAuth = true
	declaration.Credentials = []authority.Credential{
		{Name: "API_KEY", Secret: true, Required: true, Description: ""},
	}

	got := authority.Summarise(declaration)

	require.Equal(t, authority.ModeOAuth, got.Mode)
	require.Len(t, got.DemandedSecrets, 1, "the key is still reported")
}

func TestSummarise_OAuthVersionImpliesOAuth(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.OAuthVersion = "2.1"

	require.Equal(t, authority.ModeOAuth, authority.Summarise(declaration).Mode)
}

// Registries report dynamic-registration support optimistically, returning
// true for authorization servers that publish no registration endpoint. The
// endpoint's presence is the only reliable signal.
func TestSummarise_DynamicRegistrationComesFromTheEndpoint(t *testing.T) {
	t.Parallel()

	with := empty()
	with.RequiresOAuth = true
	with.RegistrationEndpoint = "https://auth.example.com/register"
	require.True(t, authority.Summarise(with).DynamicRegistration)

	without := empty()
	without.RequiresOAuth = true
	require.False(t, authority.Summarise(without).DynamicRegistration)

	blank := empty()
	blank.RequiresOAuth = true
	blank.RegistrationEndpoint = "   "
	require.False(t, authority.Summarise(blank).DynamicRegistration, "whitespace is not an endpoint")
}

// The same scope set must render identically however a server ordered it, and
// a repeated scope must not read as two grants.
func TestSummarise_ScopesAreSortedAndDeduplicated(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.RequiresOAuth = true
	declaration.Scopes = []string{"write:all", "read:user", "write:all", "  ", "read:user"}

	got := authority.Summarise(declaration)

	require.Equal(t, []string{"read:user", "write:all"}, got.Scopes)
}

// A tool reachable with no credential grants authority to anyone who can reach
// the endpoint, not to the customer.
func TestSummarise_UnauthenticatedToolsAreCarried(t *testing.T) {
	t.Parallel()

	declaration := empty()
	declaration.Transport = "http"
	declaration.UnauthenticatedTools = []string{"search", "fetch"}

	got := authority.Summarise(declaration)

	require.Equal(t, []string{"search", "fetch"}, got.UnauthenticatedTools)
	require.False(t, got.Undeclared)
}
