package demoseed

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// chCheck is one postflight assert. cond is a ClickHouse boolean expression
// over the seeded rows that is true when the check FAILS (the old script's
// throwIf condition); settings, when set, is the query's SETTINGS clause.
type chCheck struct {
	cond     string
	settings string
	msg      string
}

// chEventsAndPostflight: Agent events, the remaining telemetry, and the postflight checks.
func (s *seeder) chEventsAndPostflight(ctx context.Context, ch driver.Conn) error {
	if err := s.chRunChecks(ctx, ch, s.chTelemetryChecks()); err != nil {
		return err
	}
	if err := s.chInsertAgentTurnEvents(ctx, ch); err != nil {
		return err
	}
	if err := s.chInsertAgentToolEvents(ctx, ch); err != nil {
		return err
	}
	if err := s.chInsertMCPNetworkRequests(ctx, ch); err != nil {
		return err
	}
	return s.chRunChecks(ctx, ch, chAgentEventChecks())
}

// chCheckArgs are the tenant identifiers every check's @name placeholders
// bind to.
func (s *seeder) chCheckArgs() []any {
	return []any{
		clickhouse.Named("org", s.orgID()),
		clickhouse.Named("proj", s.projectID().String()),
		clickhouse.Named("marker", s.spec.Marker),
		clickhouse.Named("github", s.det("mcpserver-github").String()),
		clickhouse.Named("gateway_suffix", "/mcp/"+s.spec.OrgSlug+"-gateway"),
	}
}

func (s *seeder) chRunChecks(ctx context.Context, ch driver.Conn, checks []chCheck) error {
	args := s.chCheckArgs()
	for _, c := range checks {
		query := "SELECT toUInt8(" + c.cond + ")"
		if c.settings != "" {
			query += " SETTINGS " + c.settings
		}
		var failed uint8
		if err := ch.QueryRow(ctx, query, args...).Scan(&failed); err != nil {
			return fmt.Errorf("run check %q: %w", c.msg, err)
		}
		if failed != 0 {
			return errors.New(c.msg)
		}
	}
	return nil
}

const chFinalSettings = "do_not_merge_across_partitions_select_final = 1"

func (s *seeder) chTelemetryChecks() []chCheck {
	return []chCheck{
		{
			cond: `(SELECT count() FROM billing_meter_readings_by_time FINAL
   WHERE organization_id = @org AND reading_kind = 'usage') != 864`,
			settings: chFinalSettings,
			msg:      "demo seed postflight: meter usage missing or duplicated",
		},
		{
			cond: `(SELECT uniqExact(meter_id) FROM billing_meter_readings_by_time FINAL
   WHERE organization_id = @org AND reading_kind = 'usage') != 9
  OR (SELECT count() FROM billing_meter_readings_by_time FINAL
      WHERE organization_id = @org AND reading_kind != 'usage') != 0`,
			settings: chFinalSettings,
			msg:      "demo seed postflight: meter families missing or unexpected non-usage readings",
		},
		{
			cond: `(SELECT sum(reading_count) FROM billing_meter_daily_summaries
   WHERE organization_id = @org
     AND facet = 'total') != 873`,
			settings: "",
			msg:      "demo seed postflight: ordinary meter deliveries missing or duplicated",
		},
		// Postflight asserts: rows landed, the cost/session MVs actually fired,
		// and nothing leaked outside the demo scope. A failed check aborts the
		// run.
		{
			cond:     `(SELECT count() FROM telemetry_logs WHERE gram_project_id = toUUID(@proj)) < 1500`,
			settings: "",
			msg:      "demo seed postflight: expected >= 1500 demo telemetry rows",
		},
		{
			cond: `(SELECT count() FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND meta_mcp_server_id != ''
     AND event_source = 'meta_discovery') < 150`,
			settings: "",
			msg:      "demo seed postflight: gateway discovery rows missing",
		},
		{
			cond: `(SELECT uniqExact(mcp_server_id) FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND meta_mcp_server_id != ''
     AND event_source = 'tool_call') < 4`,
			settings: "",
			msg:      "demo seed postflight: gateway member calls do not cover every member",
		},
		{
			cond: `(SELECT count() FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND meta_mcp_server_id != ''
     AND event_source = 'tool_call'
     AND mcp_server_id = @github) < 24`,
			settings: "",
			msg:      "demo seed postflight: removed gateway member dispatches missing",
		},
		{
			cond: `(SELECT count() FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND event_source = 'hook'
     AND endsWith(toString(attributes.gram.mcp.server_url), @gateway_suffix)) < 40`,
			settings: "",
			msg:      "demo seed postflight: hook-observed gateway calls missing",
		},
		{
			cond:     `(SELECT uniqExact(chat_id) FROM chat_session_summaries WHERE gram_project_id = toUUID(@proj)) < 180`,
			settings: "",
			msg:      "demo seed postflight: chat_session_summaries_mv missing sessions",
		},
		// Every request-shaped row carries gen_ai.response.id, the field the
		// per-user summary counts chat requests by. Without it "Chat requests"
		// reads 0 on every identity while the chat count beside it reads
		// correctly, which looks like a broken panel rather than absent data.
		//
		// Request-shaped means the row reports token usage: that is what
		// separates an LLM request from the tool-call and hook rows seeded
		// alongside it, and it stays true of request rows added later
		// regardless of their body text. A global floor would not catch a new
		// insert that forgot the field, so the assert is that no such row is
		// missing it, with a floor beside it so an empty table cannot satisfy
		// the first check vacuously.
		{
			cond: `(SELECT countIf(toString(attributes.gen_ai.response.id) = '') FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND (toString(attributes.input_tokens) != ''
          OR toString(attributes.gen_ai.usage.input_tokens) != '')) > 0`,
			settings: "",
			msg:      "demo seed postflight: request rows missing gen_ai.response.id",
		},
		{
			cond: `(SELECT count() FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND (toString(attributes.input_tokens) != ''
          OR toString(attributes.gen_ai.usage.input_tokens) != '')) < 180`,
			settings: "",
			msg:      "demo seed postflight: too few request rows",
		},
		{
			cond: `(SELECT count() FROM attribute_metrics_summaries WHERE gram_project_id = toUUID(@proj)
     AND department_name != '') = 0`,
			settings: "",
			msg:      "demo seed postflight: attribute_metrics_summaries has no identity dimensions",
		},
		{
			cond:     `(SELECT count() FROM authz_challenges WHERE organization_id = @org) = 0`,
			settings: "",
			msg:      "demo seed postflight: authz_challenges empty",
		},
		{
			cond:     `(SELECT count() FROM skill_session_versions WHERE organization_id = @org) < 90`,
			settings: "",
			msg:      "demo seed postflight: skill_session_versions missing rows",
		},
		{
			cond:     `(SELECT count() FROM skill_efficacy_scores WHERE organization_id = @org) < 90`,
			settings: "",
			msg:      "demo seed postflight: skill_efficacy_scores missing rows",
		},
		{
			cond:     `(SELECT count() FROM risk_findings WHERE organization_id = @org) < 90`,
			settings: "",
			msg:      "demo seed postflight: risk_findings mirror missing rows",
		},
		{
			cond:     `(SELECT uniqExact(target_id) FROM ai_detections WHERE organization_id = @org) < 15`,
			settings: "",
			msg:      "demo seed postflight: ai_detections missing targets",
		},
		// Each AI Discovery tab reads one category, so a category with no rows
		// is an empty page. Assert all three survived the reseed rather than
		// only the target count, which cannot tell a missing tab from a
		// missing tool.
		{
			cond: `(SELECT uniqExact(category) FROM ai_detections
   WHERE organization_id = @org
     AND category IN ('harness', 'assistant', 'local_model')) < 3`,
			settings: "",
			msg:      "demo seed postflight: ai_detections must cover harness, assistant and local_model",
		},
		{
			cond:     `(SELECT count() FROM ai_scan_receipts WHERE organization_id = @org) < 25`,
			settings: "",
			msg:      "demo seed postflight: ai_scan_receipts missing rows",
		},
		// The Watchdog groups by these three denormalized columns. A mirror
		// that forgot them still lists signals, but every App/Team grouping
		// and every top-user row renders empty, which is the failure this
		// catches.
		{
			cond: `(SELECT count() FROM risk_findings
   WHERE organization_id = @org
     AND (chat_source = '' OR team = '' OR user_email = '')) > 0`,
			settings: "",
			msg:      "demo seed postflight: risk_findings missing chat_source/team/user_email attribution",
		},
		// Platform MCP summarizes rule-level impact, not individual finding
		// rows. Keep at least one live cluster with multiple attributed users
		// and clients plus a stored display sample so that its privacy-safe
		// evidence path is exercised.
		{
			cond: `(SELECT count() FROM (
    SELECT rule_id
    FROM risk_findings
    WHERE organization_id = @org
      AND project_id = @proj
      AND excluded_at IS NULL AND false_positive_at IS NULL
      AND dead_letter_reason = ''
    GROUP BY rule_id
    HAVING uniqExactIf(if(external_user_id != '', external_user_id, user_id),
                      external_user_id != '' OR user_id != '') > 1
       AND uniqExactIf(chat_source, chat_source != '') > 1
       AND countIf(match_redacted != '') > 0
  )) = 0`,
			settings: "",
			msg:      "demo seed postflight: watchdog needs multi-user multi-client evidence",
		},
		{
			cond: `(SELECT count() FROM risk_findings
   WHERE organization_id = @org
     AND mcp_server_id != ''
     AND (execution_id = '' OR toolset_id = '' OR tool_name = ''
          OR phase NOT IN ('request', 'response')
          OR mediation_surface != 'hosted_mcp'
          OR mcp_method != 'tools/call' OR principal_kind != 'user_session'
          OR identity_stamped = false
          OR enforcement_outcome NOT IN ('logged', 'denied'))) > 0
  OR
  (SELECT count() FROM risk_findings
   WHERE organization_id = @org
     AND mcp_server_id != '') = 0
  OR
  (SELECT count() FROM risk_findings
   WHERE organization_id = @org
     AND mcp_server_id != '' AND chat_id = '') = 0`,
			settings: "",
			msg:      "demo seed postflight: mediated risk findings missing complete MCP-only attribution",
		},
		// The dedicated Fleet exposure example survives reseeds exactly once.
		{
			cond: `(SELECT count() FROM risk_findings
   WHERE organization_id = @org
     AND description = '` + fleetFindingDescription + `') != 1`,
			settings: "",
			msg:      "demo seed postflight: expected one Fleet transcript finding",
		},
		// Fewer than four distinct rule clusters means the weighted type draw
		// collapsed and the Watchdog list is a flat rotation again.
		{
			cond:     `(SELECT uniqExact(rule_id) FROM risk_findings WHERE organization_id = @org) < 8`,
			settings: "",
			msg:      "demo seed postflight: risk_findings cover too few rules to form signals",
		},
		{
			cond: `(SELECT count() FROM risk_findings
   WHERE organization_id = @org
     AND excluded_reason NOT IN ('', 'rule', 'manual', 'automated')) > 0`,
			settings: "",
			msg:      "demo seed postflight: risk_findings carry an unrecognized excluded_reason",
		},
		{
			cond: `(SELECT count() FROM telemetry_logs
   WHERE toString(resource_attributes.gram.deployment.id) = @marker
     AND gram_project_id != toUUID(@proj)) > 0`,
			settings: "",
			msg:      "demo seed postflight: " + s.spec.Marker + " rows leaked outside demo projects",
		},
		// The inverse closes the loop: every row under the demo projects must
		// carry the seed marker, so the leak check above provably covers all
		// seeded telemetry (an insert that forgot the marker would slip past
		// it).
		{
			cond: `(SELECT count() FROM telemetry_logs
   WHERE gram_project_id = toUUID(@proj)
     AND toString(resource_attributes.gram.deployment.id) != @marker) > 0`,
			settings: "",
			msg:      "demo seed postflight: demo-project telemetry rows missing the " + s.spec.Marker + " marker",
		},
		// The Shadow AI inventory is pinned exactly, not bounded. Its size is
		// what seed/demo/PAGES.md documents and what the per-device receipt
		// counts above must add up to, and both drifted once already when
		// detections were added without either being updated. An exact count
		// fails the seed the moment the three stop agreeing, and the category
		// check guards that every kind of tool the catalog distinguishes is
		// represented on the page, since a category with no rows renders as an
		// empty filter.
		{
			cond:     `(SELECT count() FROM ai_detections WHERE organization_id = @org) <> 52`,
			settings: "",
			msg:      "demo seed postflight: expected exactly 52 demo AI detection rows, so update PAGES.md and the receipt match counts alongside any change",
		},
		{
			cond:     `(SELECT uniqExact(category) FROM ai_detections WHERE organization_id = @org) <> 3`,
			settings: "",
			msg:      "demo seed postflight: demo AI detections must cover harness, assistant and local_model",
		},
	}
}

// Postflight: the Explore datasets have tool calls to collapse,
// every demo user and both harnesses are represented, and cost is only ever
// stated where the provider states it.
func chAgentEventChecks() []chCheck {
	return []chCheck{
		{
			cond:     `(SELECT count() FROM agent_events WHERE organization_id = @org) < 2000`,
			settings: "",
			msg:      "demo seed postflight: expected >= 2000 demo agent_events rows",
		},
		{
			cond: `(SELECT uniqExact(user_email) FROM agent_events
   WHERE organization_id = @org) != 6
  OR (SELECT uniqExact(surface) FROM agent_events
      WHERE organization_id = @org) != 3`,
			settings: "",
			msg:      "demo seed postflight: demo agent events must cover all six users, both harnesses, and inference capture",
		},
		{
			cond: `(SELECT countIf(cost_usd > 0) FROM agent_events
   WHERE organization_id = @org AND surface = 'codex') != 0`,
			settings: "",
			msg:      "demo seed postflight: Codex states no cost, so no Codex demo row may carry one",
		},
		{
			cond: `(SELECT uniqExact(skill_name) FROM agent_events
   WHERE organization_id = @org AND skill_name != '') != 3
  OR (SELECT countIf(skill_name != '') FROM agent_events
      WHERE organization_id = @org AND surface = 'codex') != 0`,
			settings: "",
			msg:      "demo seed postflight: Claude tool rows name the three demo skills and Codex rows name none",
		},
	}
}

// Agent events: what Explore (the semantic catalog over agent_events) reads.
// 144 sessions over the trailing 12 days, two per user per day, dealt across
// the two harnesses the dialects know: users 1, 3 and 5 on Claude Code
// (anthropic, usage and cost stated on api_request rows), users 2, 4 and 6 on
// Codex (openai, tokens stated but no cost, which is what the provider
// emits). Each session has 2-5 turns, each turn a prompt, an api_request and
// an api_response (an api_error now and then), and 0-3 tool calls. A Claude
// tool call is a tool_decision followed by a tool_call_result unless the
// decision rejected it, so a blocked call is a decision alone; a Codex call is
// its result. Every row family has its own <seed>agent- prefix and occurred_at
// is relative to the run's start, so a reseed lands inside the window.

const (
	agentEventSessions = 144
	nanosPerSecond     = int64(1_000_000_000)
)

const agentEventInsert = `INSERT INTO agent_events
  (organization_id, project_id, occurred_at_unix_nano, observed_at_unix_nano,
   record_id, session_id, turn_id, event_id, event_type, raw_event_name,
   source, provider, surface, user_id, user_email, external_user_id,
   account_type, billing_mode, external_org_id, device_id,
   department_name, division_name, job_title, employee_type, cost_center_name,
   roles, groups, model, query_source, skill_name, agent_name,
   mcp_server_name, mcp_tool_name, tool_name, text, outcome, outcome_message,
   duration_nano, input_content, output_content,
   input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd,
   attributes, resource_attributes, scope_attributes)`

// agentEventRoles are the directory roles per demoUsers index. Directory
// attributes mirror the Postgres roster so dimensions agree across stores.
var agentEventRoles = []string{"support", "support", "engineering", "engineering", "billing", "engineering"}

var agentEventPrompts = []string{
	"Why is the refund job retrying every five minutes",
	"List the open incidents for the billing service and summarise each one",
	"Find every place the invoice total is rounded and check the currency",
	"Draft a runbook step for rotating the payments webhook secret",
	"Which customers hit the rate limit on the export endpoint this week",
	"Explain what the support-refunds skill does before I run it",
	"Check whether the nightly reconciliation finished and how long it took",
	"Summarise the last three deploys of the gateway and any rollbacks",
}

// agentSession is the per-session shape both agent_events inserts share.
type agentSession struct {
	user     demoUser
	idx      int // index into demoUsers
	start    int64
	turns    int
	onClaude bool
	surface  string
	provider string
	model    string
	id       string
}

func (s *seeder) agentSession(i int) agentSession {
	idx := i % 6
	dayBack := int64(i / 12)
	slot := int64((i % 12) / 6)
	start := s.now.UnixNano() -
		dayBack*86400*nanosPerSecond -
		(slot*5+2+int64((i*7)%3))*3600*nanosPerSecond -
		int64((i*13)%50)*60*nanosPerSecond
	// Users 1, 3 and 5 (1-based) are on Claude Code.
	onClaude := idx%2 == 0
	surface, provider := "codex", "openai"
	if onClaude {
		surface, provider = "claude-code", "anthropic"
	}
	model := "claude-sonnet-5"
	switch {
	case !onClaude:
		model = "gpt-5-codex"
	case i%3 == 0:
		model = "claude-haiku-4-5-20251001"
	}
	return agentSession{
		user:     demoUsers[idx],
		idx:      idx,
		start:    start,
		turns:    2 + i%4,
		onClaude: onClaude,
		surface:  surface,
		provider: provider,
		model:    model,
		id:       s.det("agent-session-" + strconv.Itoa(i)).String(),
	}
}

// agentEventFields are the columns that vary per agent_events row; the rest
// are fixed by the session or empty.
type agentEventFields struct {
	occurred       int64
	recordID       string
	turnID         string
	eventID        string
	eventType      string
	rawEventName   string
	skillName      string
	mcpServerName  string
	mcpToolName    string
	toolName       string
	text           string
	outcome        string
	outcomeMessage string
	durationNano   int64
	inputTokens    int64
	outputTokens   int64
	cacheRead      int64
	cacheWrite     int64
	costUSD        float64
}

func (s *seeder) appendAgentEvent(batch driver.Batch, sess agentSession, f agentEventFields) error {
	rawPrefix := "codex."
	if sess.onClaude {
		rawPrefix = "claude_code."
	}
	if err := batch.Append(
		s.orgID(),
		s.projectID().String(),
		f.occurred,
		f.occurred+800_000_000,
		f.recordID,
		sess.id,
		f.turnID,
		f.eventID,
		f.eventType,
		rawPrefix+f.rawEventName,
		sess.surface, // source
		sess.provider,
		sess.surface,
		s.userID(sess.user.handle),
		s.email(sess.user.handle),
		"", // external_user_id
		"", // account_type
		"", // billing_mode
		"", // external_org_id
		sess.user.hostname,
		sess.user.department,
		sess.user.division,
		sess.user.title,
		sess.user.empType,
		sess.user.costCenter,
		[]string{agentEventRoles[sess.idx]},
		[]string{sess.user.team},
		sess.model,
		"", // query_source
		f.skillName,
		"", // agent_name
		f.mcpServerName,
		f.mcpToolName,
		f.toolName,
		f.text,
		f.outcome,
		f.outcomeMessage,
		f.durationNano,
		"", // input_content
		"", // output_content
		f.inputTokens,
		f.outputTokens,
		f.cacheRead,
		f.cacheWrite,
		f.costUSD,
		"{}",
		"{}",
		"{}",
	); err != nil {
		return fmt.Errorf("append agent event %s: %w", f.recordID, err)
	}
	return nil
}

// chInsertAgentTurnEvents writes the turn rows: a prompt, its API request and
// the response, per turn.
func (s *seeder) chInsertAgentTurnEvents(ctx context.Context, ch driver.Conn) error {
	batch, err := ch.PrepareBatch(ctx, agentEventInsert)
	if err != nil {
		return fmt.Errorf("prepare agent turn events: %w", err)
	}
	defer func() { _ = batch.Abort() }()

	for i := range agentEventSessions {
		sess := s.agentSession(i)
		for t := range sess.turns {
			turnStart := sess.start + int64(t)*95*nanosPerSecond
			failed := (i*t)%23 == 5
			inTokens := int64(1200 + (i*31+t*17)%2600)
			outTokens := int64(150 + (i*7+t*29)%900)
			cacheRead := int64(800 + (i*5)%3000)
			cacheWrite := int64(0)
			if t == 0 {
				cacheWrite = 1500
			}
			responseNano := int64(1500+(i*11+t*3)%9000) * 1_000_000
			suffix := strconv.Itoa(i) + "-" + strconv.Itoa(t)
			turnID := "prompt_demo_" + strconv.Itoa(i) + "_" + strconv.Itoa(t)
			reqID := "req_demo_" + strconv.Itoa(i) + "_" + strconv.Itoa(t)

			responseType, outcome, outcomeMessage := "api_response", "ok", ""
			if failed {
				responseType, outcome = "api_error", "error"
				outcomeMessage = "overloaded_error: the model is busy, retry later"
			}

			var cost float64
			var reqCacheRead, reqCacheWrite int64
			if sess.onClaude {
				reqCacheRead, reqCacheWrite = cacheRead, cacheWrite
				// Same evaluation order as the old SQL so the Float64 is
				// bit-identical.
				if sess.model == "claude-sonnet-5" {
					cost = (float64(inTokens*3+outTokens*15) + float64(cacheRead)*0.3 + float64(cacheWrite)*3.75) / 1000000
				} else {
					cost = (float64(inTokens)*0.8 + float64(outTokens*4) + float64(cacheRead)*0.08 + float64(cacheWrite*1)) / 1000000
				}
			}

			rows := []agentEventFields{
				{
					occurred:       turnStart,
					recordID:       s.name("agent-prompt-" + suffix),
					turnID:         turnID,
					eventID:        s.name("agent-prompt-" + suffix),
					eventType:      "prompt",
					rawEventName:   "user_prompt",
					skillName:      "",
					mcpServerName:  "",
					mcpToolName:    "",
					toolName:       "",
					text:           agentEventPrompts[(i+t)%8],
					outcome:        "",
					outcomeMessage: "",
					durationNano:   0,
					inputTokens:    0,
					outputTokens:   0,
					cacheRead:      0,
					cacheWrite:     0,
					costUSD:        0,
				},
				{
					occurred:       turnStart + 2*nanosPerSecond,
					recordID:       s.name("agent-request-" + suffix),
					turnID:         turnID,
					eventID:        reqID,
					eventType:      "api_request",
					rawEventName:   "api_request",
					skillName:      "",
					mcpServerName:  "",
					mcpToolName:    "",
					toolName:       "",
					text:           "",
					outcome:        "",
					outcomeMessage: "",
					durationNano:   0,
					inputTokens:    inTokens,
					outputTokens:   outTokens,
					cacheRead:      reqCacheRead,
					cacheWrite:     reqCacheWrite,
					costUSD:        cost,
				},
				{
					occurred:       turnStart + 2*nanosPerSecond + responseNano,
					recordID:       s.name("agent-response-" + suffix),
					turnID:         turnID,
					eventID:        reqID,
					eventType:      responseType,
					rawEventName:   responseType,
					skillName:      "",
					mcpServerName:  "",
					mcpToolName:    "",
					toolName:       "",
					text:           "",
					outcome:        outcome,
					outcomeMessage: outcomeMessage,
					durationNano:   responseNano,
					inputTokens:    0,
					outputTokens:   0,
					cacheRead:      0,
					cacheWrite:     0,
					costUSD:        0,
				},
			}
			for _, f := range rows {
				if err := s.appendAgentEvent(batch, sess, f); err != nil {
					return err
				}
			}
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert agent turn events: %w", err)
	}
	return nil
}

var (
	agentEventTools    = []string{"Bash", "Read", "Grep", "Glob", "Edit", "mcp_tool"}
	agentEventSkills   = []string{"review-pr", "write-tests", "release-notes"}
	agentEventMCPTools = []string{"lookup_customer", "process_refund", "list_invoices"}
)

// chInsertAgentToolEvents writes the tool rows: 0-3 calls per turn. A Claude
// call is a decision then a result (or the rejecting decision alone); a Codex
// call is its result. A share of the Claude calls are Skill invocations naming
// one of three skills, which is what fills the skills dataset; Codex reports
// no skills.
func (s *seeder) chInsertAgentToolEvents(ctx context.Context, ch driver.Conn) error {
	batch, err := ch.PrepareBatch(ctx, agentEventInsert)
	if err != nil {
		return fmt.Errorf("prepare agent tool events: %w", err)
	}
	defer func() { _ = batch.Abort() }()

	for i := range agentEventSessions {
		sess := s.agentSession(i)
		for t := range sess.turns {
			turnStart := sess.start + int64(t)*95*nanosPerSecond
			tools := (i + t) % 4
			for k := range tools {
				pick := i*3 + t*5 + k*7
				tool := agentEventTools[pick%6]
				if sess.onClaude && pick%7 == 6 {
					tool = "Skill"
				}
				skill := ""
				if tool == "Skill" {
					skill = agentEventSkills[(i+t+k)%3]
				}
				mcpServer, mcpTool := "", ""
				if tool == "mcp_tool" {
					mcpServer, mcpTool = "acme-crm", agentEventMCPTools[(i+k)%3]
				}
				rejected := (i*5+t+k)%25 == 0
				errored := (i*3+t*7+k)%17 == 0
				toolNano := int64(200+(i*37+t*11+k*5)%5800) * 1_000_000
				callStart := turnStart + 15*nanosPerSecond + int64(k)*20*nanosPerSecond
				suffix := strconv.Itoa(i) + "-" + strconv.Itoa(t) + "-" + strconv.Itoa(k)
				turnID := "prompt_demo_" + strconv.Itoa(i) + "_" + strconv.Itoa(t)
				eventID := "call_demo_agent_" + strconv.Itoa(i) + "_" + strconv.Itoa(t) + "_" + strconv.Itoa(k)

				// Codex has no decision events, and a rejected Claude call has
				// no result.
				if sess.onClaude {
					outcome, outcomeMessage := "ok", ""
					if rejected {
						outcome, outcomeMessage = "rejected", "blocked by policy: write outside the checkout"
					}
					if err := s.appendAgentEvent(batch, sess, agentEventFields{
						occurred:       callStart,
						recordID:       s.name("agent-decision-" + suffix),
						turnID:         turnID,
						eventID:        eventID,
						eventType:      "tool_decision",
						rawEventName:   "tool_decision",
						skillName:      skill,
						mcpServerName:  mcpServer,
						mcpToolName:    mcpTool,
						toolName:       tool,
						text:           "",
						outcome:        outcome,
						outcomeMessage: outcomeMessage,
						durationNano:   0,
						inputTokens:    0,
						outputTokens:   0,
						cacheRead:      0,
						cacheWrite:     0,
						costUSD:        0,
					}); err != nil {
						return err
					}
				}
				if sess.onClaude && rejected {
					continue
				}
				outcome, outcomeMessage := "ok", ""
				if errored {
					outcome, outcomeMessage = "error", "exit status 1"
				}
				if err := s.appendAgentEvent(batch, sess, agentEventFields{
					occurred:       callStart + toolNano,
					recordID:       s.name("agent-result-" + suffix),
					turnID:         turnID,
					eventID:        eventID,
					eventType:      "tool_call_result",
					rawEventName:   "tool_result",
					skillName:      skill,
					mcpServerName:  mcpServer,
					mcpToolName:    mcpTool,
					toolName:       tool,
					text:           "",
					outcome:        outcome,
					outcomeMessage: outcomeMessage,
					durationNano:   toolNano,
					inputTokens:    0,
					outputTokens:   0,
					cacheRead:      0,
					cacheWrite:     0,
					costUSD:        0,
				}); err != nil {
					return err
				}
			}
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert agent tool events: %w", err)
	}
	return nil
}

// chInsertMCPNetworkRequests writes dedicated source events for the inbound
// MCP HTTP traffic summary. Keep both server kinds and surfaces represented,
// with synthetic IDs matching existing demo MCP server and gateway fixtures.
// Four hourly slots per (server kind, surface) pair: 16 rows.
func (s *seeder) chInsertMCPNetworkRequests(ctx context.Context, ch driver.Conn) error {
	batch, err := ch.PrepareBatch(ctx, `INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name)`)
	if err != nil {
		return fmt.Errorf("prepare mcp network requests: %w", err)
	}
	defer func() { _ = batch.Abort() }()

	proj := s.projectID()
	targets := []struct {
		kind, surface, serverID string
	}{
		{"mcp", "public", s.det("mcpserver-support").String()},
		{"mcp", "private", s.det("mcpserver-ops").String()},
		{"meta", "public", s.det("metamcp-1").String()},
		{"meta", "private", s.det("metamcp-1").String()},
	}
	resource := `{"gram.deployment.id":"` + s.spec.Marker + `"}`
	for n := range 4 {
		nano := s.now.AddDate(0, 0, -1).Add(time.Duration(8+n) * time.Hour).UnixNano()
		for _, tg := range targets {
			idKey := "gram.meta_mcp_server.id"
			if tg.kind == "mcp" {
				idKey = "gram.mcp_server.id"
			}
			attrs := `{"gram.event.urn":"urn:telemetry:gram_service:log:mcp_network_request"` +
				`,"gram.network.surface":"` + tg.surface + `"` +
				`,"` + idKey + `":"` + tg.serverID + `"` +
				`,"gram.project.id":"` + proj.String() + `"}`
			if err := batch.Append(
				nano,
				nano,
				"INFO",
				"Inbound MCP HTTP request",
				md5Hex(s.name("mcp-network-"+tg.kind+"-"+tg.surface)),
				attrs,
				resource,
				proj,
				"mcp_network_request",
				"gram-mcp-gateway",
			); err != nil {
				return fmt.Errorf("append mcp network request: %w", err)
			}
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert mcp network requests: %w", err)
	}
	return nil
}
