package mcp

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	testAgentSignIn = "https://app.example.com/acme/projects/default/identities/agents/agent-1/edit?step=2"
	testAgentAccess = "https://app.example.com/acme/projects/default/identities/agent:agent-1/permissions"
)

func testAgentFix() memberFix {
	return memberFix{agent: true, url: testAgentSignIn, accessURL: testAgentAccess}
}

// personFix is a dial's fix on a gateway whose caller opens url itself.
func personFix(url string) func() memberFix {
	return func() memberFix { return memberFix{agent: false, url: url, accessURL: ""} }
}

// testAgentGate is an agent gateway whose links read from the agent's
// project, or fail with err.
func testAgentGate(err error) metaGateContext {
	return metaGateContext{agentOwner: &agentOwnerReconnect{
		agent:    func() (string, error) { return "default", err },
		projects: map[uuid.UUID]string{},
		link:     func(string) memberFix { return testAgentFix() },
		logger:   nil,
	}}
}

// One resolver decides who fixes a member's connection, and where, on every
// kind of gateway: a person opens the member's connect page, an agent sends
// someone who can edit it to its sign-in step, and anyone else is sent to an
// administrator.
func TestMemberFix_PerGatewayKind(t *testing.T) {
	t.Parallel()

	root := "https://app.example.com/mcp/tools"
	page := root + "/connect/first-party?server=github"
	member := gatewayTestMember("github", "GitHub", uuid.New(), uuid.New())
	user := &issuerGateAuthentication{subject: urn.NewUserSubject("user-1")}
	none := memberFix{agent: false, url: "", accessURL: ""}
	brokenAgent := testAgentGate(errors.New("load agent: boom"))
	brokenAgent.agentOwner.logger = testenv.NewLogger(t)
	brokenAgent.agentOwner.ctx = t.Context()

	for _, tc := range []struct {
		name   string
		gate   metaGateContext
		member metaMember
		want   memberFix
	}{
		{name: "Platform gateway person", gate: metaGateContext{connectRoot: root, userID: "user-1"}, member: member, want: memberFix{agent: false, url: page, accessURL: ""}},
		{name: "person, member signs in through no service", gate: metaGateContext{connectRoot: root, userID: "user-1"}, member: gatewayTestMember("docs", "Docs", uuid.Nil, uuid.New()), want: none},
		{name: "org gateway with a connect root", gate: metaGateContext{connectRoot: root, organizationGateway: true, authentication: user}, member: member, want: memberFix{agent: false, url: page, accessURL: ""}},
		{name: "Code Mode user", gate: metaGateContext{connectRoot: root, discoveryMode: metamcp.DiscoveryModeCode, authentication: user}, member: member, want: memberFix{agent: false, url: page, accessURL: ""}},
		{name: "signed-in user without a connect root", gate: metaGateContext{authentication: user}, member: member, want: none},
		{name: "agent key on a server's own gateway", gate: metaGateContext{connectRoot: root, authentication: &issuerGateAuthentication{subject: urn.NewAgentSubject(uuid.New())}}, member: member, want: none},
		{name: "workload", gate: metaGateContext{connectRoot: root, authentication: &issuerGateAuthentication{subject: urn.NewWorkloadSubject(uuid.New(), "workload")}}, member: member, want: none},
		{name: "agent", gate: testAgentGate(nil), member: member, want: testAgentFix()},
		{name: "agent whose page could not be built", gate: brokenAgent, member: member, want: memberFix{agent: true, url: "", accessURL: ""}},
		{name: "agent gateway without links", gate: metaGateContext{connectRoot: root, userID: "user-1", agentID: uuid.New()}, member: member, want: none},
		{name: "anonymous", gate: metaGateContext{connectRoot: root}, member: member, want: none},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.gate.memberFix(tc.member))
		})
	}
	require.Empty(t, testAgentFix().callerURL(), "an agent cannot open its editors' page itself")
	require.Equal(t, page, memberFix{agent: false, url: page, accessURL: ""}.callerURL())
}

// An agent cannot sign in itself, so a rejected or missing connection sends it
// to the people who can edit it, with the agent's sign-in page. It names no
// one: the owner is not the only person who may fix it.
func TestMemberAuthFailure_AgentAsksAnEditor(t *testing.T) {
	t.Parallel()

	member := gatewayTestMember("linear", "Linear", uuid.New(), uuid.New())
	fix := testAgentFix

	err := memberAuthFailure(member, memberDial{anonymous: false, fix: fix}, http.StatusUnauthorized)
	require.ErrorContains(t, err, `server "linear" rejected this agent's connection, so it needs re-authentication`)
	require.ErrorContains(t, err, "Ask someone who can edit this agent to open "+testAgentSignIn+" and reconnect it, then retry")
	require.NotContains(t, err.Error(), "administrator")
	require.NotContains(t, err.Error(), "owner")
	_, credential := errors.AsType[memberCredentialError](err)
	require.True(t, credential, "a person fixes it; a retry does not")

	err = memberAuthFailure(member, memberDial{anonymous: true, oauthSignIn: true, fix: fix}, http.StatusUnauthorized)
	require.ErrorContains(t, err, "this agent has no connection to it")
	require.ErrorContains(t, err, "and connect it, then retry")
	require.NotContains(t, err.Error(), "for this user", "the agent's editors open the page, not the agent")

	// Without a page to send anyone to, the generic wording stands.
	noPage := func() memberFix { return memberFix{agent: true, url: "", accessURL: ""} }
	require.ErrorContains(t, memberAuthFailure(member, memberDial{fix: noPage}, http.StatusUnauthorized), "Ask your administrator to reconnect it")
	// A self client's credential is the administrator's.
	require.ErrorContains(t, memberAuthFailure(member, memberDial{clientCredential: true, fix: fix}, http.StatusUnauthorized), "contact the MCP server administrator")
	require.EqualError(t, memberAuthFailure(member, memberDial{anonymous: true, clientCredential: true, fix: fix}, http.StatusUnauthorized), `server "linear" requires authentication and has no credential for this caller. Ask your administrator to connect it`, "the agent's page is never offered as the caller's own")
	// With the agent's token sent beside configured headers, the refusal is
	// the token's.
	require.ErrorContains(t, memberAuthFailure(member, memberDial{configuredHeaders: true, fix: fix}, http.StatusUnauthorized), "Ask someone who can edit this agent to open "+testAgentSignIn+" and reconnect it")
}

// A 403 to a connection the upstream accepted is the upstream account's
// permissions: signing in again as the same account does not change them.
func TestMemberAuthFailure_AgentForbiddenIsNotReauthentication(t *testing.T) {
	t.Parallel()

	member := gatewayTestMember("linear", "Linear", uuid.New(), uuid.New())
	err := memberAuthFailure(member, memberDial{fix: testAgentFix}, http.StatusForbidden)
	require.ErrorContains(t, err, `server "linear" answered 403 Forbidden`)
	require.ErrorContains(t, err, "signing in again as the same account does not change that")
	require.ErrorContains(t, err, testAgentSignIn)
	require.NotContains(t, err.Error(), "needs re-authentication")
}

// A credential configured on the server, such as an API key, is not set at
// the agent's sign-in step, so its refusal or absence is the server's to fix.
func TestMemberAuthFailure_AgentServerCredentialIsTheServers(t *testing.T) {
	t.Parallel()

	fix := testAgentFix
	oauth := gatewayTestMember("linear", "Linear", uuid.New(), uuid.New())
	rejected := memberAuthFailure(oauth, memberDial{anonymous: true, oauthSignIn: true, configuredHeaders: true, fix: fix}, http.StatusUnauthorized)
	require.ErrorContains(t, rejected, `server "linear" rejected the credential configured for it in Speakeasy, such as an API key`)
	require.ErrorContains(t, rejected, "Ask an admin of the server's project to update the server's credential")
	require.NotContains(t, rejected.Error(), testAgentSignIn)

	apiKey := gatewayTestMember("weather", "Weather", uuid.Nil, uuid.New())
	missing := memberAuthFailure(apiKey, memberDial{anonymous: true, fix: fix}, http.StatusUnauthorized)
	require.ErrorContains(t, missing, `server "weather" requires a credential, such as an API key, and none is configured for it in Speakeasy`)
	require.ErrorContains(t, missing, "Ask an admin of the server's project to add the server's credential")
	require.NotContains(t, missing.Error(), testAgentSignIn)
}

// A missing or unusable connection asks for a reconnect; a refused use asks
// for a permissions review, since reconnecting cannot clear it. Other
// failures, and gateways that are not an agent's, pass through unchanged.
func TestWithAgentMemberFix_SeparatesBrokenConnectionsFromRefusedAccess(t *testing.T) {
	t.Parallel()

	member := gatewayTestMember("linear", "Linear", uuid.New(), uuid.New())
	other := errors.New("load agent member auth client: boom")

	none := metaGateContext{}
	require.Equal(t, other, none.withAgentMemberFix(member, other))
	refusal := agentServerCredentialError(member.slug)
	require.Equal(t, refusal, none.withAgentMemberFix(member, refusal), "only an agent gateway rewrites it")
	denied := agentServerAccessError(member.slug)
	require.Equal(t, denied, none.withAgentMemberFix(member, denied), "only an agent gateway rewrites it")
	require.Equal(t, refusal.Error(), denied.Error(), "other surfaces read the same as before")
	require.NoError(t, none.withAgentMemberFix(member, nil))
	require.ErrorContains(t, none.agentReconnectError(member), "unavailable or not permitted; reconnect it before retrying", "no link keeps the generic wording")

	agent := testAgentGate(nil)
	broken := agent.withAgentMemberFix(member, refusal)
	require.ErrorContains(t, broken, `server "linear" needs re-authentication: this agent's connection to it is missing or can no longer be used`)
	require.ErrorContains(t, broken, "Ask someone who can edit this agent to open "+testAgentSignIn+" and reconnect it, then retry")

	refused := agent.withAgentMemberFix(member, denied)
	require.ErrorContains(t, refused, `server "linear" refused this agent: it is not allowed to use a connection to this server, and reconnecting does not change that`)
	require.ErrorContains(t, refused, "review its permissions at "+testAgentAccess)
	require.NotContains(t, refused.Error(), "re-authentication")
	require.Equal(t, other, agent.withAgentMemberFix(member, other))
}

// connection_status marks a row the agent's sign-in step fixes by the error
// that failed it, not by its text, and gives it a short detail since the hint
// carries the link. A refused use, any other failure and every row off an
// agent gateway keep their own detail and no mark.
func TestMemberFailureSignIn_MarksRowsByErrorType(t *testing.T) {
	t.Parallel()

	agent := testAgentGate(nil)
	person := metaGateContext{connectRoot: "https://app.example.com/mcp/tools", userID: "user-1"}
	member := gatewayTestMember("linear", "Linear", uuid.New(), uuid.New())
	refused := agent.withAgentMemberFix(member, agentServerAccessError(member.slug))

	for _, tc := range []struct {
		name       string
		gate       metaGateContext
		err        error
		detail     string
		reconnects bool
	}{
		{name: "agent connection missing", gate: agent, err: agent.agentReconnectError(member), detail: agentDetailNeedsReauth, reconnects: true},
		{name: "agent connection missing, no link", gate: agent, err: agentServerCredentialError(member.slug), detail: agentDetailNeedsReauth, reconnects: true},
		{name: "agent connection missing, wrapped", gate: agent, err: memberConnectionError{agent.agentReconnectError(member), resolveFailureHappened(errors.New("boom"))}, detail: agentDetailNeedsReauth, reconnects: true},
		{name: "agent refused", gate: agent, err: refused, detail: refused.Error(), reconnects: false},
		{name: "agent member error", gate: agent, err: &metaMemberError{message: `server "linear" is not currently servable`}, detail: `server "linear" is not currently servable`, reconnects: false},
		{name: "agent unknown failure", gate: agent, err: errors.New("boom"), detail: "could not check this server", reconnects: false},
		{name: "person", gate: person, err: agentServerCredentialError(member.slug), detail: agentServerCredentialError(member.slug).Error(), reconnects: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := memberFailureSignIn(tc.gate, member, tc.err)
			require.Equal(t, metamcp.SignInError, row.Status)
			require.Equal(t, tc.detail, row.Detail)
			require.Equal(t, tc.reconnects, row.Reconnect)
		})
	}

	require.True(t, notConnectedRow(&agent, true).Reconnect, "the agent's sign-in step connects an OAuth sign-in")
	require.False(t, notConnectedRow(&agent, false).Reconnect, "a credential configured on the server is not set there")
	require.False(t, notConnectedRow(&person, true).Reconnect, "a person's connect page fixes every row")

	oauth, apiKey := member, gatewayTestMember("weather", "Weather", uuid.Nil, uuid.New())
	rows := []metamcp.ConnectionStatusServer{notConnectedRow(&agent, false), notConnectedRow(&agent, true)}
	require.Equal(t, testAgentSignIn, agentConnectURL(agent, []metaMember{apiKey, oauth}, rows))
	require.Empty(t, agentConnectURL(agent, []metaMember{apiKey}, rows[:1]), "nothing to reconnect gives no link")
}
