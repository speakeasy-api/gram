package telemetry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	telemetryRepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// ownScopeFixture seeds one tool call per attribution shape plus one hook
// trace per human, so each raw read can be checked against the own scope, and
// one chat session per attribution shape for the chat summary read.
type ownScopeFixture struct {
	ownTool          string
	ownAgentTool     string
	otherTool        string
	foreignAgentTool string
	ownHook          string
	otherHook        string
	ownAgentHook     string
	foreignAgentHook string
	ownEmailOnlyTool string
	ownChat          string
	ownAgentChat     string
	otherChat        string
	foreignAgentChat string
	ownEmailOnlyChat string
	ownAgentID       uuid.UUID
	foreignAgentID   uuid.UUID
	otherOwner       string
	suffix           string
	from, to         string
}

func seedOwnScopeFixture(t *testing.T, ctx context.Context, ti *testInstance, authCtx *contextvalues.AuthContext) ownScopeFixture {
	t.Helper()
	now := time.Now().UTC().Add(-time.Minute)
	suffix := uuid.NewString()[:8]
	f := ownScopeFixture{
		ownTool:          "own_tool_" + suffix,
		ownAgentTool:     "own_agent_tool_" + suffix,
		otherTool:        "other_tool_" + suffix,
		foreignAgentTool: "foreign_agent_tool_" + suffix,
		ownHook:          "own_hook_" + suffix,
		otherHook:        "other_hook_" + suffix,
		ownAgentHook:     "own_agent_hook_" + suffix,
		foreignAgentHook: "foreign_agent_hook_" + suffix,
		ownEmailOnlyTool: "own_email_only_tool_" + suffix,
		suffix:           suffix,
		from:             now.Add(-time.Hour).Format(time.RFC3339),
		to:               now.Add(time.Hour).Format(time.RFC3339),
	}

	f.ownAgentID = seedTelemetryAgent(t, ctx, ti, authCtx.UserID, "Own agent")
	f.otherOwner = "other-owner-" + suffix
	otherOwner := f.otherOwner
	f.foreignAgentID = seedTelemetryAgent(t, ctx, ti, otherOwner, "Someone else's agent")
	foreignAgentID := f.foreignAgentID

	email := conv.PtrValOr(authCtx.Email, "")
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{projectID: ti.projectID, timestamp: now, toolsetSlug: "ts", toolName: f.ownTool, userEmail: email, userID: authCtx.UserID, statusCode: 200})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{projectID: ti.projectID, timestamp: now, toolsetSlug: "ts", toolName: f.ownAgentTool, agentID: f.ownAgentID.String(), statusCode: 200})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{projectID: ti.projectID, timestamp: now, toolsetSlug: "ts", toolName: f.otherTool, userEmail: "other-" + suffix + "@example.com", userID: otherOwner, statusCode: 200})
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{projectID: ti.projectID, timestamp: now, toolsetSlug: "ts", toolName: f.foreignAgentTool, agentID: foreignAgentID.String(), statusCode: 200})
	insertHookEvent(t, ctx, hookEventParams{projectID: ti.projectID, timestamp: now, traceID: uuid.NewString(), userEmail: email, hookSource: "mcp", toolSource: "srv", toolName: f.ownHook, result: `"ok"`})
	insertHookEvent(t, ctx, hookEventParams{projectID: ti.projectID, timestamp: now, traceID: uuid.NewString(), userEmail: "other-" + suffix + "@example.com", hookSource: "mcp", toolSource: "srv", toolName: f.otherHook, result: `"ok"`})
	// Agent-driven hook sessions carry the agent as actor and no human email.
	insertHookEvent(t, ctx, hookEventParams{projectID: ti.projectID, timestamp: now, traceID: uuid.NewString(), hookSource: "mcp", toolSource: "srv", toolName: f.ownAgentHook, result: `"ok"`, customAttrs: agentActorAttrs(f.ownAgentID)})
	insertHookEvent(t, ctx, hookEventParams{projectID: ti.projectID, timestamp: now, traceID: uuid.NewString(), hookSource: "mcp", toolSource: "srv", toolName: f.foreignAgentHook, result: `"ok"`, customAttrs: agentActorAttrs(foreignAgentID)})
	// A row attributed by email alone exercises the email leg on its own.
	insertHostedToolEvent(t, ctx, ti, hostedToolEventParams{projectID: ti.projectID, timestamp: now, toolsetSlug: "ts", toolName: f.ownEmailOnlyTool, userEmail: email, statusCode: 200})

	// Chat sessions carry identity on their completion rows (user.id and
	// user.email) or, for agent-driven sessions, on their gateway tool-call
	// rows as the agent actor. The agent chats also hold an unattributed
	// completion row, so the summary must admit the whole chat, not just the
	// attributed rows.
	f.ownChat, f.ownAgentChat, f.otherChat, f.foreignAgentChat, f.ownEmailOnlyChat = uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	otherEmail := "other-" + suffix + "@example.com"
	insertOwnScopeChatRow(t, ctx, ti, f.ownChat, now, chatCompletionURN, chatCompletionAttrs(map[string]any{"user.id": authCtx.UserID, "user.email": email}))
	insertOwnScopeChatRow(t, ctx, ti, f.ownAgentChat, now, chatCompletionURN, chatCompletionAttrs(nil))
	insertOwnScopeChatRow(t, ctx, ti, f.ownAgentChat, now.Add(time.Second), chatToolCallURN, chatToolCallAttrs(f.ownAgentID))
	insertOwnScopeChatRow(t, ctx, ti, f.otherChat, now, chatCompletionURN, chatCompletionAttrs(map[string]any{"user.id": otherOwner, "user.email": otherEmail}))
	insertOwnScopeChatRow(t, ctx, ti, f.foreignAgentChat, now, chatCompletionURN, chatCompletionAttrs(nil))
	insertOwnScopeChatRow(t, ctx, ti, f.foreignAgentChat, now.Add(time.Second), chatToolCallURN, chatToolCallAttrs(foreignAgentID))
	insertOwnScopeChatRow(t, ctx, ti, f.ownEmailOnlyChat, now, chatCompletionURN, chatCompletionAttrs(map[string]any{"user.email": email}))
	return f
}

const (
	chatCompletionURN = "chat:completion"
	chatToolCallURN   = "tools:http:gram:agent_tool"
)

func chatCompletionAttrs(identity map[string]any) map[string]any {
	attrs := map[string]any{
		"gen_ai.response.id":    uuid.NewString(),
		"gen_ai.response.model": "test-model",
	}
	for k, v := range identity {
		attrs[k] = v
	}
	return attrs
}

func chatToolCallAttrs(agentID uuid.UUID) map[string]any {
	attrs := map[string]any{
		"gram.event.source":         "tool_call",
		"gram.tool.name":            "agent_tool",
		"http.response.status_code": 200,
	}
	for k, v := range agentActorAttrs(agentID) {
		attrs[k] = v
	}
	return attrs
}

// insertOwnScopeChatRow writes one telemetry row of a chat session.
func insertOwnScopeChatRow(t *testing.T, ctx context.Context, ti *testInstance, chatID string, timestamp time.Time, urn string, attrs map[string]any) {
	t.Helper()
	attrsJSON, err := json.Marshal(attrs)
	require.NoError(t, err)
	// Committed before returning so the read that follows sees the row.
	err = ti.chClient.InsertTelemetryLogsSync(ctx, []telemetryRepo.InsertTelemetryLogParams{{
		ID:                   uuid.NewString(),
		TimeUnixNano:         timestamp.UnixNano(),
		ObservedTimeUnixNano: timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "chat row",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        ti.projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              urn,
		ServiceName:          "gram-server",
		ServiceVersion:       nil,
		GramChatID:           &chatID,
	}})
	require.NoError(t, err)
}

// chatsVisible runs the chat summary read and returns the chats it lists,
// keyed by chat id.
func chatsVisible(t *testing.T, ctx context.Context, ti *testInstance, f ownScopeFixture) map[string]*telem_gen.ChatSummary {
	t.Helper()
	res, err := ti.service.SearchChats(ctx, &telem_gen.SearchChatsPayload{Limit: 100, Sort: "desc", Filter: &telem_gen.SearchChatsFilter{From: &f.from, To: &f.to}})
	require.NoError(t, err)
	out := make(map[string]*telem_gen.ChatSummary, len(res.Chats))
	for _, chat := range res.Chats {
		out[chat.GramChatID] = chat
	}
	return out
}

func agentActorAttrs(agentID uuid.UUID) map[string]any {
	return map[string]any{
		"gram.authorization.actor.type": "agent",
		"gram.authorization.actor.id":   agentID.String(),
	}
}

func seedTelemetryAgent(t *testing.T, ctx context.Context, ti *testInstance, ownerUserID, name string) uuid.UUID {
	t.Helper()
	_, err := orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: ti.orgID,
		UserID:         conv.ToPGText(ownerUserID),
	})
	require.NoError(t, err)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID,
		OwnerUserID:    ownerUserID,
		Name:           name,
	})
	require.NoError(t, err)
	return agent.ID
}

// toolNamesVisible runs every raw read and returns the tool names each one
// returned, keyed by endpoint.
func toolNamesVisible(t *testing.T, ctx context.Context, ti *testInstance, f ownScopeFixture) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}

	logs, err := ti.service.SearchLogs(ctx, &telem_gen.SearchLogsPayload{Limit: 100, Sort: "desc", Filter: &telem_gen.SearchLogsFilter{From: &f.from, To: &f.to}})
	require.NoError(t, err)
	out["SearchLogs"] = map[string]bool{}
	for _, record := range logs.Logs {
		if name := nestedString(record.Attributes, "gram", "tool", "name"); name != "" {
			out["SearchLogs"][name] = true
		}
	}

	calls, err := ti.service.SearchToolCalls(ctx, &telem_gen.SearchToolCallsPayload{Limit: 100, Sort: "desc", Filter: &telem_gen.SearchToolCallsFilter{From: &f.from, To: &f.to}})
	require.NoError(t, err)
	out["SearchToolCalls"] = map[string]bool{}
	for _, call := range calls.ToolCalls {
		out["SearchToolCalls"][conv.PtrValOr(call.ToolName, "")] = true
	}

	traces, err := ti.service.ListToolUsageTraces(ctx, &telem_gen.ListToolUsageTracesPayload{From: f.from, To: f.to, Limit: 100, Sort: "desc"})
	require.NoError(t, err)
	out["ListToolUsageTraces"] = map[string]bool{}
	for _, trace := range traces.Traces {
		out["ListToolUsageTraces"][trace.ToolName] = true
	}

	// A free-text query leaves the trace_summaries fast path for the raw
	// telemetry_logs scan; the fixture suffix is in every tool's gram_urn.
	rawTraces, err := ti.service.ListToolUsageTraces(ctx, &telem_gen.ListToolUsageTracesPayload{From: f.from, To: f.to, Limit: 100, Sort: "desc", Query: &f.suffix})
	require.NoError(t, err)
	out["ListToolUsageTracesRaw"] = map[string]bool{}
	for _, trace := range rawTraces.Traces {
		out["ListToolUsageTracesRaw"][trace.ToolName] = true
	}

	hooks, err := ti.service.ListHooksTraces(ctx, &telem_gen.ListHooksTracesPayload{From: f.from, To: f.to, Limit: 100, Sort: "desc"})
	require.NoError(t, err)
	out["ListHooksTraces"] = map[string]bool{}
	for _, trace := range hooks.Traces {
		out["ListHooksTraces"][conv.PtrValOr(trace.ToolName, "")] = true
	}
	return out
}

// A member without log:read reads their own tool calls and those of agents
// they own, and nothing else, through every raw read.
func TestTelemetry_LogReads_MemberSeesOwnAndOwnedAgentActivity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)

	memberCtx := authztest.WithExactGrants(t, ctx)
	visible := toolNamesVisible(t, memberCtx, ti, f)

	for _, endpoint := range []string{"SearchLogs", "SearchToolCalls", "ListToolUsageTraces", "ListToolUsageTracesRaw"} {
		require.True(t, visible[endpoint][f.ownTool], "%s: own tool call; visible=%v", endpoint, visible[endpoint])
		require.True(t, visible[endpoint][f.ownAgentTool], "%s: owned agent's tool call", endpoint)
		require.False(t, visible[endpoint][f.otherTool], "%s: another member's tool call", endpoint)
		require.False(t, visible[endpoint][f.foreignAgentTool], "%s: another owner's agent", endpoint)
	}
	require.True(t, visible["ListHooksTraces"][f.ownHook])
	require.False(t, visible["ListHooksTraces"][f.otherHook])
	require.True(t, visible["ListHooksTraces"][f.ownAgentHook], "owned agent's hook session")
	require.False(t, visible["ListHooksTraces"][f.foreignAgentHook], "another owner's agent's hook session")
	// Hook rows for the member's own session are also raw logs.
	require.True(t, visible["SearchLogs"][f.ownHook])
	require.False(t, visible["SearchLogs"][f.otherHook])
	require.True(t, visible["SearchLogs"][f.ownAgentHook])
	require.False(t, visible["SearchLogs"][f.foreignAgentHook])
	// The email leg alone attributes a row to the member on the trace reads.
	require.True(t, visible["SearchToolCalls"][f.ownEmailOnlyTool], "email-only attribution")
	require.True(t, visible["ListToolUsageTraces"][f.ownEmailOnlyTool], "email-only attribution")
}

// log:read on the project still reads everything; the scope only applies to
// callers without it.
func TestTelemetry_LogReads_LogReadGrantSeesEveryMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeLogRead, authCtx.ProjectID.String()))
	visible := toolNamesVisible(t, readerCtx, ti, f)

	for _, endpoint := range []string{"SearchLogs", "SearchToolCalls", "ListToolUsageTraces", "ListToolUsageTracesRaw"} {
		for _, tool := range []string{f.ownTool, f.ownAgentTool, f.otherTool, f.foreignAgentTool} {
			require.True(t, visible[endpoint][tool], "%s: %s; visible=%v", endpoint, tool, visible[endpoint])
		}
	}
	require.True(t, visible["ListHooksTraces"][f.ownHook])
	require.True(t, visible["ListHooksTraces"][f.otherHook])
}

// A member without chat:read lists their own chat sessions and those of agents
// they own, with every row of each session counted, and nothing else.
func TestTelemetry_SearchChats_MemberSeesOwnAndOwnedAgentChats(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)

	visible := chatsVisible(t, authztest.WithExactGrants(t, ctx), ti, f)

	require.NotNil(t, visible[f.ownChat], "own chat")
	require.NotNil(t, visible[f.ownEmailOnlyChat], "chat attributed by email alone")
	require.NotNil(t, visible[f.ownAgentChat], "owned agent's chat")
	require.Nil(t, visible[f.otherChat], "another member's chat")
	require.Nil(t, visible[f.foreignAgentChat], "another owner's agent's chat")
	// The agent chat's unattributed completion row stays in its summary.
	require.Equal(t, uint64(2), visible[f.ownAgentChat].LogCount)
	require.Equal(t, uint64(1), visible[f.ownAgentChat].ToolCallCount)
}

// chat:read on the project lists every member's sessions.
func TestTelemetry_SearchChats_ChatReadGrantSeesEveryMember(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeChatRead, authCtx.ProjectID.String()))
	visible := chatsVisible(t, readerCtx, ti, f)

	for _, chat := range []string{f.ownChat, f.ownAgentChat, f.otherChat, f.foreignAgentChat, f.ownEmailOnlyChat} {
		require.NotNil(t, visible[chat], "chat %s", chat)
	}
}

// Naming another member in the chat filter narrows the own scope to nothing
// instead of widening it.
func TestTelemetry_SearchChats_MemberFilterCannotWidenScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)

	res, err := ti.service.SearchChats(authztest.WithExactGrants(t, ctx), &telem_gen.SearchChatsPayload{Limit: 100, Sort: "desc", Filter: &telem_gen.SearchChatsFilter{From: &f.from, To: &f.to, UserID: &f.otherOwner}})
	require.NoError(t, err, "cause: %v", errors.Unwrap(err))
	require.Empty(t, res.Chats)
}

// Payload filters never widen the scope: asking for another member's user id
// returns nothing rather than their rows.
func TestTelemetry_LogReads_MemberFilterCannotWidenScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)
	memberCtx := authztest.WithExactGrants(t, ctx)

	logs, err := ti.service.SearchLogs(memberCtx, &telem_gen.SearchLogsPayload{Limit: 100, Sort: "desc", Filter: &telem_gen.SearchLogsFilter{From: &f.from, To: &f.to, UserID: &f.otherOwner}})
	require.NoError(t, err)
	require.Empty(t, logs.Logs)

	// Naming another owner's agent is the obvious widening vector on the
	// usage traces; it narrows to nothing instead.
	traces, err := ti.service.ListToolUsageTraces(memberCtx, &telem_gen.ListToolUsageTracesPayload{From: f.from, To: f.to, Limit: 100, Sort: "desc", AgentIds: []string{f.foreignAgentID.String()}})
	require.NoError(t, err)
	require.Empty(t, traces.Traces)
}

// Assistant-runtime, embedded chat-session and API-key callers are not a member
// reading their own activity; all keep the plain check and are refused without
// the read's wide scope.
func TestTelemetry_OwnScopeReads_NonMemberCallersKeepPlainCheck(t *testing.T) {
	t.Parallel()

	type caller struct {
		shape func(t *testing.T, ctx context.Context) context.Context
		code  oops.Code
	}
	callers := map[string]caller{
		"assistant runtime": {
			shape: func(t *testing.T, ctx context.Context) context.Context {
				t.Helper()
				return contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
			},
			code: oops.CodeForbidden,
		},
		"embedded chat session": {
			shape: func(t *testing.T, ctx context.Context) context.Context {
				t.Helper()
				authCtx, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				clone := *authCtx
				clone.ExternalUserID = "external-" + uuid.NewString()[:8]
				return contextvalues.SetAuthContext(ctx, &clone)
			},
			code: oops.CodeForbidden,
		},
		// An API key that carries its creator's user id but no authorization
		// mode is refused outright rather than read as that member.
		"api key with creator user id": {
			shape: func(t *testing.T, ctx context.Context) context.Context {
				t.Helper()
				authCtx, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				clone := *authCtx
				clone.APIKeyID = uuid.NewString()
				return contextvalues.SetAuthContext(ctx, &clone)
			},
			code: oops.CodeUnauthorized,
		},
	}
	for name, c := range callers {
		for _, endpoint := range ownScopeEndpoints {
			t.Run(name+"/"+endpoint.name, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestLogsService(t)
				ctx = authztest.WithExactGrants(t, c.shape(t, ctx))
				endpoint.call(ctx, ti, func(err error) {
					var oopsErr *oops.ShareableError
					require.ErrorAs(t, err, &oopsErr)
					require.Equal(t, c.code, oopsErr.Code)
				})
			})
		}
	}
}

// An agent latched for owner reassignment no longer answers for its owner, so
// its rows drop out of the owner's scope.
func TestTelemetry_LogReads_LatchedAgentLeavesOwnerScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)
	_, err := agentsrepo.New(ti.conn).LatchAgentsForOwnerLossByMembership(ctx, agentsrepo.LatchAgentsForOwnerLossByMembershipParams{
		OwnerReassignmentReason: conv.ToPGText("organization_membership_lost"),
		OrganizationID:          ti.orgID,
		OwnerUserID:             authCtx.UserID,
	})
	require.NoError(t, err)

	memberCtx := authztest.WithExactGrants(t, ctx)
	visible := toolNamesVisible(t, memberCtx, ti, f)
	for _, endpoint := range []string{"SearchLogs", "SearchToolCalls", "ListToolUsageTraces", "ListToolUsageTracesRaw"} {
		require.True(t, visible[endpoint][f.ownTool], "%s: own calls stay visible", endpoint)
		require.False(t, visible[endpoint][f.ownAgentTool], "%s: a latched agent's calls are no longer the owner's", endpoint)
	}
	chats := chatsVisible(t, memberCtx, ti, f)
	require.NotNil(t, chats[f.ownChat], "own chats stay visible")
	require.Nil(t, chats[f.ownAgentChat], "a latched agent's chats are no longer the owner's")
}

// requireOwnScopeOnly seeds rows for another member and checks that the
// caller, who lacks the project-wide scope, does not see them through any raw
// read or the chat summary read.
func requireOwnScopeOnly(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	f := seedOwnScopeFixture(t, ctx, ti, authCtx)
	visible := toolNamesVisible(t, ctx, ti, f)
	for _, endpoint := range []string{"SearchLogs", "SearchToolCalls", "ListToolUsageTraces", "ListToolUsageTracesRaw"} {
		require.False(t, visible[endpoint][f.otherTool], "%s: another member's tool call leaked", endpoint)
		require.False(t, visible[endpoint][f.foreignAgentTool], "%s: another owner's agent leaked", endpoint)
	}
	require.False(t, visible["ListHooksTraces"][f.otherHook])
	chats := chatsVisible(t, ctx, ti, f)
	require.Nil(t, chats[f.otherChat], "another member's chat leaked")
	require.Nil(t, chats[f.foreignAgentChat], "another owner's agent's chat leaked")
}

func nestedString(value any, path ...string) string {
	for _, key := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value = object[key]
	}
	str, _ := value.(string)
	return str
}
