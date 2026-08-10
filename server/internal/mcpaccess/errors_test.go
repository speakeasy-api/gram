package mcpaccess_test

import (
	"errors"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestServerPermissionDeniedRewritesForbiddenError(t *testing.T) {
	t.Parallel()

	cause := oops.C(oops.CodeForbidden)
	err := mcpaccess.ServerPermissionDenied(cause, "")

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
	require.Equal(t, mcpaccess.ServerPermissionDeniedMessage, shareableErr.Error())
	require.ErrorIs(t, err, cause)
}

func TestToolPermissionDeniedRewritesForbiddenError(t *testing.T) {
	t.Parallel()

	cause := oops.C(oops.CodeForbidden)
	err := mcpaccess.ToolPermissionDenied(cause)

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
	require.Equal(t, mcpaccess.ToolPermissionDeniedMessage, shareableErr.Error())
	require.ErrorIs(t, err, cause)
}

func TestPermissionDeniedPreservesNonForbiddenError(t *testing.T) {
	t.Parallel()

	cause := errors.New("temporary failure")

	require.ErrorIs(t, mcpaccess.ServerPermissionDenied(cause, "https://app.example.com/acme/access/challenges"), cause)
	require.ErrorIs(t, mcpaccess.ToolPermissionDenied(cause), cause)
}

func TestServerPermissionDeniedIncludesRequestAccessURL(t *testing.T) {
	t.Parallel()

	cause := oops.C(oops.CodeForbidden)
	requestAccessURL := "https://app.example.com/acme/access/challenges"
	err := mcpaccess.ServerPermissionDenied(cause, requestAccessURL)

	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, mcpaccess.ServerPermissionDeniedMessage+"\n\nRequest access:\n"+requestAccessURL, shareableErr.Error())
	require.ErrorIs(t, err, cause)
}

func TestAuthorizationChallengesURL(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.com/base/")
	require.NoError(t, err)

	require.Equal(
		t,
		"https://app.example.com/base/acme/access/challenges",
		mcpaccess.AuthorizationChallengesURL(siteURL, "acme"),
	)
}

func TestAuthorizationChallengesURLRequiresInputs(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)

	require.Empty(t, mcpaccess.AuthorizationChallengesURL(nil, "acme"))
	require.Empty(t, mcpaccess.AuthorizationChallengesURL(siteURL, " "))
}

func TestRequestAccessURL(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.com/base/")
	require.NoError(t, err)

	// The query-param names are an integration contract with the dashboard's
	// /request-access page — changing them breaks deep links in the wild.
	require.Equal(
		t,
		"https://app.example.com/base/acme/request-access?resource_id=srv_123&resource_name=My+MCP+Server&scope=mcp%3Aconnect",
		mcpaccess.RequestAccessURL(siteURL, "acme", mcpaccess.RequestAccessURLParams{
			Scope:        "mcp:connect",
			ResourceID:   "srv_123",
			ResourceName: "My MCP Server",
		}),
	)
}

func TestRequestAccessURLOmitsEmptyParams(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)

	require.Equal(
		t,
		"https://app.example.com/acme/request-access?scope=mcp%3Aconnect",
		mcpaccess.RequestAccessURL(siteURL, "acme", mcpaccess.RequestAccessURLParams{
			Scope:        "mcp:connect",
			ResourceID:   "",
			ResourceName: "",
		}),
	)
}

func TestRequestAccessURLRequiresInputs(t *testing.T) {
	t.Parallel()

	siteURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)

	params := mcpaccess.RequestAccessURLParams{Scope: "mcp:connect", ResourceID: "", ResourceName: ""}
	require.Empty(t, mcpaccess.RequestAccessURL(nil, "acme", params))
	require.Empty(t, mcpaccess.RequestAccessURL(siteURL, " ", params))
}
