package demoseed

import (
	"strconv"
	"time"
)

// chOwnerSessionRows are the tool calls behind the agent owner persona's
// sessions (see ownerSessions), which the owner-scoped Tool Logs read returns
// without log:read. The agent's calls name it as actor, as a request signed
// with the agent's own key records: no user.email and no authorizer. The
// persona's own calls carry user.id and user.email like any member's. One
// agent session adds the hook rows its harness sent with the agent's key.
func (s *seeder) chOwnerSessionRows() []chTelemetryRow {
	var rows []chTelemetryRow
	agentActor := `,"gram.authorization.actor.type":"agent","gram.authorization.actor.id":"` + s.ownerAgentID().String() + `"`
	owner := `,"user.id":"` + s.userID(demoAgentOwner.handle) + `"` +
		`,"user.email":"` + s.email(demoAgentOwner.handle) + `"` +
		`,"gram.external_user.id":"` + s.email(demoAgentOwner.handle) + `"` +
		chUserIdentityAttrs(demoAgentOwner, `["developer"]`) +
		`,"gram.account_type":"team"` +
		`,"gram.hook.source":"claude-code"`
	for _, sess := range ownerSessions {
		chatID := s.ownerSessionChatID(sess.key).String()
		start := s.now.Add(-time.Duration(sess.ageMin) * time.Minute)
		for k, tool := range sess.tools {
			call := k + 1
			ts := start.Add(ownerCallOffset(call)).UnixNano()
			attrs := `{"gram.tool.urn":"` + toolURN(tool) + `"` +
				`,"gram.tool.name":"` + tool + `"` +
				`,"gram.event.source":"tool_call"` +
				chIf(sess.agent, agentActor, owner) +
				`,"gram.toolset.slug":"acme-ops"` +
				`,"gram.mcp_server.id":"` + s.det("mcpserver-ops").String() + `"` +
				`,"http.response.status_code":200` +
				`,"gram.outcome":"ok"` +
				`,"http.server.request.duration":` + chRound(0.05+float64(hash64("ownerdur", sess.key, call)%200)/100, 3) +
				`,"gen_ai.tool.call.arguments":"` + s.ownerToolArgs(sess, call) + `"` +
				`,"gen_ai.tool.call.result":"` + s.ownerToolResult(sess, call) + `"` +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"}`
			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "Tool call: " + tool,
				traceID:      s.chTrace("ownertool-" + sess.key + "-" + strconv.Itoa(call)),
				attributes:   attrs,
				resource:     s.chResource(),
				urn:          toolURN(tool),
				service:      "gram-mcp-gateway",
				chatID:       new(chatID),
			})
		}
	}
	return append(rows, s.chOwnerHookRows(agentActor)...)
}

// chOwnerHookRows: the first agent session ran in a harness whose hooks were
// sent with the agent's key, so each of its calls is also a PostToolUse hook
// naming the agent and the gateway it called, as chfGatewayHooks shapes them.
func (s *seeder) chOwnerHookRows(agentActor string) []chTelemetryRow {
	sess := ownerHookSession()
	gatewaySlug := s.spec.OrgSlug + "-gateway"
	gatewayURL := "https://app.getgram.ai/mcp/" + gatewaySlug
	chatID := s.ownerSessionChatID(sess.key).String()
	start := s.now.Add(-time.Duration(sess.ageMin) * time.Minute)
	rows := make([]chTelemetryRow, 0, len(sess.tools))
	for k, tool := range sess.tools {
		call := k + 1
		ts := start.Add(ownerCallOffset(call) + 2*time.Second).UnixNano()
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "INFO",
			body:         "Hook: PostToolUse " + tool,
			traceID:      s.chTrace("ownerhook-" + sess.key + "-" + strconv.Itoa(call)),
			attributes: `{"gram.event.source":"hook"` +
				`,"gram.hook.source":"claude-code"` +
				`,"gram.hook.event":"PostToolUse"` +
				`,"gram.tool.name":"` + tool + `"` +
				`,"gram.tool_call.source":"` + gatewaySlug + `"` +
				`,"gram.mcp.match":"` + gatewayURL + `"` +
				`,"gram.mcp.server_url":"` + gatewayURL + `"` +
				`,"gen_ai.tool.call.result":"ok"` +
				`,"gen_ai.tool.call.id":"` + ownerToolCallID(sess.key, call) + `"` +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				agentActor + `}`,
			resource: s.chResource(),
			urn:      "",
			service:  "",
			chatID:   new(chatID),
		})
	}
	return rows
}

// ownerAgentTraces and ownerHumanTraces are how many traces the rows above
// write for the agent and for the persona, which the postflight asserts.
func ownerAgentTraces() int {
	n := len(ownerHookSession().tools)
	for _, sess := range ownerSessions {
		if sess.agent {
			n += len(sess.tools)
		}
	}
	return n
}

func ownerHumanTraces() int {
	n := 0
	for _, sess := range ownerSessions {
		if !sess.agent {
			n += len(sess.tools)
		}
	}
	return n
}
