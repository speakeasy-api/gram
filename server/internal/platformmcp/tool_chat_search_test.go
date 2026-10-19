package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestChatEvidenceToolContractAndDiscovery(t *testing.T) {
	t.Parallel()
	f := newChatEvidenceFixture(t)
	ctx := f.context(t.Context())
	live := newRegistrar(newTestMCPServer())
	registerChatSearchTools(live, f.service)
	stub := newRegistrar(newTestMCPServer())
	registerChatSearchTools(stub, nil)
	for _, name := range []string{searchChatsToolName, readChatToolName} {
		descriptor := descriptorByName(t, live, name)
		unavailable := descriptorByName(t, stub, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.Equal(t, descriptor.Meta, unavailable.Meta)
		require.JSONEq(t, string(descriptor.InputSchema), string(unavailable.InputSchema))
		require.Equal(t, descriptor.output, unavailable.output)
		require.True(t, descriptor.Annotations.ReadOnlyHint)
		require.Nil(t, descriptor.Annotations.DestructiveHint)
		require.Contains(t, descriptor.Description, "untrusted")
	}
	tools := []*mcp.Tool{{Name: searchChatsToolName}, {Name: readChatToolName}}
	for _, scope := range []authz.Scope{authz.ScopeChatRead, authz.ScopeChatWrite} {
		ctx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(scope, authz.WildcardResource)})
		require.Len(t, live.FilterExternalTools(ctx, f.principal, tools), 2)
	}
	admin := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)})
	require.Empty(t, live.FilterExternalTools(admin, f.principal, tools))
	require.Empty(t, live.For(AudienceAssistant))
	for _, arguments := range []string{
		`{}`,
		`{"from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"}`,
		`{"project_slug":"fixture","project_id":"00000000-0000-0000-0000-000000000001","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"}`,
		`{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z","limit":21}`,
		`{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z","unknown":true}`,
		`{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z","participant":{}}`,
		`{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z","participant":{"provider":"slack","provider_user_id":"UEXAMPLE01","unknown":true}}`,
		`{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z","participant":{"user_id":"fixture","provider":"slack","provider_user_id":"UEXAMPLE01"}}`,
	} {
		_, err := descriptorByName(t, live, searchChatsToolName).Invoke(ctx, json.RawMessage(arguments))
		require.ErrorContains(t, err, "arguments do not match the tool schema", arguments)
	}
	for _, name := range []string{searchChatsToolName, readChatToolName} {
		arguments := `{"project_slug":"fixture","chat_id":"00000000-0000-0000-0000-000000000001"}`
		if name == searchChatsToolName {
			arguments = `{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"}`
		}
		_, err := descriptorByName(t, stub, name).Invoke(ctx, json.RawMessage(arguments))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, unavailableCode, refusal.Code)
	}
	_, err := descriptorByName(t, live, searchChatsToolName).Invoke(ctx, json.RawMessage(`{"project_id":"bad","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"}`))
	var invalid *ToolRefusalError
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, invalid.Payload, `"code":"invalid_request"`)
	// Public tool invocation exercises typed schemas and the shared error mapping.
	args, err := json.Marshal(f.search("no match"))
	require.NoError(t, err)
	result, err := descriptorByName(t, live, searchChatsToolName).Invoke(ctx, args)
	require.NoError(t, err)
	output, ok := result.(SearchChatsOutput)
	require.True(t, ok)
	require.Empty(t, output.Hits)
	_, err = descriptorByName(t, live, searchChatsToolName).Invoke(admin, args)
	var denied *ToolRefusalError
	require.ErrorAs(t, err, &denied)
	require.Contains(t, denied.Payload, `"required_scope":"chat:read"`)
}
