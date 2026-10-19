package platformmcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestChatSearchToolContractAndDiscovery(t *testing.T) {
	t.Parallel()
	f := newChatSearchFixture(t)
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
		scopeCtx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(scope, authz.WildcardResource)})
		require.Len(t, live.FilterExternalTools(scopeCtx, f.principal, tools), 2)
	}
	admin := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, authz.WildcardResource)})
	require.Empty(t, live.FilterExternalTools(admin, f.principal, tools))
	require.Empty(t, live.For(AudienceAssistant))

	for _, name := range []string{searchChatsToolName, readChatToolName} {
		arguments := `{"project_slug":"fixture","chat_id":"00000000-0000-0000-0000-000000000001"}`
		if name == searchChatsToolName {
			arguments = `{"project_slug":"fixture","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"}`
		}
		_, err := descriptorByName(t, stub, name).Invoke(ctx, json.RawMessage(arguments))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, unavailableCode, refusal.Code)
		require.True(t, json.Valid([]byte(refusal.Payload)))
	}
	_, err := descriptorByName(t, live, searchChatsToolName).Invoke(ctx, json.RawMessage(`{"project_id":"bad","from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"}`))
	var invalid *ToolRefusalError
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, invalid.Payload, `"code":"invalid_request"`)
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

func TestChatSearchInputSchemas(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(newTestMCPServer())
	registerChatSearchTools(reg, nil)
	chatIDs := make([]string, chatSearchMaxChatIDs+1)
	for i := range chatIDs {
		chatIDs[i] = `"00000000-0000-0000-0000-000000000001"`
	}
	window := `"from":"2026-01-05T11:00:00Z","to":"2026-01-05T13:00:00Z"`
	cases := []struct {
		name, tool, arguments string
		valid                 bool
	}{
		{"search minimal", searchChatsToolName, `{"project_slug":"fixture",` + window + `}`, true},
		{"search every filter", searchChatsToolName, `{"project_id":"00000000-0000-0000-0000-000000000001",` + window + `,"query":"x","sort_order":"oldest_first","owner_user_id":"user","participant_id":"UEXAMPLE01","participant_provider":"slack","chat_ids":["00000000-0000-0000-0000-000000000001"],"source":"codex","limit":20,"cursor":"c"}`, true},
		{"search unicode query", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"query":"` + strings.Repeat("🙂", 256) + `"}`, true},
		{"search long query", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"query":"` + strings.Repeat("a", 257) + `"}`, false},
		{"search no project", searchChatsToolName, `{` + window + `}`, false},
		{"search both projects", searchChatsToolName, `{"project_slug":"fixture","project_id":"00000000-0000-0000-0000-000000000001",` + window + `}`, false},
		{"provider without participant", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"participant_provider":"slack"}`, false},
		{"search no dates", searchChatsToolName, `{"project_slug":"fixture"}`, false},
		{"search limit", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"limit":21}`, false},
		{"search sort", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"sort_order":"relevance"}`, false},
		{"search chat ids", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"chat_ids":[` + strings.Join(chatIDs, ",") + `]}`, false},
		{"search unknown", searchChatsToolName, `{"project_slug":"fixture",` + window + `,"unknown":true}`, false},
		{"read minimal", readChatToolName, `{"project_slug":"fixture","chat_id":"00000000-0000-0000-0000-000000000001"}`, true},
		{"read context", readChatToolName, `{"project_slug":"fixture","chat_id":"00000000-0000-0000-0000-000000000001","context_size":6}`, false},
		{"read no chat", readChatToolName, `{"project_slug":"fixture"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var schema jsonschema.Schema
			require.NoError(t, json.Unmarshal(descriptorByName(t, reg, tc.tool).InputSchema, &schema))
			resolved, err := schema.Resolve(nil)
			require.NoError(t, err)
			var args map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.arguments), &args))
			if tc.valid {
				require.NoError(t, resolved.Validate(args))
			} else {
				require.Error(t, resolved.Validate(args))
			}
		})
	}
}
