package builtinmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCatalogStableProjectIdentity(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	other := uuid.New()
	parsed, err := uuid.Parse(project.String())
	require.NoError(t, err)
	require.Equal(t, ID(project, Slack), ID(parsed, Slack))
	require.NotEqual(t, ID(project, Slack), ID(other, Slack))
	base, err := url.Parse("https://mcp.example")
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example/mcp/builtin/example/project/slack", URL(base, "example", "project", Slack))
}

func TestCatalogExplicitSlackTools(t *testing.T) {
	t.Parallel()
	definition, ok := Lookup(Slack)
	require.True(t, ok)
	require.NotEmpty(t, definition.Tools)
	require.Len(t, definition.Tools, len(slackHandlers))
	seen := map[string]bool{}
	for _, tool := range definition.Tools {
		require.Equal(t, Slack, tool.SourceSlug)
		require.False(t, seen[tool.Name], tool.Name)
		seen[tool.Name] = true
		require.True(t, json.Valid(tool.InputSchema), tool.Name)
		require.NotEqual(t, "get_file_url", tool.HandlerName)
	}
	require.True(t, seen["platform_slack_send_message"])
	require.Len(t, All(), 1)
	_, ok = Lookup("msteams")
	require.False(t, ok)
	_, ok = Lookup("assistants")
	require.False(t, ok)
}

func TestCatalogReturnedCollectionsAreIndependent(t *testing.T) {
	t.Parallel()
	first, _ := Lookup(Slack)
	first.Tools = nil
	first.CredentialVariables[0] = "OTHER"
	second, _ := Lookup(Slack)
	require.NotEmpty(t, second.Tools)
	require.Equal(t, "SLACK_BOT_TOKEN", second.CredentialVariables[0])
}

func TestRequireAdminRejectsUntrustedActorWithoutDatabase(t *testing.T) {
	t.Parallel()
	// No database is supplied: each path must reject before reading state, and
	// neither public provenance fields nor broad prepared grants establish actor identity.
	require.Error(t, RequireAdmin(context.Background(), nil, "org_test"))
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", UserID: "user_test", IsAdmin: true})
	require.Error(t, RequireAdmin(ctx, nil, "org_test"))
	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: "org_test", UserID: "user_test"}, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()))
	require.Error(t, RequireAdmin(ctx, nil, "org_test"))
	ctx = contextvalues.WithAuthenticatedActor(context.Background(), &contextvalues.AuthContext{ActiveOrganizationID: "org_test", UserID: "user_test", APIKeyID: "key_test"}, urn.NewPrincipal(urn.PrincipalTypeUser, "user_test"))
	require.Error(t, RequireAdmin(ctx, nil, "org_test"))
	ctx = contextvalues.WithAuthenticatedActor(context.Background(), &contextvalues.AuthContext{ActiveOrganizationID: "org_other", UserID: "user_test"}, urn.NewPrincipal(urn.PrincipalTypeUser, "user_test"))
	require.Error(t, RequireAdmin(ctx, nil, "org_test"))
}
