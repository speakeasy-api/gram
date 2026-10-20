package demoseed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// chPurgeAndTelemetry regenerates the tenant's telemetry, ClickHouse side.
//
// Every statement is scoped to the tenant's project id / org id
// (telemetry_logs has no org column; the project ids ARE the isolation
// boundary — they must match the Postgres seed).
//
// The MVs (trace/metrics/attribute_metrics/chat_token/chat_session summaries,
// attribute_keys, spend_rule_usage) fire on INSERT, and a DELETE on
// telemetry_logs never shrinks their targets — so each target is deleted
// explicitly below before the fresh insert repopulates it through the MVs.
// Rows are inserted with recent timestamps (trailing ~12 days), safely past
// every MV date cutoff, so NO hand-written MV backfill is needed here.
//
// PROVENANCE MATTERS: attribute_metrics_summaries_mv and
// chat_session_summaries_mv are provenance-first — they admit ONLY rows from
// observed agent surfaces. Generic "chat:completion" rows are deliberately
// excluded. The inserts below emit:
//   - odd-numbered chats  → Claude provenance: claude-code:otel:logs
//     api_request rows (usage/cost, per-turn prompt.id) + tool_result rows
//     (tool calls, tool_use_id call_demo_<i>_<k>, payload sizes)
//   - even-numbered chats → Cursor provenance: cursor:usage:metrics rows
//     (usage/cost) + PostToolUse hook rows (tool calls, hook branch of the
//     insights CTE via gram.event.source=hook)
//   - every chat          → "tools:" urn rows with gram.toolset.slug so the
//     Insights direct branch classifies them as hosted MCP traffic
//   - odd chats           → one Skill hook row (Insights skill panels)
//   - every chat          → one chat_analysis:work_units:score row (Costs
//     "Efficiency" dataset)
//   - shadow MCP inventory + hook telemetry, authz challenges, and the
//     risk_findings mirror of the Postgres findings
//
// Every row also carries the WorkOS-style user identity attributes
// (user.attributes.*, user.roles, user.groups, gram.hook.hostname) — the cost
// page HIDES any pivot whose key never appears in attribute_keys, so these
// must be present, not merely non-empty.
//
// Chat ids reproduce the Postgres formula (s.chatID: md5 with version nibble
// '5', variant '8') so the ClickHouse telemetry joins the Postgres chats
// exactly. Trace ids are unique per surface (tooltrace/hooktrace/skilltrace/
// ...): trace_summaries collapses per trace_id, so sharing one trace across
// row types would merge them into a single unclassifiable trace.
//
// The old script opened with SET lightweight_deletes_sync = 1; runClickHouse
// puts lightweight_deletes_sync=2 on ctx instead (see clickhouse.go).
func (s *seeder) chPurgeAndTelemetry(ctx context.Context, ch driver.Conn) error {
	if err := s.chPurge(ctx, ch); err != nil {
		return err
	}
	if err := s.chTranscriptRows(ctx, ch); err != nil {
		return err
	}

	inserts := []struct {
		name string
		rows func() []chTelemetryRow
	}{
		{"hosted tool calls", s.chToolCallRows},
		{"unattributed agent tool calls", s.chUnattributedToolRows},
		{"unattributed person api requests", s.chUnattributedAPIRows},
		{"claude api requests", s.chClaudeAPIRows},
		{"claude tool results", s.chClaudeToolResultRows},
		{"skill hook rows", s.chSkillRows},
		{"mcp skill loads", s.chMCPSkillLoadRows},
		{"blocked hook rows", s.chBlockedRows},
		{"cursor usage metrics", s.chCursorUsageRows},
		{"cursor hook rows", s.chCursorHookRows},
		{"work units scores", s.chWorkUnitRows},
	}
	for _, ins := range inserts {
		if err := s.chInsertTelemetry(ctx, ch, ins.rows()); err != nil {
			return fmt.Errorf("insert demo %s: %w", ins.name, err)
		}
	}

	return s.chShadowInventory(ctx, ch)
}

// chPurgeProjectTables are deleted by gram_project_id: telemetry source +
// every MV target.
var chPurgeProjectTables = []string{
	"telemetry_logs",
	"trace_summaries",
	"metrics_summaries",
	"attribute_metrics_summaries",
	"chat_token_summaries",
	"chat_session_summaries",
	"spend_rule_usage_summaries",
	"attribute_keys",
	"shadow_mcp_inventory_urls",
	"mcp_network_traffic_hourly_summaries",
}

// chPurgeOrgTables are deleted by organization_id.
var chPurgeOrgTables = []string{
	"ai_detections",
	"ai_scan_receipts",
	"authz_challenges",
	// Target of authz_challenge_bucket_summaries_mv.
	"authz_challenge_bucket_summaries",
	"risk_findings",
	"skill_session_versions",
	"skill_efficacy_scores",
	"billing_meter_daily_summaries",
	"billing_meter_readings_by_time",
	"agent_events",
	"sigint_readings",
	"sigint_readings_v2",
}

// chPurge runs the scoped deletes: telemetry source + every MV target + the
// org-keyed tables. Lightweight DELETEs, not ALTER TABLE ... DELETE: a heavy
// mutation on telemetry_logs rewrites every column of every part that contains
// demo rows (~12 days of shared, all-tenant partitions — the partition key is
// time-only, so project id cannot prune), which previously starved merges in
// prod. A lightweight delete only writes the _row_exists mask for matching
// parts and hides the rows as soon as the statement returns; physical cleanup
// happens in background merges.
func (s *seeder) chPurge(ctx context.Context, ch driver.Conn) error {
	projectID := s.projectID().String()
	for _, table := range chPurgeProjectTables {
		if err := ch.Exec(ctx, "DELETE FROM "+table+" WHERE gram_project_id IN (toUUID(?))", projectID); err != nil {
			return fmt.Errorf("delete demo %s rows: %w", table, err)
		}
	}
	for _, table := range chPurgeOrgTables {
		if err := ch.Exec(ctx, "DELETE FROM "+table+" WHERE organization_id = ?", s.orgID()); err != nil {
			return fmt.Errorf("delete demo %s rows: %w", table, err)
		}
	}

	// Inserts must never race rows from the previous seed generation: poll
	// until the deletes are visible before advancing past the delete phase.
	countRemaining := func(queryCtx context.Context) (uint64, error) {
		var remaining uint64
		err := ch.QueryRow(queryCtx, `
			SELECT
				(SELECT count() FROM telemetry_logs WHERE gram_project_id = toUUID(?))
				+ (SELECT count() FROM billing_meter_readings_by_time WHERE organization_id = ?)
				+ (SELECT count() FROM billing_meter_daily_summaries WHERE organization_id = ?)
				+ (SELECT count() FROM sigint_readings WHERE organization_id = ?)
				+ (SELECT count() FROM sigint_readings_v2 WHERE organization_id = ?)
		`, projectID, s.orgID(), s.orgID(), s.orgID(), s.orgID()).Scan(&remaining)
		if err != nil {
			return 0, fmt.Errorf("count remaining demo telemetry, meter ledger, summary, and sensor reading rows: %w", err)
		}
		return remaining, nil
	}
	if err := waitForClickHouseDelete(ctx, clickHouseDeleteVisibilityTimeout, clickHouseDeleteVisibilityPoll, countRemaining); err != nil {
		return err
	}

	// Preflight: keep the invariant explicit even after the wait.
	remaining, err := countRemaining(ctx)
	if err != nil {
		return err
	}
	if remaining != 0 {
		return errors.New("demo seed preflight: source or summary rows remain after scoped deletes")
	}
	var agentEvents uint64
	if err := ch.QueryRow(ctx, "SELECT count() FROM agent_events WHERE organization_id = ?", s.orgID()).Scan(&agentEvents); err != nil {
		return fmt.Errorf("count remaining demo agent_events rows: %w", err)
	}
	if agentEvents != 0 {
		return errors.New("demo seed preflight: agent_events rows remain after the scoped delete")
	}
	return nil
}

// chTelemetryRow is one telemetry_logs row. traceID and chatID are nil where
// the old INSERT left the column out (NULL).
type chTelemetryRow struct {
	timeNano     int64
	observedNano int64
	severity     string
	body         string
	traceID      *string
	attributes   string
	resource     string
	urn          string
	service      string
	chatID       *string
}

func (s *seeder) chInsertTelemetry(ctx context.Context, ch driver.Conn, rows []chTelemetryRow) error {
	batch, err := ch.PrepareBatch(ctx, `INSERT INTO telemetry_logs
		(time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
		 attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)`)
	if err != nil {
		return fmt.Errorf("prepare telemetry_logs batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	for _, r := range rows {
		if err := batch.Append(
			r.timeNano, r.observedNano, r.severity, r.body, r.traceID,
			r.attributes, r.resource, s.projectID(), r.urn, r.service, r.chatID,
		); err != nil {
			return fmt.Errorf("append telemetry_logs row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send telemetry_logs batch: %w", err)
	}
	return nil
}

// chTrace is lower(hex(MD5(<seed><name>))), the trace id formula.
func (s *seeder) chTrace(name string) *string { return new(md5Hex(s.name(name))) }

// chResource is the resource attributes every row carries: the marker the
// ClickHouse postflight leak-checks.
func (s *seeder) chResource() string {
	return `{"gram.deployment.id":"` + s.spec.Marker + `"}`
}

func (s *seeder) chClaudeResource() string {
	return `{"service.name":"claude-code","gram.deployment.id":"` + s.spec.Marker + `"}`
}

// chChatOwnerIdx is the 0-based demoUsers index of chat n's owner (the old
// script's uidx minus one); s.chatOwner(n) is demoUsers[chChatOwnerIdx(n)].
func (s *seeder) chChatOwnerIdx(n int) int {
	return chatOwnerDraw[hashByte(s.chatName(n), 12)%16]
}

// chChatByte reads byte off of md5(<seed>chat-<n>), the script's
// reinterpretAsUInt8(unhex(substring(h, off+1, 2))).
func (s *seeder) chChatByte(n, off int) int { return hashByte(s.chatName(n), off) }

// chRolesJSON is each demo user's user.roles JSON, indexed like demoUsers.
var chRolesJSON = []string{
	`["developer","viewer"]`, `["developer"]`, `["admin","developer"]`,
	`["developer"]`, `["analyst","viewer"]`, `["admin","viewer"]`,
}

// chIdentityAttrs is the WorkOS-style identity block (division through
// hostname) a row carries for demo user idx.
func chIdentityAttrs(idx int) string {
	u := demoUsers[idx]
	return `,"user.attributes.division_name":"` + u.division + `"` +
		`,"user.attributes.department_name":"` + u.department + `"` +
		`,"user.attributes.job_title":"` + u.title + `"` +
		`,"user.attributes.employee_type":"` + u.empType + `"` +
		`,"user.attributes.cost_center_name":"` + u.costCenter + `"` +
		`,"user.roles":` + chRolesJSON[idx] +
		`,"user.groups":["` + u.team + `"]` +
		`,"gram.hook.hostname":"` + u.hostname + `"`
}

// chPersonalAccount reports whether chat i ran through a personal account:
// the contractor works entirely on his own subscription; the manager splits,
// roughly a third of his chats going through the personal Claude login
// user_accounts already lists for him. A person who is wholly one or wholly
// the other never exercises the account filter on the identity Usage tab,
// which exists precisely for the split case.
func chPersonalAccount(handle string, i int) bool {
	return handle == "mateo" || (handle == "lucas" && hash64("acct", i)%3 == 0)
}

func chIf(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func chItoa[T int | uint64](v T) string { return strconv.FormatUint(uint64(v), 10) }

// chRound mirrors ClickHouse round(x, places) and toString of the Float64.
func chRound(x float64, places int) string {
	p := math.Pow(10, float64(places))
	return strconv.FormatFloat(math.RoundToEven(x*p)/p, 'f', -1, 64)
}

// chStartOfDay is toStartOfDay(now()) on a UTC server.
func (s *seeder) chStartOfDay() time.Time { return s.now.UTC().Truncate(24 * time.Hour) }

// chTranscriptRows writes the transcript-only inference capture (counts as a
// chat without fabricating usage), then derives its agent_events rows from
// the telemetry_logs rows just inserted.
func (s *seeder) chTranscriptRows(ctx context.Context, ch driver.Conn) error {
	chatID := s.det("anthropic-inference-chat").String()
	messages := []string{"anthropic-inference-prompt", "anthropic-inference-reply", "anthropic-inference-followup"}
	rows := make([]chTelemetryRow, 0, len(messages))
	for number, msg := range messages {
		rows = append(rows, chTelemetryRow{
			timeNano:     s.now.Add(-time.Duration(20-number) * time.Minute).UnixNano(),
			observedNano: s.now.UnixNano(),
			severity:     "INFO",
			body:         "",
			traceID:      nil,
			attributes: `{"gram.event.source":"hook","gram.hook.source":"claude-chat-web","gram.hook.event":"session.observed",` +
				`"gen_ai.conversation.id":"` + chatID + `",` +
				`"gram.chat.message.id":"` + s.det(msg).String() + `",` +
				`"user.id":"` + s.userID("amara") + `","user.email":"` + s.email("amara") + `",` +
				`"gen_ai.response.model":"claude-sonnet-4-6","gen_ai.provider.name":"anthropic"}`,
			resource: s.chResource(),
			urn:      "chat:transcript:observed",
			service:  "gram-server",
			chatID:   new(chatID),
		})
	}
	if err := s.chInsertTelemetry(ctx, ch, rows); err != nil {
		return fmt.Errorf("insert demo transcript capture: %w", err)
	}

	if err := ch.Exec(ctx, `
		INSERT INTO agent_events
		  (organization_id, project_id, occurred_at_unix_nano, observed_at_unix_nano,
		   record_id, session_id, event_id, raw_event_name, source, surface,
		   user_id, user_email, provider, model, attributes, resource_attributes, scope_attributes)
		SELECT ?, toString(gram_project_id), time_unix_nano, observed_time_unix_nano,
		       concat('transcript:', toString(attributes.gram.chat.message.id)), chat_id,
		       concat('transcript:', toString(attributes.gram.chat.message.id)), 'session.observed',
		       'transcript', hook_source, user_id, user_email, 'anthropic',
		       toString(attributes.gen_ai.response.model), '{}', '{}', '{}'
		FROM telemetry_logs
		WHERE gram_project_id = toUUID(?)
		  AND gram_urn = 'chat:transcript:observed'`,
		s.orgID(), s.projectID().String(),
	); err != nil {
		return fmt.Errorf("insert demo transcript agent_events: %w", err)
	}
	return nil
}

// MCP clients, 1-based like the script's cidx: Claude Code, claude-ai,
// Cursor, Visual Studio Code.
var (
	chClientNames = []string{"Claude Code", "claude-ai", "Cursor", "Visual Studio Code"}
	// The MCP client follows the harness the chat ran in: a Claude Code chat
	// calls from Claude Code or the Claude web app, a Cursor chat from Cursor
	// or the VS Code extension. A client drawn independently of the harness
	// would make "top clients" and "top agents" disagree for no reason.
	chClaudeClientDraw = [16]int{1, 1, 1, 1, 1, 1, 1, 2, 1, 1, 2, 1, 1, 2, 1, 1}
	chCursorClientDraw = [16]int{3, 3, 3, 3, 3, 4, 3, 3, 4, 3, 3, 3, 4, 3, 3, 4}
	// A real fleet is spread over a few releases, weighted toward the newest.
	chClientVersions = [][5]string{
		{"2.4.1", "2.4.1", "2.4.1", "2.3.8", "2.2.0"},
		{"1.0.0", "1.0.0", "1.0.0", "1.0.0", "1.0.0"},
		{"1.7.42", "1.7.42", "1.7.39", "1.6.14", "1.6.14"},
		{"1.104.2", "1.104.2", "1.103.1", "1.103.1", "1.102.0"},
	}
	// Each client reaches for a different part of the catalog, so "most used
	// tools by client" is a real breakdown rather than the same eight tools
	// in the same proportions under every client. Values are 1-based
	// toolNames indexes.
	chClientToolSlots = [][16]int{
		{1, 1, 2, 3, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4, 7, 8},
		{4, 4, 4, 6, 6, 6, 2, 2, 4, 6, 4, 6, 4, 6, 2, 4},
		{1, 1, 1, 2, 2, 3, 3, 3, 7, 7, 8, 1, 2, 3, 7, 8},
		{2, 2, 4, 4, 5, 5, 5, 8, 8, 2, 4, 5, 8, 2, 5, 8},
	}
)

// chClientIdx is chat i's 1-based MCP client: the Claude pair for odd (Claude
// Code) chats, the Cursor pair for even ones.
func (s *seeder) chClientIdx(i int) int {
	b := s.chChatByte(i, 14) % 16
	if i%2 == 1 {
		return chClaudeClientDraw[b]
	}
	return chCursorClientDraw[b]
}

// chToolName is the tool call k of chat i made, by the client-aware draw.
// The hosted tool call and the Claude tool_result / Cursor hook row
// correlated with it by call_demo_<i>_<k> share it; drawing independently
// would give one call two different tool names depending on which row you
// read it from.
func (s *seeder) chToolName(i, k int) string {
	cidx := s.chClientIdx(i)
	return toolNames[chClientToolSlots[cidx-1][hash64("toolslot", i-1, k)%16]-1]
}

// chToolCallRows are the tool-execution rows: 3-12 per chat (hash-picked, so
// busy chats and quick ones both exist). gram.toolset.slug makes the Insights
// CTE's direct branch classify each trace as hosted MCP traffic; unique
// per-call trace ids keep trace_summaries from merging them. Failures cluster
// into a process_refund incident over the last ~3 days plus a low background
// rate (~5% overall) instead of an evenly spread modulus.
func (s *seeder) chToolCallRows() []chTelemetryRow {
	var rows []chTelemetryRow
	for i := 1; i <= bulkChats; i++ {
		calls := 3 + s.chChatByte(i, 8)%10
		uidx := s.chChatOwnerIdx(i)
		u := demoUsers[uidx]
		email := s.email(u.handle)
		chatID := s.chatID(i).String()
		nano := s.chatTS(i).UnixNano()
		hook := chIf(i%2 == 1, "claude-code", "cursor")
		cidx := s.chClientIdx(i)
		clientName := chClientNames[cidx-1]
		clientVersion := chClientVersions[cidx-1][s.chChatByte(i, 16)%5]
		clientSeen := hash64("cliseen", i-1)%8 > 0
		managedAgentID := s.det("managed-agent-" + strconv.Itoa(1+i%3)).String()
		dayOff := s.chatDayOff(i)

		for k := 1; k <= calls; k++ {
			toolName := s.chToolName(i, k)
			// One predicate for the status code and the result payload: a
			// call that returns 500 must not also carry a success body.
			failed := (toolName == "process_refund" && dayOff <= 2 && hash64("err", i, k)%2 == 0) ||
				hash64("errbg", i, k)%30 == 0
			// Short per-call ids so two calls to the same tool do not read as
			// one request replayed.
			ioID := md5Hex(s.name(fmt.Sprintf("toolio-%d-%d", i, k)))[:8]
			ioMag := hash64("iomag", i, k) % 900

			ts := nano + int64(k)*75000000000 + int64(hash64("gap", i, k)%45000000000)

			attrs := `{"gram.tool.urn":"` + toolURN(toolName) + `"` +
				`,"gram.tool.name":"` + toolName + `"`
			// Managed-agent calls retain the approving human email too: the
			// actor must win.
			if i%4 == 0 {
				attrs += `,"gram.event.source":"tool_call","gram.authorization.actor.type":"agent","gram.authorization.actor.id":"` + managedAgentID + `"`
			}
			attrs += `,"gram.toolset.slug":"` + chIf(i%5 == 0, "acme-ops", "acme-support-tools") + `"` +
				`,"http.response.status_code":` + chIf(failed, "500", "200") +
				`,"http.server.request.duration":` + chRound(0.05+float64(hash64(i, k)%200)/100, 3) +
				// The arguments and the result, as a real tool call records
				// them. Without these the log detail sheet has nothing to show
				// but the tool's name, and reports the payload as withheld by
				// tool_io_logs.
				`,"gen_ai.tool.call.arguments":"` + chToolArgs(toolName, ioID, ioMag) + `"` +
				`,"gen_ai.tool.call.result":"` + chToolResult(toolName, failed, ioID, ioMag) + `"` +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.email":"` + email + `"` +
				chIdentityAttrs(uidx) +
				// The account the call was made through, matching the chat
				// rows below so an account-scoped read of someone's usage
				// keeps its tool calls; without it every tool count drops to
				// zero the moment a filter is applied. Only the Claude half
				// can be personal: the two seeded personal accounts are
				// Anthropic ones, so labelling a Cursor call personal would
				// attribute it to an account that does not exist.
				`,"gram.account_type":"` + chIf(hook == "claude-code" && chPersonalAccount(u.handle, i), "personal", "team") + `"` +
				`,"gram.hook.source":"` + hook + `"`
			// What the caller said it was at the MCP initialize handshake.
			// Sessions that predate the handshake being recorded report
			// nothing, which is why a slice here carries no client at all:
			// the dashboard folds those into "unattributed", and a demo where
			// every call is attributed would hide that bucket entirely.
			if clientSeen {
				attrs += `,"gram.mcp.client.name":"` + clientName + `"` +
					`,"gram.mcp.client.version":"` + clientVersion + `"`
			}
			attrs += `}`

			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "Tool call: " + toolName,
				traceID:      s.chTrace(fmt.Sprintf("tooltrace-%d-%d", i, k)),
				attributes:   attrs,
				resource:     s.chResource(),
				urn:          toolURN(toolName),
				service:      "gram-mcp-gateway",
				chatID:       new(chatID),
			})
		}
	}
	return rows
}

// chToolArgs is a tool call's arguments, written per tool. The JSON quotes
// are escaped because these land INSIDE the attributes JSON document, as
// string values.
func chToolArgs(toolName, ioID string, ioMag uint64) string {
	switch toolName {
	case "search_logs":
		return `{\"query\":\"level:error service:payments-api\",\"limit\":100}`
	case "get_metrics":
		return `{\"metric\":\"http.server.request.duration\",\"service\":\"payments-api\",\"window\":\"15m\"}`
	case "query_db":
		return `{\"sql\":\"SELECT id, status FROM refunds WHERE created_at > now() - interval 1 day LIMIT 50\"}`
	case "get_customer":
		return `{\"customer_id\":\"cus_` + ioID + `\"}`
	case "list_deploys":
		return `{\"service\":\"payments-api\",\"limit\":10}`
	case "process_refund":
		return `{\"order_id\":\"ord_` + ioID + `\",\"amount_cents\":` + chItoa(1200+ioMag*7) + `,\"reason\":\"duplicate_charge\"}`
	case "fetch_traces":
		return `{\"trace_id\":\"` + ioID + ioID + `\",\"service\":\"payments-api\"}`
	default:
		return `{\"service\":\"payments-api\"}`
	}
}

// chToolResult is a tool call's result, escaped like chToolArgs.
func chToolResult(toolName string, failed bool, ioID string, ioMag uint64) string {
	switch {
	case failed && toolName == "process_refund":
		return `{\"error\":\"refund declined by the payment processor\",\"code\":\"processor_declined\",\"order_id\":\"ord_` + ioID + `\"}`
	case failed:
		return `{\"error\":\"upstream returned 500 after ` + chItoa(1+ioMag%3) + ` retries\",\"code\":\"upstream_error\"}`
	case toolName == "search_logs":
		return `{\"matches\":` + chItoa(ioMag) + `,\"truncated\":` + chIf(ioMag > 500, "true", "false") + `}`
	case toolName == "get_metrics":
		return `{\"p50_ms\":` + chItoa(20+ioMag%80) + `,\"p95_ms\":` + chItoa(200+ioMag) + `,\"error_rate\":0.0` + chItoa(ioMag%9) + `}`
	case toolName == "query_db":
		return `{\"rows\":` + chItoa(ioMag%50) + `,\"elapsed_ms\":` + chItoa(8+ioMag%120) + `}`
	case toolName == "get_customer":
		return `{\"customer_id\":\"cus_` + ioID + `\",\"plan\":\"enterprise\",\"status\":\"active\"}`
	case toolName == "list_deploys":
		return `{\"deploys\":` + chItoa(1+ioMag%9) + `,\"latest\":\"v2026.9.` + chItoa(ioMag%40) + `\"}`
	case toolName == "process_refund":
		return `{\"refund_id\":\"ref_` + ioID + `\",\"status\":\"succeeded\",\"amount_cents\":` + chItoa(1200+ioMag*7) + `}`
	case toolName == "fetch_traces":
		return `{\"spans\":` + chItoa(3+ioMag%40) + `,\"root_service\":\"payments-api\"}`
	default:
		return `{\"status\":\"healthy\",\"checks_passed\":` + chItoa(4+ioMag%5) + `}`
	}
}

// chUnattributedToolRows are an agent that reports only an id for itself, and
// no address at all. The Identities roster classifies a subject by what it
// can be keyed on, so without this the demo org has no Agent row to show.
// Surfaced from the raw logs rather than the agent-metrics view, which is
// keyed by email. Cleaned up by the same project-scoped telemetry_logs delete
// as every other row here.
func (s *seeder) chUnattributedToolRows() []chTelemetryRow {
	const actor = "svc-nightly-triage"
	tools := []string{"search_logs", "get_metrics", "fetch_traces", "check_health"}
	rows := make([]chTelemetryRow, 0, 12)
	for number := range 12 {
		i := number + 1
		toolName := tools[hash64("unattr", number)%4]
		ts := s.chStartOfDay().AddDate(0, 0, -(1 + number%6)).Add(time.Duration(9+number%8) * time.Hour).UnixNano()
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "INFO",
			body:         "Tool call: " + toolName,
			traceID:      s.chTrace("unattributed-" + strconv.Itoa(i)),
			attributes: `{"gram.tool.urn":"` + toolURN(toolName) + `"` +
				`,"gram.tool.name":"` + toolName + `"` +
				`,"gram.toolset.slug":"acme-support-tools"` +
				`,"http.response.status_code":200` +
				`,"http.server.request.duration":0.42` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.id":"` + actor + `"` +
				// Also the external user id: that is the key every identity
				// read path filters a non-directory actor by (external:<id>
				// resolves to exactly this), so reporting only user.id leaves
				// the agent's own page empty while the roster still lists it.
				`,"gram.external_user.id":"` + actor + `"` +
				`,"gram.hook.source":"codex"}`,
			resource: s.chResource(),
			urn:      toolURN(toolName),
			service:  "gram-mcp-gateway",
			chatID:   nil,
		})
	}
	return rows
}

// chUnattributedAPIRows are a person the directory has never heard of: an
// address that matches no member, so the roster can show an Unattributed row
// beside the members and the agent. It has to be an api_request row rather
// than a tool call because the agent-metrics view only admits the agent
// surfaces, and that view is what the roster reads for email-keyed
// identities.
func (s *seeder) chUnattributedAPIRows() []chTelemetryRow {
	// In the tenant's email domain, but deliberately not one of the seeded
	// members: that mismatch is the whole point.
	actor := s.email("ana.vidal")
	rows := make([]chTelemetryRow, 0, 12)
	for number := range 12 {
		i := number + 1
		respID := "msg_" + md5Hex(s.name("unattributed-resp-" + strconv.Itoa(i)))[:24]
		chatID := s.det("unattributed-chat-" + strconv.Itoa(i)).String()
		ts := s.chStartOfDay().AddDate(0, 0, -(1 + number%5)).Add(time.Duration(10+number%6) * time.Hour).UnixNano()
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "INFO",
			body:         "claude_code.api_request",
			traceID:      s.chTrace("unattributed-api-" + strconv.Itoa(i)),
			attributes: `{"prompt.id":"demo-unattributed-prompt-` + strconv.Itoa(i) + `"` +
				`,"event.name":"api_request"` +
				`,"gen_ai.response.id":"` + respID + `"` +
				`,"input_tokens":` + strconv.Itoa(2200+(i*37)%900) +
				`,"output_tokens":` + strconv.Itoa(180+(i*11)%220) +
				`,"cache_read_tokens":` + strconv.Itoa(9000+(i*53)%4000) +
				`,"cache_creation_tokens":600` +
				`,"cost_usd":0.1841` +
				`,"model":"claude-sonnet-4-6"` +
				`,"query_source":"user"` +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.email":"` + actor + `"` +
				`,"gram.external_user.id":"` + actor + `"` +
				`,"gram.hook.source":"claude-code"}`,
			resource: s.chResource(),
			urn:      "claude-code:otel:logs",
			service:  "claude-code",
			chatID:   new(chatID),
		})
	}
	return rows
}

// chClaudeAPIRows are Claude provenance (odd chats): one
// claude_code.api_request row per turn. prompt.id demo-prompt-<i>-<turn>
// joins the Postgres user messages; the skill/agent/mcp attribution keys
// light up the Costs Skills/Subagents/MCP datasets (only read on api_request
// rows).
func (s *seeder) chClaudeAPIRows() []chTelemetryRow {
	skills := []string{"", "triage-incident", "support-refunds"}
	agents := []string{"", "explore", "general-purpose"}
	mcpTools := []string{"search_logs", "get_customer", "process_refund"}
	var rows []chTelemetryRow
	for i := 1; i <= bulkChats; i += 2 {
		number := i - 1
		uidx := s.chChatOwnerIdx(i)
		u := demoUsers[uidx]
		email := s.email(u.handle)
		chatID := s.chatID(i).String()
		nano := s.chatTS(i).UnixNano()
		model := chIf(hash64("model", number)%3 == 0, "claude-opus-4-5", "claude-sonnet-4-6")
		personal := chPersonalAccount(u.handle, i)
		for turn := 1; turn <= 3; turn++ {
			inTok := 500000 + hash64("in", number, turn)%3000000
			outTok := 2000 + hash64("out", number, turn)%28000
			ts := nano + int64(turn)*60000000000
			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "claude_code.api_request",
				traceID:      s.chTrace(fmt.Sprintf("apitrace-%d-%d", i, turn)),
				attributes: `{"prompt.id":"demo-prompt-` + strconv.Itoa(i) + `-` + strconv.Itoa(turn) + `"` +
					`,"event.name":"api_request"` +
					`,"gen_ai.response.id":"msg_` + md5Hex(s.name(fmt.Sprintf("respid-%d-%d", i, turn)))[:24] + `"` +
					`,"input_tokens":` + chItoa(inTok) +
					`,"output_tokens":` + chItoa(outTok) +
					`,"cache_read_tokens":` + chItoa(inTok*6) +
					`,"cache_creation_tokens":` + chItoa(inTok/4) +
					`,"cost_usd":` + chRound(float64(inTok*57375+outTok*150000)/10000000000, 4) +
					`,"model":"` + model + `"` +
					`,"query_source":"user"` +
					`,"skill.name":"` + skills[hash64("skill", i, turn)%3] + `"` +
					`,"agent.name":"` + agents[hash64("agent", i, turn)%3] + `"` +
					`,"mcp_server.name":"acme-support-mcp"` +
					`,"mcp_tool.name":"` + mcpTools[hash64("mtool", i, turn)%3] + `"` +
					`,"gen_ai.conversation.id":"` + chatID + `"` +
					`,"gram.project.id":"` + s.projectID().String() + `"` +
					`,"user.email":"` + email + `"` +
					`,"gram.external_user.id":"` + email + `"` +
					chIdentityAttrs(uidx) +
					`,"gram.hook.source":"claude-code"` +
					`,"gram.provider":"anthropic"` +
					// See chPersonalAccount for the contractor/manager split.
					`,"gram.account_type":"` + chIf(personal, "personal", "team") + `"` +
					`,"gram.billing_mode":"` + chIf(personal, "flat_rate", "metered") + `"}`,
				resource: s.chClaudeResource(),
				urn:      "claude-code:otel:logs",
				service:  "claude-code",
				chatID:   new(chatID),
			})
		}
	}
	return rows
}

// chClaudeToolResultRows are Claude provenance (odd chats): one
// claude_code.tool_result row per tool call. tool_use_id joins the Postgres
// tool messages (call_demo_<i>_<k>); prompt.id is REQUIRED here too — the
// tool-payload-size query filters on it. Odd chats ran in Claude Code, so the
// client (and the tool draw) is the Claude pair's.
func (s *seeder) chClaudeToolResultRows() []chTelemetryRow {
	var rows []chTelemetryRow
	for i := 1; i <= bulkChats; i += 2 {
		uidx := s.chChatOwnerIdx(i)
		email := s.email(demoUsers[uidx].handle)
		chatID := s.chatID(i).String()
		nano := s.chatTS(i).UnixNano()
		for k := 1; k <= 2; k++ {
			ts := nano + int64(k)*61000000000
			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "claude_code.tool_result",
				// Same apitrace-<i>-<n> name family as the api_request rows,
				// exactly as the old script wrote it.
				traceID: s.chTrace(fmt.Sprintf("apitrace-%d-%d", i, k)),
				attributes: `{"event.name":"tool_result"` +
					`,"tool_use_id":"call_demo_` + strconv.Itoa(i) + `_` + strconv.Itoa(k) + `"` +
					`,"prompt.id":"demo-prompt-` + strconv.Itoa(i) + `-` + strconv.Itoa(k) + `"` +
					`,"tool_name":"` + s.chToolName(i, k) + `"` +
					`,"tool_input_size_bytes":` + chItoa(200+hash64("tin", i, k)%1800) +
					`,"tool_result_size_bytes":` + chItoa(500+hash64("tout", i, k)%8000) +
					`,"success":` + chIf(hash64("cres", i, k)%15 == 0, "false", "true") +
					`,"gen_ai.conversation.id":"` + chatID + `"` +
					`,"gram.project.id":"` + s.projectID().String() + `"` +
					`,"user.email":"` + email + `"` +
					chIdentityAttrs(uidx) +
					`,"gram.hook.source":"claude-code"}`,
				resource: s.chClaudeResource(),
				urn:      "claude-code:otel:logs",
				service:  "claude-code",
				chatID:   new(chatID),
			})
		}
	}
	return rows
}

// chSkillRows are odd chats' 1-2 Skill hook rows each with a hash-weighted
// skill pick — feeds the Insights "Skill Usage" / "Users per Skill" panels
// (skill_name materializes only when gram.tool.name = 'Skill'). Deliberately
// looser than the efficacy tables' fixed skill-per-chat formula: these rows
// only drive the usage charts, which should not look like a perfect rotation.
func (s *seeder) chSkillRows() []chTelemetryRow {
	skills := []string{
		"triage-incident", "support-refunds", "triage-incident", "runbook",
		"support-refunds", "triage-incident", "support-refunds", "runbook",
	}
	var rows []chTelemetryRow
	for i := 1; i <= bulkChats; i += 2 {
		u := demoUsers[s.chChatOwnerIdx(i)]
		chatID := s.chatID(i).String()
		nano := s.chatTS(i).UnixNano()
		n := 1 + s.chChatByte(i, 10)%2
		for j := 1; j <= n; j++ {
			skill := skills[hash64("skl", i, j)%8]
			ts := nano + int64(45000000000+uint64(j-1)*420000000000+hash64("sklt", i, j)%300000000000)
			// Skills fail like anything else that shells out. Without this
			// the skills half of the erroring-targets list is permanently
			// empty.
			outcome := chIf(hash64("sklerr", i, j)%12 == 0,
				`,"gram.hook.error":"skill step exited non-zero"`,
				`,"gen_ai.tool.call.result":"ok"`)
			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "Hook: Skill invoked",
				traceID:      s.chTrace(fmt.Sprintf("skilltrace-%d-%d", i, j)),
				attributes: `{"gram.event.source":"hook"` +
					`,"gram.hook.source":"claude-code"` +
					`,"gram.hook.event":"PostToolUse"` +
					`,"gram.tool.name":"Skill"` +
					`,"gen_ai.tool.call.arguments":"{\"skill\":\"` + skill + `\"}"` +
					outcome +
					`,"gen_ai.conversation.id":"` + chatID + `"` +
					`,"gram.project.id":"` + s.projectID().String() + `"` +
					`,"user.email":"` + s.email(u.handle) + `"` +
					`,"gram.hook.hostname":"` + u.hostname + `"}`,
				resource: s.chResource(),
				urn:      "",
				service:  "",
				chatID:   new(chatID),
			})
		}
	}
	return rows
}

// chMCPSkillLoadRows are skill:// reads served over the support server, one
// in every third chat, shaped like the rows the hosted MCP skill read path
// writes: a resource_read named Skill with the skill name as its argument, so
// skill_name materializes and the tool-usage queries classify it as a skill
// next to the hook rows above. Without them every skill on Logs and MCP &
// Tools reads as hook-only and the "Loaded via" MCP filter is empty.
func (s *seeder) chMCPSkillLoadRows() []chTelemetryRow {
	var rows []chTelemetryRow
	for i := 3; i <= bulkChats; i += 3 {
		uidx := s.chChatOwnerIdx(i)
		u := demoUsers[uidx]
		chatID := s.chatID(i).String()
		pos := 1 + int(hash64("mcpskl", i)%3)
		name := skillNames[pos-1]
		content := skillsManual[pos-1].content
		digest := sha256.Sum256([]byte(content))
		cidx := s.chClientIdx(i)
		// Loads come early in a chat: the agent reads the skill before it
		// starts calling tools.
		ts := s.chatTS(i).UnixNano() + int64(20000000000+hash64("mcpsklt", i)%40000000000)
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "INFO",
			body:         "Skill read: " + name,
			traceID:      s.chTrace(fmt.Sprintf("mcpskilltrace-%d", i)),
			attributes: `{"gram.event.source":"resource_read"` +
				`,"gram.tool.name":"Skill"` +
				`,"gen_ai.tool.call.arguments":"{\"skill\":\"` + name + `\"}"` +
				`,"gram.resource.uri":"` + mcpskills.SkillURI(name) + `"` +
				`,"gram.mcp.skill.id":"` + s.skillID(pos).String() + `"` +
				`,"gram.mcp.skill.version_id":"` + s.skillVersionID(pos).String() + `"` +
				`,"gram.mcp.skill.digest":"sha256:` + hex.EncodeToString(digest[:]) + `"` +
				`,"gram.toolset.slug":"acme-support-tools"` +
				`,"gram.mcp_server.id":"` + s.det("mcpserver-support").String() + `"` +
				`,"http.response.status_code":200` +
				`,"http.response.body":` + strconv.Itoa(len(content)) +
				`,"gram.mcp.client.name":"` + chClientNames[cidx-1] + `"` +
				`,"gram.mcp.client.version":"` + chClientVersions[cidx-1][s.chChatByte(i, 16)%5] + `"` +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.email":"` + s.email(u.handle) + `"` +
				chIdentityAttrs(uidx) + `}`,
			resource: s.chResource(),
			urn:      urn.NewSkill(s.skillID(pos)).String(),
			service:  "gram-server",
			chatID:   new(chatID),
		})
	}
	return rows
}

// chBlockedRows are calls the Speakeasy hook denied before they ran
// (PreToolUse). These are the only rows that carry gram.hook.block_reason, so
// without them the blocked status filter on Tool Logs and the blocked
// counters on Insights are dead controls in the demo org. Denials cluster on
// the destructive tools, and on the two users whose roles do not carry
// production access.
func (s *seeder) chBlockedRows() []chTelemetryRow {
	tools := []string{"process_refund", "process_refund", "query_db", "restart_service", "run_payroll"}
	days := []int{0, 0, 1, 1, 2, 2, 3, 4, 5, 7, 8, 11}
	hours := []int{9, 10, 11, 11, 13, 14, 15, 16, 16, 17}
	var rows []chTelemetryRow
	for number := range 150 {
		i := number + 1
		// The chat's own owner, by the same formula every other block uses.
		// Drawing the person independently would file the block under a
		// conversation somebody else was having.
		uidx := s.chChatOwnerIdx(i)
		// Billing and part-time roles, i.e. the people a policy is written
		// for: mateo, hana, lucas. Selecting the chats they own keeps the
		// denial and the conversation it happened in agreeing about who was
		// there. Roughly a third of chats qualify, so 150 candidates yield
		// ~46 denials.
		if uidx < 3 {
			continue
		}
		u := demoUsers[uidx]
		chatID := s.chatID(i).String()
		hook := chIf(i%2 == 1, "claude-code", "cursor")
		toolName := tools[hash64("blkt", number)%5]
		var blockReason string
		switch toolName {
		case "process_refund":
			blockReason = "refunds above the approval threshold require a human"
		case "query_db":
			blockReason = "query touches a table holding customer PII"
		case "restart_service":
			blockReason = "production restart outside the change window"
		default:
			blockReason = "payroll tools are not callable by agents"
		}
		ts0 := s.chStartOfDay().
			AddDate(0, 0, -days[hash64("blkd", number)%12]).
			Add(time.Duration(hours[hash64("blkh", number)%10]) * time.Hour).
			Add(time.Duration(hash64("blkm", number)%60) * time.Minute)
		if ts0.After(s.now.Add(-30 * time.Minute)) {
			ts0 = ts0.AddDate(0, 0, -1)
		}
		ts := ts0.UnixNano()
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "WARN",
			body:         "Hook: blocked " + toolName,
			traceID:      s.chTrace("blocktrace-" + strconv.Itoa(i)),
			attributes: `{"gram.event.source":"hook"` +
				`,"gram.hook.source":"` + hook + `"` +
				`,"gram.hook.event":"PreToolUse"` +
				`,"gram.tool.name":"` + toolName + `"` +
				`,"gram.tool_call.source":"acme-internal-mcp"` +
				`,"gram.hook.block_reason":"` + blockReason + `"` +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.email":"` + s.email(u.handle) + `"` +
				`,"gram.hook.hostname":"` + u.hostname + `"}`,
			resource: s.chResource(),
			urn:      "hooks:" + toolName,
			service:  "gram-hooks",
			chatID:   new(chatID),
		})
	}
	return rows
}

// chCursorUsageRows are Cursor provenance (even chats): one
// cursor:usage:metrics row per chat. Provider follows the model (gpt-5.6 ->
// openai).
func (s *seeder) chCursorUsageRows() []chTelemetryRow {
	var rows []chTelemetryRow
	for i := 2; i <= bulkChats; i += 2 {
		number := i - 1
		uidx := s.chChatOwnerIdx(i)
		email := s.email(demoUsers[uidx].handle)
		chatID := s.chatID(i).String()
		ts := s.chatTS(i).UnixNano() + 2000000
		model := chIf(hash64("model", number)%2 == 0, "claude-sonnet-4-6", "gpt-5.6")
		inTok := 800000 + hash64("in", number)%7000000
		outTok := 3000 + hash64("out", number)%40000
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "INFO",
			body:         "Cursor usage metrics",
			traceID:      s.chTrace("usagetrace-" + strconv.Itoa(i)),
			attributes: `{"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gen_ai.response.id":"msg_` + md5Hex(s.name("cursor-respid-" + strconv.Itoa(i)))[:24] + `"` +
				`,"gen_ai.usage.input_tokens":` + chItoa(inTok) +
				`,"gen_ai.usage.output_tokens":` + chItoa(outTok) +
				`,"gen_ai.usage.cache_read.input_tokens":` + chItoa(inTok*4) +
				`,"gen_ai.usage.cost":` + chRound(float64(inTok*42+outTok*150)/10000000, 6) +
				`,"gen_ai.response.model":"` + model + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.email":"` + email + `"` +
				`,"gram.external_user.id":"` + email + `"` +
				chIdentityAttrs(uidx) +
				`,"gram.hook.source":"cursor"` +
				`,"gram.provider":"` + chIf(model == "gpt-5.6", "openai", "anthropic") + `"` +
				`,"gram.account_type":"team"` +
				`,"gram.billing_mode":"flat_rate"}`,
			resource: s.chResource(),
			urn:      "cursor:usage:metrics",
			service:  "",
			chatID:   new(chatID),
		})
	}
	return rows
}

// chCursorHookRows are Cursor provenance (even chats): completed tool-call
// hook rows. Unique hooktrace ids + gram.event.source=hook put them on the
// Insights hook branch as shadow-MCP-sourced traffic; result/error attrs
// drive success/failure classification. Even chats ran in Cursor, so the
// client (and the tool draw) is the Cursor pair's.
func (s *seeder) chCursorHookRows() []chTelemetryRow {
	var rows []chTelemetryRow
	for i := 2; i <= bulkChats; i += 2 {
		uidx := s.chChatOwnerIdx(i)
		email := s.email(demoUsers[uidx].handle)
		chatID := s.chatID(i).String()
		nano := s.chatTS(i).UnixNano()
		for k := 1; k <= 2; k++ {
			toolName := s.chToolName(i, k)
			hfail := hash64("hfail", i, k)%16 == 0
			ts := nano + int64(k)*31000000000
			rows = append(rows, chTelemetryRow{
				timeNano:     ts,
				observedNano: ts,
				severity:     "INFO",
				body:         "Hook: PostToolUse " + toolName,
				traceID:      s.chTrace(fmt.Sprintf("hooktrace-%d-%d", i, k)),
				attributes: `{"gram.event.source":"hook"` +
					`,"gram.hook.source":"cursor"` +
					`,"gram.hook.event":"` + chIf(hfail, "PostToolUseFailure", "PostToolUse") + `"` +
					`,"gram.tool.name":"` + toolName + `"` +
					`,"gram.tool_call.source":"acme-internal-mcp"` +
					chIf(hfail, `,"gram.hook.error":"tool execution failed"`, `,"gen_ai.tool.call.result":"ok"`) +
					`,"gen_ai.tool.call.id":"call_demo_` + strconv.Itoa(i) + `_` + strconv.Itoa(k) + `"` +
					`,"gen_ai.conversation.id":"` + chatID + `"` +
					`,"gram.project.id":"` + s.projectID().String() + `"` +
					`,"user.email":"` + email + `"` +
					chIdentityAttrs(uidx) + `}`,
				resource: s.chResource(),
				urn:      "",
				service:  "",
				chatID:   new(chatID),
			})
		}
	}
	return rows
}

// chWorkUnitRows are one chat-analysis work-units score row per chat: feeds
// the Costs "Efficiency" dataset (total_work_units / scored_cost /
// scored_tokens are summed only from these synthetic rows).
func (s *seeder) chWorkUnitRows() []chTelemetryRow {
	rows := make([]chTelemetryRow, 0, bulkChats)
	for i := 1; i <= bulkChats; i++ {
		uidx := s.chChatOwnerIdx(i)
		u := demoUsers[uidx]
		chatID := s.chatID(i).String()
		ts := s.chatTS(i).UnixNano() + 3000000
		odd := i%2 == 1
		provider := chIf(odd, "anthropic", chIf(hash64("model", i-1)%2 == 1, "openai", "anthropic"))
		rows = append(rows, chTelemetryRow{
			timeNano:     ts,
			observedNano: ts,
			severity:     "INFO",
			body:         "Chat analysis: work units",
			traceID:      s.chTrace("wutrace-" + strconv.Itoa(i)),
			attributes: `{"gram.chat_analysis.work_units":` + chItoa(1+hash64("wu", i)%5) +
				`,"gram.chat_analysis.scored_cost":` + chRound(float64(5000000+hash64("sc", i)%40000000)/1000000, 4) +
				`,"gram.chat_analysis.scored_tokens":` + chItoa(2000000+hash64("st", i)%12000000) +
				`,"gen_ai.conversation.id":"` + chatID + `"` +
				`,"gen_ai.response.model":"` + chIf(odd, "claude-sonnet-4-6", "gpt-5.6") + `"` +
				`,"gram.project.id":"` + s.projectID().String() + `"` +
				`,"user.email":"` + s.email(u.handle) + `"` +
				chIdentityAttrs(uidx) +
				`,"gram.provider":"` + provider + `"` +
				// Same split as the chat rows above, keyed the same way so a
				// chat's score lands on the account that ran it. Odd rows
				// only: those are the Anthropic ones, and both seeded
				// personal accounts are Anthropic.
				`,"gram.account_type":"` + chIf(odd && chPersonalAccount(u.handle, i), "personal", "team") + `"` +
				`,"gram.hook.source":"` + chIf(odd, "claude-code", "cursor") + `"}`,
			resource: s.chResource(),
			urn:      "chat_analysis:work_units:score",
			service:  "",
			chatID:   new(chatID),
		})
	}
	return rows
}

// chShadowInventory writes the Shadow MCP inventory (Shadow MCP page list,
// call/user counts); its companion hook telemetry follows in the next phase.
func (s *seeder) chShadowInventory(ctx context.Context, ch driver.Conn) error {
	servers := []struct {
		url, host, name string
		firstDays       int
		lastHours       int
	}{
		{"https://api.githubcopilot.com/mcp", "api.githubcopilot.com", "GitHub", 30, 2},
		{"https://mcp.notion.com/mcp", "mcp.notion.com", "Notion", 29, 4},
		{"https://mcp.linear.app/mcp", "mcp.linear.app", "Linear", 28, 6},
		{"https://mcp.slack.com/mcp", "mcp.slack.com", "Slack", 26, 8},
		{"https://mcp.sentry.dev/mcp", "mcp.sentry.dev", "Sentry", 25, 10},
		{"https://mcp.datadoghq.com/api/mcp", "mcp.datadoghq.com", "Datadog", 23, 12},
		{"https://mcp.cloudflare.com/mcp", "mcp.cloudflare.com", "Cloudflare", 22, 14},
		{"https://mcp.stripe.com/mcp", "mcp.stripe.com", "Stripe", 20, 16},
		{"https://mcp.figma.com/mcp", "mcp.figma.com", "Figma", 19, 18},
		{"https://postgres.internal.example.com/mcp", "postgres.internal.example.com", "Postgres Explorer", 17, 20},
		{"https://support-tools.example.com/mcp", "support-tools.example.com", "Customer Support", 16, 22},
		{"https://prod-admin.example.com/mcp", "prod-admin.example.com", "Production Admin", 14, 24},
		{"https://warehouse.example.com/mcp", "warehouse.example.com", "Data Warehouse", 13, 26},
		{"https://incidents.example.com/mcp", "incidents.example.com", "Incident Commander", 11, 28},
		{"https://payroll.example.com/mcp", "payroll.example.com", "Payroll Assistant", 10, 30},
	}
	batch, err := ch.PrepareBatch(ctx, `INSERT INTO shadow_mcp_inventory_urls
		(gram_project_id, canonical_server_url, url_host, server_name, first_seen, last_seen, updated_at)`)
	if err != nil {
		return fmt.Errorf("prepare demo shadow_mcp_inventory_urls batch: %w", err)
	}
	defer func() { _ = batch.Abort() }()
	for _, srv := range servers {
		if err := batch.Append(
			s.projectID(), srv.url, srv.host, srv.name,
			s.now.AddDate(0, 0, -srv.firstDays),
			s.now.Add(-time.Duration(srv.lastHours)*time.Hour),
			s.now,
		); err != nil {
			return fmt.Errorf("append demo shadow_mcp_inventory_urls row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert demo shadow_mcp_inventory_urls: %w", err)
	}
	return nil
}
