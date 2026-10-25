package demoseed

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// chDetectionsAndFindings: Shadow MCP inventory, AI detections and scans, authz challenges, risk findings, skill versions and efficacy, billing meter readings.
func (s *seeder) chDetectionsAndFindings(ctx context.Context, ch driver.Conn) error {
	steps := []func(context.Context, driver.Conn) error{
		s.chfShadowTelemetry,
		s.chfAIDetections,
		s.chfAIScanReceipts,
		s.chfAuthzChallenges,
		s.chfRiskFindings,
		s.chfSkillSessionVersions,
		s.chfSkillEfficacyScores,
		s.chfGatewayDiscovery,
		s.chfGatewayMemberCalls,
		s.chfGatewayGoneCalls,
		s.chfGatewayHooks,
		s.chfBillingMeterReadings,
	}
	for _, step := range steps {
		if err := step(ctx, ch); err != nil {
			return err
		}
	}
	return nil
}

// chfInsert prepares one INSERT batch, lets fill append its rows, and sends
// it.
func chfInsert(ctx context.Context, ch driver.Conn, what, query string, fill func(driver.Batch) error) error {
	batch, err := ch.PrepareBatch(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare demo %s insert: %w", what, err)
	}
	if err := fill(batch); err != nil {
		_ = batch.Abort()
		return fmt.Errorf("append demo %s: %w", what, err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("insert demo %s: %w", what, err)
	}
	return nil
}

// chfDay is the start of today (UTC) minus n days: the old script's
// toStartOfDay(now()) - toIntervalDay(n).
func (s *seeder) chfDay(n int) time.Time {
	return s.now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -n)
}

// chfNoFuture slides a slot later than 30 minutes ago back a day, so the seed
// never writes future timestamps.
func (s *seeder) chfNoFuture(ts time.Time) time.Time {
	if ts.After(s.now.UTC().Add(-30 * time.Minute)) {
		return ts.AddDate(0, 0, -1)
	}
	return ts
}

// chfHashSlot is the humanized chat schedule (see chatTS) applied to an
// arbitrary hash name.
func (s *seeder) chfHashSlot(name string) time.Time {
	ts := s.chfDay(chatDayOffsets[hashByte(name, 0)%16]).
		Add(time.Duration(chatHours[hashByte(name, 2)%16]) * time.Hour).
		Add(time.Duration(hashByte(name, 4)%60) * time.Minute)
	return s.chfNoFuture(ts)
}

// chfRound3 renders round(x, 3) as a JSON number.
func chfRound3(x float64) string {
	return strconv.FormatFloat(math.Round(x*1000)/1000, 'f', -1, 64)
}

func (s *seeder) chfResourceAttrs() string {
	return `{"gram.deployment.id":"` + s.spec.Marker + `"}`
}

const chfTelemetryInsert = `INSERT INTO telemetry_logs
  (time_unix_nano, observed_time_unix_nano, severity_text, body, trace_id,
   attributes, resource_attributes, gram_project_id, gram_urn, service_name, gram_chat_id)`

type chfTelemetryRow struct {
	ts      time.Time
	body    string
	traceID string
	attrs   string
	urn     string
	service string
	// chatID is nil where the old insert left gram_chat_id out (NULL).
	chatID *string
}

func (s *seeder) chfInsertTelemetry(ctx context.Context, ch driver.Conn, what string, rows []chfTelemetryRow) error {
	return chfInsert(ctx, ch, what, chfTelemetryInsert, func(b driver.Batch) error {
		for _, r := range rows {
			nano := r.ts.UnixNano()
			if err := b.Append(nano, nano, "INFO", r.body, r.traceID, r.attrs, s.chfResourceAttrs(),
				s.projectID(), r.urn, r.service, r.chatID); err != nil {
				return fmt.Errorf("append telemetry row: %w", err)
			}
		}
		return nil
	})
}

var (
	chfShadowTools = []string{
		"search_issues", "search_pages", "list_issues", "post_message", "list_errors", "query_metrics",
		"list_zones", "create_charge", "get_file", "run_query", "lookup_ticket", "restart_service",
		"run_report", "page_oncall", "run_payroll",
	}
	chfShadowServers = []string{
		"https://api.githubcopilot.com/mcp", "https://mcp.notion.com/mcp", "https://mcp.linear.app/mcp",
		"https://mcp.slack.com/mcp", "https://mcp.sentry.dev/mcp", "https://mcp.datadoghq.com/api/mcp",
		"https://mcp.cloudflare.com/mcp", "https://mcp.stripe.com/mcp", "https://mcp.figma.com/mcp",
		"https://postgres.internal.example.com/mcp", "https://support-tools.example.com/mcp",
		"https://prod-admin.example.com/mcp", "https://warehouse.example.com/mcp",
		"https://incidents.example.com/mcp", "https://payroll.example.com/mcp",
	}
	// Shadow usage is long-tailed: two or three servers carry most of the
	// traffic and the rest are occasional. A uniform draw across all fifteen
	// makes every ranking flat and every server look equally worth chasing.
	// Values are 1-based indexes into chfShadowTools/chfShadowServers.
	chfShadowServerDraw = [32]int{
		1, 1, 1, 1, 2, 2, 2, 3, 3, 4, 5, 5, 6, 7, 8, 9,
		10, 11, 1, 2, 3, 12, 13, 2, 1, 14, 15, 1, 2, 3, 1, 5,
	}
	chfShadowErrors = []string{
		"upstream returned 401", "upstream returned 429", "connection reset by peer",
		"upstream timed out after 30s",
	}
	// 1-based indexes into demoUsers.
	chfShadowUserDraw = [8]int{3, 3, 3, 1, 1, 4, 2, 5}
)

// chfShadowTelemetry: hook rows for unsanctioned (shadow) MCP servers.
func (s *seeder) chfShadowTelemetry(ctx context.Context, ch driver.Conn) error {
	empty := ""
	rows := make([]chfTelemetryRow, 0, 180)
	for n := range 180 {
		sidx := chfShadowServerDraw[hash64("sht", n)%32]
		tool := chfShadowTools[sidx-1]
		// The servers touching money and production access fail more than the
		// read-only ones, which is what makes an erroring-servers list useful.
		failed := ((sidx == 8 || sidx == 12 || sidx == 15) && hash64("shf", n)%4 == 0) ||
			hash64("shfbg", n)%14 == 0
		// Unsanctioned servers are where calls actually fail: expired personal
		// tokens, rate limits, servers nobody owns. A shadow inventory that is
		// 100% healthy hides the reason anyone looks at this page.
		result := `,"gen_ai.tool.call.result":"ok"`
		if failed {
			result = `,"gram.hook.error":"` + chfShadowErrors[hash64("sherr", n)%4] + `"`
		}
		user := demoUsers[chfShadowUserDraw[hash64("shu", n)%8]-1]
		attrs := `{"gram.event.source":"hook"` +
			`,"gram.hook.source":"claude-code"` +
			`,"gram.hook.event":"PostToolUse"` +
			`,"gram.tool.name":"` + tool + `"` +
			`,"gram.mcp.server_url":"` + chfShadowServers[sidx-1] + `"` +
			result +
			`,"gram.project.id":"` + s.projectID().String() + `"` +
			`,"user.email":"` + s.email(user.handle) + `"}`
		rows = append(rows, chfTelemetryRow{
			// Spread over the same trailing fortnight as the chat traffic and
			// weighted to working hours, rather than a fixed nine-hour drumbeat
			// marching back two months past every other row in the org.
			ts:      s.chfHashSlot(s.name("shadowslot-" + strconv.Itoa(n))),
			body:    "Shadow MCP tool call",
			traceID: md5Hex(s.name("shadowtrace-" + strconv.Itoa(n))),
			attrs:   attrs,
			urn:     "hooks:" + tool,
			service: "gram-hooks",
			chatID:  &empty,
		})
	}
	return s.chfInsertTelemetry(ctx, ch, "shadow mcp telemetry", rows)
}

type chfDetection struct {
	target, device, handle, signal, category, version string
	firstDays, lastHours                              int
}

// Shadow AI detections (employee enrollment detail): device-agent AI scan
// results for the six demo directory users. Target ids and categories come
// from the aitargets catalog (server/internal/agent/aitargets); one row per
// (target, device, user, signal) matching the ReplacingMergeTree key. Priya
// carries two devices so the users/devices counts differ. Versions stamp
// installed rows only — running detections usually cannot extract one.
//
// Cline and Continue are the editor-extension case: both ship a CLI and both
// carry a wildcard config dir for their version-stamped VS Code extension
// directory, so they are also the rows that exercise the glob path end to
// end. Zed and Goose are the two new targets that publish CIMD, so they are
// the ones carrying a real decision below.
//
// The two assistants are deliberately a pair: both speak MCP to Speakeasy like
// a harness does without being coding tools, but Hermes publishes a CIMD
// document and OpenClaw does not. So a decision on Hermes is enforceable and
// shows as one, while OpenClaw reads unreviewed however hard somebody wants
// to block it — the same contrast the Harnesses tab draws between Codex and
// Cursor.
var chfDetections = []chfDetection{
	{"claude-code", "DEMO-MBP-AMARA", "amara", "installed", "harness", "2.0.44", 8, 2},
	{"claude-code", "DEMO-MBP-AMARA", "amara", "running", "harness", "", 8, 2},
	{"claude-code", "DEMO-MBP-JONAS", "jonas", "installed", "harness", "2.0.41", 7, 8},
	{"claude-code", "DEMO-MBP-JONAS", "jonas", "running", "harness", "", 7, 8},
	{"claude-code", "DEMO-MBP-PRIYA", "priya", "installed", "harness", "2.0.44", 8, 3},
	{"claude-code", "DEMO-MBP-PRIYA", "priya", "running", "harness", "", 8, 3},
	{"claude-code", "DEMO-MSTUDIO-PRIYA", "priya", "installed", "harness", "2.0.38", 5, 30},
	{"claude-code", "DEMO-MBP-MATEO", "mateo", "installed", "harness", "2.0.44", 7, 22},
	{"claude-code", "DEMO-MBP-MATEO", "mateo", "running", "harness", "", 7, 26},
	{"claude-code", "DEMO-MBP-HANA", "hana", "installed", "harness", "2.0.41", 6, 18},
	{"claude-code", "DEMO-MBP-HANA", "hana", "running", "harness", "", 6, 18},
	{"claude-code", "DEMO-MBP-LUCAS", "lucas", "installed", "harness", "2.0.44", 5, 12},
	{"cursor", "DEMO-MBP-AMARA", "amara", "installed", "harness", "1.7.52", 7, 5},
	{"cursor", "DEMO-MBP-AMARA", "amara", "running", "harness", "", 7, 5},
	{"cursor", "DEMO-MBP-JONAS", "jonas", "installed", "harness", "1.7.49", 6, 28},
	{"cursor", "DEMO-MBP-PRIYA", "priya", "installed", "harness", "1.7.52", 7, 7},
	{"cursor", "DEMO-MBP-PRIYA", "priya", "running", "harness", "", 7, 7},
	{"codex", "DEMO-MBP-PRIYA", "priya", "installed", "harness", "0.52.0", 6, 26},
	{"codex", "DEMO-MBP-MATEO", "mateo", "installed", "harness", "0.50.1", 5, 50},
	{"ollama", "DEMO-MBP-PRIYA", "priya", "installed", "local_model", "0.6.2", 6, 4},
	{"ollama", "DEMO-MBP-PRIYA", "priya", "running", "local_model", "", 6, 4},
	{"ollama", "DEMO-MSTUDIO-PRIYA", "priya", "installed", "local_model", "0.6.0", 5, 72},
	{"ollama", "DEMO-MBP-MATEO", "mateo", "installed", "local_model", "0.6.2", 5, 9},
	{"ollama", "DEMO-MBP-MATEO", "mateo", "running", "local_model", "", 5, 9},
	{"lmstudio", "DEMO-MBP-MATEO", "mateo", "installed", "local_model", "0.3.9", 4, 72},
	{"aider", "DEMO-MBP-JONAS", "jonas", "installed", "harness", "0.86.1", 7, 120},
	{"openclaw", "DEMO-MBP-HANA", "hana", "installed", "assistant", "0.4.1", 5, 11},
	{"openclaw", "DEMO-MBP-HANA", "hana", "running", "assistant", "", 5, 11},
	{"openclaw", "DEMO-MBP-LUCAS", "lucas", "installed", "assistant", "0.4.0", 4, 40},
	{"openclaw", "DEMO-MBP-AMARA", "amara", "installed", "assistant", "0.4.1", 6, 6},
	{"openclaw", "DEMO-MBP-AMARA", "amara", "running", "assistant", "", 6, 6},
	{"openclaw", "DEMO-MSTUDIO-PRIYA", "priya", "installed", "assistant", "0.3.8", 4, 64},
	{"hermes-agent", "DEMO-MBP-JONAS", "jonas", "installed", "assistant", "1.2.0", 5, 14},
	{"hermes-agent", "DEMO-MBP-JONAS", "jonas", "running", "assistant", "", 5, 14},
	{"hermes-agent", "DEMO-MBP-MATEO", "mateo", "installed", "assistant", "1.1.4", 3, 30},
	{"cline", "DEMO-MBP-AMARA", "amara", "installed", "harness", "4.1.17", 6, 3},
	{"cline", "DEMO-MBP-AMARA", "amara", "running", "harness", "", 6, 3},
	{"cline", "DEMO-MBP-JONAS", "jonas", "installed", "harness", "4.1.12", 5, 20},
	{"cline", "DEMO-MBP-PRIYA", "priya", "installed", "harness", "4.1.17", 4, 9},
	{"continue", "DEMO-MBP-MATEO", "mateo", "installed", "harness", "2.1.0", 4, 44},
	{"zed", "DEMO-MBP-HANA", "hana", "installed", "harness", "0.221.4", 7, 5},
	{"zed", "DEMO-MBP-HANA", "hana", "running", "harness", "", 7, 5},
	{"zed", "DEMO-MBP-LUCAS", "lucas", "installed", "harness", "0.221.4", 5, 16},
	{"goose", "DEMO-MBP-PRIYA", "priya", "installed", "harness", "1.34.2", 5, 34},
	{"goose", "DEMO-MSTUDIO-PRIYA", "priya", "installed", "harness", "1.33.0", 4, 58},
	{"warp", "DEMO-MBP-AMARA", "amara", "installed", "harness", "0.2026.08.19", 8, 7},
	{"warp", "DEMO-MBP-AMARA", "amara", "running", "harness", "", 8, 7},
	{"warp", "DEMO-MBP-HANA", "hana", "installed", "harness", "0.2026.08.19", 6, 21},
	{"msty", "DEMO-MBP-JONAS", "jonas", "installed", "assistant", "1.9.2", 4, 26},
	{"msty", "DEMO-MBP-LUCAS", "lucas", "installed", "assistant", "1.9.0", 3, 52},
	{"jan", "DEMO-MBP-HANA", "hana", "installed", "local_model", "0.8.4", 5, 13},
	{"jan", "DEMO-MBP-HANA", "hana", "running", "local_model", "", 5, 13},
}

func (s *seeder) chfAIDetections(ctx context.Context, ch driver.Conn) error {
	now := s.now.UTC()
	return chfInsert(ctx, ch, "ai detections", `INSERT INTO ai_detections
  (organization_id, target_id, device_serial, user_email, signal, category,
   version, first_seen, last_seen, updated_at)`, func(b driver.Batch) error {
		for _, d := range chfDetections {
			if err := b.Append(s.orgID(), d.target, d.device, s.email(d.handle), d.signal, d.category,
				d.version, now.AddDate(0, 0, -d.firstDays), now.Add(-time.Duration(d.lastHours)*time.Hour), now); err != nil {
				return fmt.Errorf("append ai detection: %w", err)
			}
		}
		return nil
	})
}

var (
	chfScanDevices = []string{
		"DEMO-MBP-AMARA", "DEMO-MBP-JONAS", "DEMO-MBP-PRIYA",
		"DEMO-MSTUDIO-PRIYA", "DEMO-MBP-MATEO", "DEMO-MBP-HANA",
		"DEMO-MBP-LUCAS",
	}
	chfScanHandles = []string{"amara", "jonas", "priya", "priya", "mateo", "hana", "lucas"}
	chfScanMatches = []uint32{10, 8, 9, 4, 8, 9, 4}
)

// chfAIScanReceipts: one receipt per device per day over the trailing 5
// days, proving every enrolled device scanned recently (the page's freshness
// story and the provable-coverage contract for zero-match devices).
// match_count is the number of matches the agent reported, and every reported
// match becomes one ai_detections row, so each device's count here must equal
// that device's row count in chfDetections — add a detection, update
// chfScanMatches.
func (s *seeder) chfAIScanReceipts(ctx context.Context, ch driver.Conn) error {
	now := s.now.UTC()
	return chfInsert(ctx, ch, "ai scan receipts", `INSERT INTO ai_scan_receipts
  (organization_id, device_serial, user_email, scan_started_at,
   scan_completed_at, target_list_version, match_count, received_at)`, func(b driver.Batch) error {
		for n := range 35 {
			didx := n % 7
			dayOff := n / 7
			ts := now.AddDate(0, 0, -dayOff).
				Add(-time.Duration(2+hash64("aiscan", n)%4) * time.Hour).
				Add(-time.Duration(hash64("aiscanm", n)%55) * time.Minute)
			if err := b.Append(s.orgID(), chfScanDevices[didx], s.email(chfScanHandles[didx]),
				ts.Add(-6*time.Second), ts.Add(-1*time.Second), int32(3), chfScanMatches[didx], ts); err != nil {
				return fmt.Errorf("append ai scan receipt: %w", err)
			}
		}
		return nil
	})
}

var (
	chfChalScopes = []string{
		"toolset:admin", "project:admin", "environment:read",
		"environment:write", "mcp:connect", "skill:write", "chat:read",
	}
	chfChalKinds = []string{"toolset", "project", "environment", "environment", "mcp", "skill", "chat"}
)

// chfAuthzChallenges: org home "Recent challenges" panel, /access/challenges,
// and the per-identity Access and Security tabs.
//
// user_id values are demo members (organization_user_relationships rows
// exist, so the member-suppression filter keeps them visible); one api_key
// bucket stays visible regardless of membership.
//
// The subject and the scope are drawn from DIFFERENT hashes on purpose.
// Keying both off `number % 3` locked each person to a single scope forever,
// so every identity's Access tab listed one action repeated N times and the
// whole panel read as a stuck query. Outcomes are mixed for the same reason: a
// log that is 100% denials says nothing about which denials matter, and the
// donut on the Access tab has only one segment to draw.
func (s *seeder) chfAuthzChallenges(ctx context.Context, ch driver.Conn) error {
	now := s.now.UTC()
	proj := s.projectID().String()
	resources := []string{s.toolset1().String(), proj, s.fixed("0000-4000-a000-00000000ee01").String()}
	selector := `{"project":"` + proj + `"}`
	const apiKeyID = "akey_demo0000000001"
	return chfInsert(ctx, ch, "authz challenges", `INSERT INTO authz_challenges
  (timestamp, organization_id, project_id, trace_id, span_id, request_id,
   principal_urn, principal_type, user_id, user_email, api_key_id,
   role_slugs, operation, outcome, reason, scope, resource_kind, resource_id,
   selector, expanded_scopes,
   `+"`requested_checks.scope`, `requested_checks.resource_kind`,\n   `requested_checks.resource_id`, `requested_checks.selector`,\n   `matched_grants.principal_urn`, `matched_grants.scope`,\n   `matched_grants.selector`, `matched_grants.matched_via_check_scope`,"+`
   evaluated_grant_count)`, func(b driver.Batch) error {
		for n := range 64 {
			isKey := n%7 == 6
			user := demoUsers[hash64("chal-user", n)%6]
			var principalURN, principalType string
			var userID, userEmail, keyID *string
			if isKey {
				principalURN, principalType = "api_key:"+apiKeyID, "api_key"
				keyID = new(apiKeyID)
			} else {
				principalURN, principalType = "user:"+s.userID(user.handle), "user"
				userID, userEmail = new(s.userID(user.handle)), new(s.email(user.handle))
			}
			// Roughly one in three is refused: enough denials to be worth
			// reading, not so many that the org looks locked out of its own
			// tools.
			denied := hash64("chal-outcome", n)%3 == 0
			outcome, reason := "allow", "grant_matched"
			if denied {
				// reason is a closed enum on the wire: an allow that carries an
				// empty reason fails response validation and the client discards
				// the whole page, which reads in the UI as "no authorization
				// checks recorded".
				outcome, reason = "deny", "no_grants"
				if n%2 == 0 {
					reason = "scope_unsatisfied"
				}
			}
			sc := hash64("chal-scope", n) % 7
			scope, kind, resource := chfChalScopes[sc], chfChalKinds[sc], resources[n%3]
			if err := b.Append(
				now.Add(-time.Duration(2+n*5)*time.Hour),
				s.orgID(),
				proj,
				md5Hex(s.name("chal-"+strconv.Itoa(n))),
				md5Hex(s.name("chalspan-" + strconv.Itoa(n)))[:16],
				"req_"+md5Hex(s.name("chalreq-" + strconv.Itoa(n)))[:16],
				principalURN, principalType, userID, userEmail, keyID,
				[]string{},
				"require",
				outcome, reason, scope, kind, resource,
				selector,
				[]string{scope},
				[]string{scope}, []string{kind}, []string{resource}, []string{selector},
				[]string{}, []string{}, []string{}, []string{},
				uint32(3+n%5),
			); err != nil {
				return fmt.Errorf("append authz challenge: %w", err)
			}
		}
		return nil
	})
}

// chfRiskType is one row of the finding-type table in postgres.sql, as the
// ClickHouse mirror stamps it. Every field is indexed by the finding type k
// and must stay aligned with that table; startPos/matchLen are the fixed
// content prefixes there, so an edit to a prefix string is an edit here too.
type chfRiskType struct {
	policy      func(*seeder) uuid.UUID
	rule        string
	description string
	source      string
	confidence  float64
	// category is categories.Classify(source, rule_id) computed at ingest;
	// hardcoded here because ClickHouse cannot call it. Source-based
	// categories win over the rule prefix, which is why k=4 is prompt_policy
	// and k=9 prompt_injection.
	category string
	tags     []string
	startPos int32
	matchLen uint32
	// redacted builds match_redacted for chat i.
	redacted func(i int) string
}

func chfStars(prefix string, n int, suffix string) func(int) string {
	v := prefix + strings.Repeat("*", n) + suffix
	return func(int) string { return v }
}

var chfRiskTypes = []chfRiskType{
	{(*seeder).policyA, "stripe-access-token", "Stripe live secret key found in tool output", "gitleaks", 0.97, "secrets", []string{"secret", "stripe"}, 58, 32, chfStars("sk_l", 26, "u0")},
	{(*seeder).policyA, "aws-access-token", "AWS secret access key in tool output", "gitleaks", 0.95, "secrets", []string{"secret", "aws"}, 48, 38, chfStars("wJal", 32, "MO")},
	{(*seeder).policyA, "pii.credit_card", "Credit card number in user message", "presidio", 0.92, "financial", []string{"pii", "pci"}, 41, 19, func(i int) string {
		if i%4 == 0 {
			return strings.Repeat("*", 15) + "1111"
		}
		return strings.Repeat("*", 15) + "6467"
	}},
	{(*seeder).policyCD, "pii.email_address", "Customer email address in tool output", "presidio", 0.88, "pii", []string{"pii"}, 41, 22, chfStars("***@example.com", 0, "")},
	{(*seeder).policyPI, "llm_judge", "Prompt injection attempt: instruction override + credential exfiltration", "llm_judge", 0.72, "prompt_policy", []string{"prompt-injection"}, 0, 32, chfStars("", 0, "")},
	{(*seeder).policyA, "pii.us_ssn", "US Social Security number in user message", "presidio", 0.94, "government_ids", []string{"pii", "govid"}, 59, 11, chfStars("412-", 5, "91")},
	{(*seeder).policyCR, "custom.sensitive_file_read", "Agent reads of SSH keys, cloud credentials, or dotenv files outside the project (OWASP LLM02).", "custom", 1.0, "custom", []string{}, 16, 32, chfStars("cat ", 26, "ls")},
	{(*seeder).policyCR, "custom.env_secret_dump", "Agent dumping the process environment, where CI/CD tokens and API keys live (OWASP LLM02).", "custom", 1.0, "custom", []string{}, 16, 24, chfStars("prin", 18, "en")},
	{(*seeder).policyCR, "custom.ssrf_metadata_endpoint", "Agent-controlled requests to cloud metadata or loopback addresses (MCP security best practices).", "custom", 1.0, "custom", []string{}, 17, 65, chfStars("http", 59, "s/")},
	{(*seeder).policyPI, "prompt-injection.indirect", "Injected instruction in retrieved content redirecting the agent to exfiltrate data", "prompt_injection", 0.81, "prompt_injection", []string{"prompt-injection", "indirect"}, 13, 56, chfStars("", 0, "")},
	{(*seeder).policyDS, "cli.destructive_command", "Destructive database command issued through a tool call", "cli_destructive", 0.99, "cli_destructive", []string{"destructive"}, 16, 34, chfStars("", 0, "")},
	{(*seeder).policyTB, "pii.topic_boundary_violation", "Conversation strayed outside the approved support topics", "presidio", 0.64, "off_policy", []string{"off-policy"}, 28, 40, chfStars("walk", 34, "an")},
	{(*seeder).policyCD, "pii.phone_number", "Customer phone number in tool output", "presidio", 0.83, "pii", []string{"pii"}, 42, 15, chfStars("+1-4", 9, "42")},
}

var (
	chfExcludedReasons = []string{"", "rule", "manual", "automated"}
	chfExcludedDetails = []string{"", "", "Known internal test fixture, not customer data", "placeholder_value"}
)

// chfRiskFindings mirrors the Postgres findings (the ClickHouse read path
// behind the risk overview, the Risk Events listing and the Watchdog): one
// row per Postgres finding, with the same md5-derived ids and the SAME
// weighted type draw as the Postgres loop.
//
// Beyond mirroring, it stamps the attribution columns the ingest pipeline
// denormalizes and the Watchdog reads without touching Postgres: chat_source
// (its App grouping), team (its Team grouping) and user_email (its top-user
// display). Tool-output findings additionally model the mediated response
// that produced that content, so the Risk Events MCP-server filter has
// representative data without misattributing user-message findings.
//
// It ends with one MCP-only blocked request with no chat anchor. This
// exercises the Risk Events and Watchdog fallback that shows execution
// context instead of an "Untitled" chat session.
func (s *seeder) chfRiskFindings(ctx context.Context, ch driver.Conn) error {
	org, proj := s.orgID(), s.projectID().String()
	return chfInsert(ctx, ch, "risk findings", `INSERT INTO risk_findings
  (id, created_at, organization_id, project_id, chat_message_id, chat_id,
   user_id, external_user_id, user_email, team, chat_source,
   risk_policy_id, risk_policy_version, rule_id,
   description, source, confidence, category, tags, start_pos, end_pos,
   match_len, match_redacted, surface, field, message_created_at,
   excluded_at, exclusion_id, false_positive_at, excluded_reason,
   excluded_detail, execution_id, mcp_server_id, toolset_id, tool_name,
   phase, mediation_surface, mcp_method, principal_kind, identity_stamped,
   enforcement_outcome)`, func(b driver.Batch) error {
		for i := 1; i <= bulkChats; i++ {
			// demo.risk_ftype(i): the weighted draw, with the trailing-3-days
			// override that makes types 7 and 8 read as newly-emerged signals.
			k := s.riskType(i)
			if k < 0 {
				continue
			}
			t := chfRiskTypes[k]
			// Types 2, 4, 5 and 11 flag the opening user message (position 1);
			// the rest a tool output at position 3 — must match the Postgres
			// f_on_user.
			onUser := k == 2 || k == 4 || k == 5 || k == 11
			msgPos := 3
			if onUser {
				msgPos = 1
			}
			// demo.risk_suppression(i), restricted to the same types, with the
			// match-driven rule suppression taking precedence.
			supp := 0
			switch {
			case (k == 2 && i%4 == 0) || (k == 3 && i%5 == 0):
				supp = 1
			case k == 3 || k == 11 || k == 12:
				supp = s.riskSuppression(i)
			}
			chatDT := s.chatTS(i)
			ts := chatDT.Add(2 * time.Minute)
			var excludedAt, falsePositiveAt *time.Time
			var exclusionID *uuid.UUID
			if supp > 0 {
				excludedAt = new(chatDT.Add(3 * time.Hour))
			}
			if supp == 1 {
				if k == 2 {
					exclusionID = new(s.exclTestcard())
				} else {
					exclusionID = new(s.exclFixture())
				}
			}
			if supp == 2 || supp == 3 {
				falsePositiveAt = new(chatDT.Add(3 * time.Hour))
			}
			// Same chat-to-server rule as telemetry_logs (i % 5 = 0 is Acme
			// Ops), so the Risk Events MCP filter agrees with the call details.
			var execID, mcpServerID, toolsetID, toolName, phase, mediation, method, principal, outcome string
			if !onUser {
				execID = s.det("mcp-execution-" + strconv.Itoa(i)).String()
				mcpServerID = s.det("mcpserver-support").String()
				toolsetID = s.toolset1().String()
				toolName = "get_customer"
				if i%5 == 0 {
					mcpServerID = s.det("mcpserver-ops").String()
					toolsetID = s.toolset2().String()
					toolName = "check_health"
				}
				phase, mediation, method, principal, outcome = "response", "hosted_mcp", "tools/call", "user_session", "logged"
			}
			owner := s.chatOwner(i)
			if err := b.Append(
				s.det("risk-"+strconv.Itoa(i)),
				ts,
				org,
				proj,
				s.det(fmt.Sprintf("msg-%d-%d", i, msgPos)).String(),
				s.chatID(i).String(),
				s.userID(owner.handle),
				s.email(owner.handle),
				s.email(owner.handle),
				// WorkOS directory department_name, matching demo_departments in
				// postgres.sql.
				owner.department,
				// demo.chat_surface(i).
				chatSurface(i),
				t.policy(s).String(),
				int64(1),
				t.rule,
				t.description,
				t.source,
				t.confidence,
				t.category,
				t.tags,
				t.startPos,
				t.startPos+int32(t.matchLen), //nolint:gosec // small constants
				t.matchLen,
				t.redacted(i),
				"content",
				"content",
				ts,
				excludedAt,
				exclusionID,
				falsePositiveAt,
				chfExcludedReasons[supp],
				chfExcludedDetails[supp],
				execID, mcpServerID, toolsetID, toolName,
				phase, mediation, method, principal,
				!onUser,
				outcome,
			); err != nil {
				return fmt.Errorf("append risk finding: %w", err)
			}
		}

		ts := s.now.UTC().Add(-2 * time.Hour)
		if err := b.Append(
			s.det("risk-181"),
			ts,
			org,
			proj,
			"",
			"",
			s.userID("amara"),
			"",
			s.email("amara"),
			"Support Engineering",
			"claude-code",
			s.policyA().String(),
			int64(1),
			"secret.github_pat",
			"GitHub access token in an MCP tool request",
			"gitleaks",
			0.98,
			"secrets",
			[]string{"secret", "github"},
			int32(24),
			int32(64),
			uint32(40),
			"ghp_"+strings.Repeat("*", 32)+"demo",
			"tool.args",
			"tool.args",
			ts,
			// The old statement left the suppression columns out: defaults.
			(*time.Time)(nil),
			(*uuid.UUID)(nil),
			(*time.Time)(nil),
			"",
			"",
			s.det("mcp-execution-181").String(),
			s.det("mcpserver-support").String(),
			s.toolset1().String(),
			"get_customer",
			"request",
			"hosted_mcp",
			"tools/call",
			"user_session",
			true,
			"denied",
		); err != nil {
			return fmt.Errorf("append mcp-only risk finding: %w", err)
		}
		return nil
	})
}

// chfSkillIdx is the skill a chat's observations name: 1 + (((i-1)/2) % 3),
// the same skill-per-chat formula as the Postgres skill_observations.
func chfSkillIdx(i int) int { return 1 + ((i-1)/2)%3 }

// chfSkillVersion is the version chat i observed: runbook (skill 3) moved to
// v2 inside the trailing 5 days.
func (s *seeder) chfSkillVersion(i, sidx int) uuid.UUID {
	if sidx == 3 && s.chatDayOff(i) <= 4 {
		return s.fixed("0000-4000-a000-0000000052b3")
	}
	return s.skillVersionID(sidx)
}

// chfSkillSessionVersions: skill efficacy mappings, one skill_session_versions
// row per Postgres skill_observation (same det-uuid ids, same skill-per-chat
// formula). surface='dev' — the insights query joins scores to mappings on
// (project, session, surface, skill, version).
func (s *seeder) chfSkillSessionVersions(ctx context.Context, ch driver.Conn) error {
	return chfInsert(ctx, ch, "skill session versions", `INSERT INTO skill_session_versions
  (id, created_at, seen_at, organization_id, project_id, session_id,
   skill_id, skill_version_id, canonical_sha256, surface)`, func(b driver.Batch) error {
		for i := 1; i <= bulkChats; i += 2 {
			sidx := chfSkillIdx(i)
			for k := 1; k <= 1+i%3; k++ {
				ts := s.chatTS(i).Add(45*time.Second + time.Duration(2*(k-1))*time.Minute)
				if err := b.Append(
					s.det(fmt.Sprintf("skillobs-%d-%d", i, k)),
					ts, ts,
					s.orgID(),
					s.projectID(),
					s.chatID(i).String(),
					s.skillID(sidx),
					s.chfSkillVersion(i, sidx),
					"",
					"dev",
				); err != nil {
					return fmt.Errorf("append skill session version: %w", err)
				}
			}
		}
		return nil
	})
}

var (
	chfSkillBaseScore = []float64{0.87, 0.66, 0.42}
	chfSkillRationale = []string{
		"Agent followed the refund checklist and refused the pasted card number.",
		"Triage steps mostly followed but escalation criteria applied loosely.",
		"Runbook steps were skipped or applied out of order in this session.",
	}
	chfSkillTurns   = []uint64{3, 2, 1}
	chfSkillMinutes = []uint64{13, 7, 2}
	chfSkillROI     = []string{"high", "med", "low"}
)

// chfSkillEfficacyScores: one judged session per odd chat, joined to the k=1
// mapping above. Per-skill quality profiles: support-refunds strong,
// triage-incident middling, runbook weak with ignored/misapplied flags.
func (s *seeder) chfSkillEfficacyScores(ctx context.Context, ch driver.Conn) error {
	return chfInsert(ctx, ch, "skill efficacy scores", `INSERT INTO skill_efficacy_scores
  (id, created_at, organization_id, project_id, session_id,
   skill_id, skill_version_id, canonical_sha256, surface, trace_id,
   gram_chat_id, score, rationale, est_turns_saved, est_minutes_saved,
   roi_confidence, flags, judge_model, judge_prompt_version)`, func(b driver.Batch) error {
		for i := 1; i <= bulkChats; i += 2 {
			sidx := chfSkillIdx(i)
			chatID := s.chatID(i).String()
			// Runbook v2 scores markedly lower than v1: surfaces the regression
			// signal.
			score := chfSkillBaseScore[sidx-1]
			if sidx == 3 && s.chatDayOff(i) <= 4 {
				score -= 0.14
			}
			score += float64(int64(hash64("js", i)%21)-10) / 100
			score = math.Min(0.99, math.Max(0.05, score))
			flags := []string{}
			jf := hash64("jf", i)
			switch {
			case sidx == 3 && jf%3 == 0:
				flags = []string{"ignored"}
			case sidx == 3 && jf%3 == 1:
				flags = []string{"misapplied"}
			case sidx == 2 && jf%5 == 0:
				flags = []string{"partially_followed"}
			case sidx == 1 && jf%7 == 0:
				flags = []string{"partially_followed"}
			}
			if err := b.Append(
				s.det("skillscore-"+strconv.Itoa(i)),
				s.chatTS(i).Add(45*time.Second+2*time.Hour),
				s.orgID(),
				s.projectID().String(),
				chatID,
				s.skillID(sidx),
				s.chfSkillVersion(i, sidx),
				"",
				"dev",
				(*string)(nil),
				chatID,
				score,
				chfSkillRationale[sidx-1],
				new(float64(chfSkillTurns[sidx-1]+hash64("jt", i)%3)),
				new(float64(chfSkillMinutes[sidx-1]+hash64("jm", i)%5)),
				new(chfSkillROI[sidx-1]),
				flags,
				"claude-sonnet-4-6",
				"v1",
			); err != nil {
				return fmt.Errorf("append skill efficacy score: %w", err)
			}
		}
		return nil
	})
}

var (
	chfClientNames = []string{"Claude Code", "Cursor", "Visual Studio Code", "mcp-inspector", "claude-ai"}
	// 1-based indexes into chfClientNames.
	chfClientDraw     = [16]int{1, 1, 1, 1, 2, 2, 2, 3, 3, 5, 1, 2, 1, 3, 2, 4}
	chfClientVersions = [][]string{
		{"2.4.1", "2.4.1", "2.3.8"},
		{"1.7.42", "1.7.42", "1.6.14"},
		{"1.104.2", "1.103.1", "1.103.1"},
		{"0.16.2", "0.16.2", "0.15.0"},
		{"1.0.0", "1.0.0", "1.0.0"},
	}
)

// chfClientAttrs is the MCP client a gateway session presents. A gateway
// handshakes once per session and every member dispatch in that session
// inherits the client, which is why these proxy hops carry one. pick,
// version, and seen are the three hash draws (client, client version, whether
// the client was seen at all).
func chfClientAttrs(pick, version, seen uint64) string {
	cidx := chfClientDraw[pick%16]
	if seen%9 == 0 {
		return ""
	}
	return `,"gram.mcp.client.name":"` + chfClientNames[cidx-1] + `"` +
		`,"gram.mcp.client.version":"` + chfClientVersions[cidx-1][version%3] + `"`
}

// chfEmailClient keys the client draw on the person, not the row: one gateway
// session walks the catalog and then calls, so discovery and dispatch have to
// agree on who did it. An independent per-row draw would have the same
// session change client between listing a server and calling it.
func chfEmailClient(email string) string {
	return chfClientAttrs(hash64("gwcli", email), hash64("gwcliv", email), hash64("gwcliseen", email))
}

// chfGatewayDiscovery is gateway (meta MCP) traffic for the Acme Agent
// Gateway, which fronts the four mcp_servers seeded in postgres.sql. Ids
// reproduce s.det("metamcp-1") / ("mcpserver-<member>") /
// ("remotemcp-<member>") so ClickHouse rows join the Postgres rows.
//
// Discovery calls (list_servers / describe_server / describe_tools) land under
// gram.event.source = meta_discovery with a "metamcp:" urn: the runtime never
// gives them a "tools:" urn, so they cannot count as tool calls (the query
// layer's classifier), and the seed mirrors that. execute_tool writes no row
// of its own: the member call below IS the row, stamped with the gateway id.
func (s *seeder) chfGatewayDiscovery(ctx context.Context, ch driver.Conn) error {
	gateway := s.det("metamcp-1").String()
	rows := make([]chfTelemetryRow, 0, 154)
	for n := range 150 {
		i := n + 1
		email := s.email(demoUsers[hash64("gwu", n)%6].handle)
		// Funnel shape: every walk lists, most describe a server, fewer pull
		// schemas.
		tool := "describe_tools"
		switch {
		case n%10 < 5:
			tool = "list_servers"
		case n%10 < 8:
			tool = "describe_server"
		}
		ts := s.chfNoFuture(s.chfDay(n % 12).
			Add(time.Duration(8+hash64("gwh", n)%11) * time.Hour).
			Add(time.Duration(hash64("gwm", n)%60) * time.Minute))
		attrs := `{"gram.event.source":"meta_discovery"` +
			`,"gram.meta_mcp_server.id":"` + gateway + `"` +
			`,"gram.tool.name":"` + tool + `"` +
			`,"gram.tool.urn":"metamcp:` + gateway + `:` + tool + `"` +
			`,"http.response.status_code":200` +
			`,"http.server.request.duration":` + chfRound3(0.02+float64(hash64("gwd", i)%40)/100) +
			`,"gram.project.id":"` + s.projectID().String() + `"` +
			`,"user.email":"` + email + `"` +
			`,"gram.external_user.id":"` + email + `"` +
			chfEmailClient(email) +
			`,"gram.hook.source":"claude-code"}`
		rows = append(rows, chfTelemetryRow{
			ts:      ts,
			body:    "Gateway discovery: " + tool,
			traceID: md5Hex(s.name("gwdisc-" + strconv.Itoa(i))),
			attrs:   attrs,
			urn:     "metamcp:" + gateway + ":" + tool,
			service: "gram-mcp-gateway",
			chatID:  nil,
		})
	}
	return s.chfInsertTelemetry(ctx, ch, "gateway discovery telemetry", append(rows, s.chfGatewayListingAttempts()...))
}

// chfClassRank orders failure classes the way a failed listing picks one.
var chfClassRank = map[string]int{"endpoint": 0, "internal": 1, "configuration": 2}

// chfGatewayMemberAttempt is one member's result in a seeded Direct listing.
type chfGatewayMemberAttempt struct {
	member, outcome, class, reason string
	calls, status                  int
}

// chfGatewayListingAttempts is Direct tools/list traffic recorded as
// connection attempts: one discovery_listing row per call and one
// discovery_member row per visible member, the shape the gateway writes. A
// failed member's row also carries the gateway discovery labels and fixed
// text the Activity drill-in searches, so one row is both facts. Attempt rows
// carry no trace, so they never form traces. The last two listings fall inside
// the current UTC half-hour, which the chart reads from raw rows, and one
// listing listed no member at all.
func (s *seeder) chfGatewayListingAttempts() []chfTelemetryRow {
	gateway := s.det("metamcp-1").String()
	providers := map[string]string{"linear": s.det("remote-identity-provider-linear").String()}
	omission := map[string]string{
		"rate_limited":             "was rate limited",
		"upstream_unavailable":     "was unavailable",
		"listing_timeout":          "did not answer in time",
		"listing_budget_exhausted": "was not reached within the gateway listing time",
	}
	listed := func(member string, calls int) chfGatewayMemberAttempt {
		return chfGatewayMemberAttempt{member: member, outcome: "listed", class: "", reason: "", calls: calls, status: 0}
	}
	failed := func(member, class, reason string, calls, status int) chfGatewayMemberAttempt {
		return chfGatewayMemberAttempt{member: member, outcome: "failed", class: class, reason: reason, calls: calls, status: status}
	}
	blocked := func(member, outcome, reason string) chfGatewayMemberAttempt {
		return chfGatewayMemberAttempt{member: member, outcome: outcome, class: "configuration", reason: reason, calls: 1, status: 0}
	}
	special := map[int][]chfGatewayMemberAttempt{
		3:  {listed("support", 1), listed("ops", 1), failed("linear", "endpoint", "rate_limited", 3, 429), listed("slack", 1)},
		7:  {listed("support", 1), listed("ops", 1), listed("linear", 2), listed("slack", 1)},
		11: {listed("support", 1), listed("ops", 1), listed("linear", 1), failed("slack", "endpoint", "upstream_unavailable", 3, 503)},
		16: {listed("support", 1), listed("ops", 1), blocked("linear", "credential_blocked", "needs_sign_in"), listed("slack", 1)},
		20: {listed("support", 1), failed("ops", "internal", "listing_budget_exhausted", 0, 0), listed("linear", 1), failed("slack", "endpoint", "listing_timeout", 1, 0)},
		23: {listed("support", 1), listed("ops", 1), listed("linear", 1), blocked("slack", "consent_blocked", "consent_not_approved")},
		29: {listed("support", 1), listed("ops", 1), listed("linear", 3), listed("slack", 2)},
		31: {failed("support", "internal", "listing_budget_exhausted", 0, 0), failed("ops", "internal", "listing_budget_exhausted", 0, 0), failed("linear", "endpoint", "listing_timeout", 1, 0), failed("slack", "endpoint", "upstream_unavailable", 3, 503)},
		33: {listed("support", 1), listed("ops", 1), failed("linear", "endpoint", "rate_limited", 2, 429), listed("slack", 1)},
		38: {listed("support", 1), listed("ops", 1), listed("linear", 1), failed("slack", "endpoint", "upstream_unavailable", 3, 503)},
		39: {listed("support", 1), listed("ops", 1), failed("linear", "endpoint", "rate_limited", 3, 429), listed("slack", 1)},
	}
	// Listings that failed before their members were known record no member
	// rows. Listing 21's client kept a cursor from an earlier paginated
	// listing; its next listing, 22, is complete. Listing 23 waits on consent
	// for Slack; the same person's listing 25 lists it.
	listingOnly := map[int][3]string{21: {"failed", "configuration", "stale_cursor"}}
	callers := map[int]string{21: demoUsers[0].handle, 22: demoUsers[0].handle, 23: demoUsers[1].handle, 25: demoUsers[1].handle}
	resource := `,"gram.project.id":"` + s.projectID().String() + `"`
	// Strictly past times inside the current half-hour, or just before now
	// when the half-hour has only just begun.
	currentBucket := s.now.UTC().Truncate(30 * time.Minute)
	sinceBucket := s.now.Sub(currentBucket)
	rows := make([]chfTelemetryRow, 0, 200)
	for n := range 40 {
		ts := s.chfNoFuture(s.now.Add(-time.Duration(40-n)*6*time.Hour - time.Duration(hash64("gwlist", n)%300)*time.Minute))
		if n >= 38 {
			ts = s.now.Add(-time.Duration(40-n) * time.Second)
			if sinceBucket > 3*time.Second {
				ts = currentBucket.Add(sinceBucket * time.Duration(n-37) / 3)
			}
		}
		handle := demoUsers[hash64("gwlistu", n)%6].handle
		if fixed, ok := callers[n]; ok {
			handle = fixed
		}
		subject := urn.NewUserSubject(s.userID(handle)).String()
		clientKey := s.det("usc-gateway").String()
		if n%5 == 4 {
			clientKey = "credential:agent"
			subject = urn.NewAgentSubject(s.det("managed-agent-1")).String()
		}
		members, ok := special[n]
		if !ok {
			members = []chfGatewayMemberAttempt{listed("support", 1), listed("ops", 1), listed("linear", 1), listed("slack", 1)}
		}
		failure, early := listingOnly[n]
		if early {
			members = nil
		}
		// The gateway's rule: partial when some member listed, failed when
		// none did, taking the endpoint's class before the gateway's.
		outcome, class, reason, listedCount := "complete", "", "", 0
		for _, m := range members {
			switch {
			case m.outcome == "listed":
				listedCount++
			case class == "" || chfClassRank[m.class] < chfClassRank[class]:
				class = m.class
			}
		}
		switch {
		case early:
			outcome, class, reason = failure[0], failure[1], failure[2]
		case listedCount == len(members):
			class = ""
		case listedCount > 0:
			outcome, class = "partial", ""
		default:
			outcome, reason = "failed", "no_member_listed"
		}
		listingID := s.det("gwlist-" + strconv.Itoa(n)).String()
		common := `"gram.event.urn":"urn:telemetry:gram_service:log:mcp_connection_attempt"` +
			`,"gram.meta_mcp_server.id":"` + gateway + `"` +
			`,"gram.connection_attempt.subject":"` + subject + `"` +
			`,"gram.connection_attempt.client_key":"` + clientKey + `"` + resource
		rows = append(rows, chfTelemetryRow{
			ts:   ts,
			body: "",
			attrs: `{` + common +
				`,"gram.connection_attempt.id":"` + listingID + `"` +
				`,"gram.connection_attempt.stage":"discovery_listing"` +
				`,"gram.connection_attempt.outcome":"` + outcome + `"` +
				`,"gram.connection_attempt.failure_class":"` + class + `"` +
				`,"gram.connection_attempt.reason":"` + reason + `"}`,
			traceID: "",
			urn:     "",
			service: "gram-mcp-gateway",
			chatID:  nil,
		})
		for _, m := range members {
			attrs := `{` + common +
				`,"gram.connection_attempt.id":"` + s.det("gwlistm-"+strconv.Itoa(n)+"-"+m.member).String() + `"` +
				`,"gram.connection_attempt.listing_id":"` + listingID + `"` +
				`,"gram.mcp_server.id":"` + s.det("mcpserver-"+m.member).String() + `"` +
				`,"gram.connection_attempt.stage":"discovery_member"` +
				`,"gram.connection_attempt.outcome":"` + m.outcome + `"` +
				`,"gram.connection_attempt.failure_class":"` + m.class + `"` +
				`,"gram.connection_attempt.reason":"` + m.reason + `"` +
				`,"gram.connection_attempt.calls":` + strconv.Itoa(m.calls) +
				`,"gram.connection_attempt.retries":` + strconv.Itoa(max(m.calls-1, 0))
			if provider, ok := providers[m.member]; ok {
				attrs += `,"gram.connection_attempt.provider_id":"` + provider + `"`
			}
			row := chfTelemetryRow{ts: ts, body: "", traceID: "", urn: "", service: "gram-mcp-gateway", chatID: nil, attrs: ""}
			if m.outcome == "failed" {
				status := m.status
				if status == 0 {
					status = 502
				}
				attrs += `,"gram.event.source":"meta_discovery"` +
					`,"gram.tool.id":"` + gateway + `"` +
					`,"gram.tool.name":"tools/list"` +
					`,"gram.tool.urn":"metamcp:` + gateway + `:tools/list"` +
					`,"http.response.status_code":` + strconv.Itoa(status) +
					`,"error.message":"server \"` + m.member + `\" ` + omission[m.reason] + ` and was left out of the tool listing"`
				row.urn = "metamcp:" + gateway + ":tools/list"
			}
			row.attrs = attrs + `}`
			rows = append(rows, row)
		}
	}
	return rows
}

var (
	// Weighted member pick: support 5, ops 3, linear 1, slack 1 of every 10.
	chfGatewayMembers = [10]string{"support", "support", "support", "support", "support", "ops", "ops", "ops", "linear", "slack"}
	chfGatewayTools   = map[string][]string{
		"support": {"search_logs", "get_customer", "query_db", "fetch_traces"},
		"ops":     {"list_deploys", "check_health", "get_metrics", "process_refund"},
		"linear":  {"list_issues", "create_issue", "get_issue"},
		"slack":   {"send_message", "list_channels"},
	}
	chfGatewayToolsets = map[string]string{"support": "acme-support-tools", "ops": "acme-ops"}
)

// chfGatewayMemberCalls: member calls dispatched through the gateway. They
// are ordinary tool_call rows for the member (hosted members keep the
// "tools:http:acme:" urn + toolset slug the direct path emits; remote members
// the "tools:externalmcp:<remote id>:" urn the proxy emits), plus
// gram.meta_mcp_server.id. One row serves the member's own usage, the gateway
// Activity section, and the MCP listing marker alike, so call totals never
// double. Ops carries most of the failures.
func (s *seeder) chfGatewayMemberCalls(ctx context.Context, ch driver.Conn) error {
	gateway := s.det("metamcp-1").String()
	rows := make([]chfTelemetryRow, 0, 240)
	for n := range 240 {
		i := n + 1
		mkey := chfGatewayMembers[hash64("gwmem", n)%10]
		member := s.det("mcpserver-" + mkey).String()
		remote := s.det("remotemcp-" + mkey).String()
		toolsetSlug := chfGatewayToolsets[mkey]
		tools := chfGatewayTools[mkey]
		tool := tools[hash64("gwt", n)%uint64(len(tools))]
		toolURNStr := "tools:externalmcp:" + remote + ":" + tool
		if toolsetSlug != "" {
			toolURNStr = toolURN(tool)
		}
		status := 200
		if (mkey == "ops" && hash64("gwerr", n)%5 == 0) || hash64("gwbg", n)%40 == 0 {
			status = 500
		}
		// Gateway endpoints see more kinds of client than a hosted server
		// does: agent harnesses plus the MCP Inspector someone left open.
		// Keyed on the person so a caller presents the same client here as in
		// discovery.
		email := s.email(demoUsers[hash64("gwu2", n)%6].handle)
		var sb strings.Builder
		sb.WriteString(`{"gram.tool.urn":"` + toolURNStr + `"`)
		sb.WriteString(`,"gram.tool.name":"` + tool + `"`)
		if toolsetSlug != "" {
			sb.WriteString(`,"gram.toolset.slug":"` + toolsetSlug + `"`)
		}
		sb.WriteString(`,"gram.event.source":"tool_call"`)
		sb.WriteString(`,"gram.mcp_server.id":"` + member + `"`)
		if toolsetSlug == "" {
			sb.WriteString(`,"gram.remote_mcp_server.id":"` + remote + `"`)
			// The proxy stamps the urn's source segment as the tool call
			// source; the tool-usage classifier reads it to attribute the call
			// to the member.
			sb.WriteString(`,"gram.tool_call.source":"` + remote + `"`)
		}
		sb.WriteString(`,"gram.meta_mcp_server.id":"` + gateway + `"`)
		sb.WriteString(`,"http.response.status_code":` + strconv.Itoa(status))
		sb.WriteString(`,"http.server.request.duration":` + chfRound3(0.05+float64(hash64("gwc", i)%300)/100))
		sb.WriteString(`,"gram.project.id":"` + s.projectID().String() + `"`)
		sb.WriteString(`,"user.email":"` + email + `"`)
		sb.WriteString(`,"gram.external_user.id":"` + email + `"`)
		sb.WriteString(chfEmailClient(email))
		sb.WriteString(`,"gram.hook.source":"claude-code"}`)
		ts := s.chfNoFuture(s.chfDay(n % 12).
			Add(time.Duration(8+hash64("gwh2", n)%11) * time.Hour).
			Add(time.Duration(hash64("gwm2", n)%60) * time.Minute))
		rows = append(rows, chfTelemetryRow{
			ts:      ts,
			body:    "Tool call: " + tool,
			traceID: md5Hex(s.name("gwcall-" + strconv.Itoa(i))),
			attrs:   sb.String(),
			urn:     toolURNStr,
			service: "gram-mcp-gateway",
			chatID:  nil,
		})
	}
	return s.chfInsertTelemetry(ctx, ch, "gateway member call telemetry", rows)
}

// chfGatewayGoneCalls: dispatches to GitHub from before it left the gateway
// (its membership is soft-deleted in Postgres three days ago). They count
// toward the gateway's dispatched-call totals but must not appear in Calls by
// member.
func (s *seeder) chfGatewayGoneCalls(ctx context.Context, ch driver.Conn) error {
	gateway := s.det("metamcp-1").String()
	member := s.det("mcpserver-github").String()
	remote := s.det("remotemcp-github").String()
	tools := []string{"list_pull_requests", "get_issue", "search_code"}
	handles := []string{"amara", "priya", "mateo"}
	rows := make([]chfTelemetryRow, 0, 24)
	for n := range 24 {
		i := n + 1
		tool := tools[hash64("gwgt", n)%3]
		urn := "tools:externalmcp:" + remote + ":" + tool
		status := 200
		if hash64("gwge", n)%8 == 0 {
			status = 502
		}
		email := s.email(handles[hash64("gwgu", n)%3])
		attrs := `{"gram.tool.urn":"` + urn + `"` +
			`,"gram.tool.name":"` + tool + `"` +
			`,"gram.event.source":"tool_call"` +
			`,"gram.mcp_server.id":"` + member + `"` +
			`,"gram.remote_mcp_server.id":"` + remote + `"` +
			`,"gram.tool_call.source":"` + remote + `"` +
			`,"gram.meta_mcp_server.id":"` + gateway + `"` +
			`,"http.response.status_code":` + strconv.Itoa(status) +
			`,"http.server.request.duration":` + chfRound3(0.08+float64(hash64("gwgc", i)%200)/100) +
			`,"gram.project.id":"` + s.projectID().String() + `"` +
			`,"user.email":"` + email + `"` +
			`,"gram.external_user.id":"` + email + `"` +
			chfClientAttrs(hash64("gwgcli", n), hash64("gwgcliv", n), hash64("gwgcliseen", n)) +
			`,"gram.hook.source":"claude-code"}`
		// Only while GitHub was still a member: four to eleven days back.
		ts := s.chfDay(4 + n%8).
			Add(time.Duration(9+hash64("gwgh", n)%9) * time.Hour).
			Add(time.Duration(hash64("gwgm", n)%60) * time.Minute)
		rows = append(rows, chfTelemetryRow{
			ts:      ts,
			body:    "Tool call: " + tool,
			traceID: md5Hex(s.name("gwgone-" + strconv.Itoa(i))),
			attrs:   attrs,
			urn:     urn,
			service: "gram-mcp-gateway",
			chatID:  nil,
		})
	}
	return s.chfInsertTelemetry(ctx, ch, "gateway departed member telemetry", rows)
}

// chfGatewayHooks: what the Claude Code hooks record when an agent calls the
// gateway itself — hook rows whose gram.mcp.server_url is the gateway endpoint
// URL. The tool usage classifier matches that URL to the gateway, so Tool Logs
// and Insights show them as the gateway rather than as an unknown (shadow) MCP
// server.
func (s *seeder) chfGatewayHooks(ctx context.Context, ch driver.Conn) error {
	gatewaySlug := s.spec.OrgSlug + "-gateway"
	gatewayURL := "https://app.getgram.ai/mcp/" + gatewaySlug
	rows := make([]chfTelemetryRow, 0, 40)
	for n := range 40 {
		i := n + 1
		// Odd chats carry Claude provenance, matching the rest of the hook rows.
		chatID := s.chatID(2*n + 1).String()
		// Same funnel shape as the discovery rows, ending in an execute_tool.
		tool := "execute_tool"
		switch {
		case n%10 < 4:
			tool = "list_servers"
		case n%10 < 6:
			tool = "describe_server"
		case n%10 < 8:
			tool = "describe_tools"
		}
		email := s.email(demoUsers[hash64("gwhu", n)%6].handle)
		attrs := `{"gram.event.source":"hook"` +
			`,"gram.hook.source":"claude-code"` +
			`,"gram.hook.event":"PostToolUse"` +
			`,"gram.tool.name":"` + tool + `"` +
			`,"gram.tool_call.source":"` + gatewaySlug + `"` +
			`,"gram.mcp.match":"` + gatewayURL + `"` +
			`,"gram.mcp.server_url":"` + gatewayURL + `"` +
			`,"gen_ai.tool.call.result":"ok"` +
			`,"gen_ai.tool.call.id":"call_demo_gw_` + strconv.Itoa(i) + `"` +
			`,"gen_ai.conversation.id":"` + chatID + `"` +
			`,"gram.project.id":"` + s.projectID().String() + `"` +
			`,"user.email":"` + email + `"}`
		ts := s.chfNoFuture(s.chfDay(n % 12).
			Add(time.Duration(8+hash64("gwhh", n)%11) * time.Hour).
			Add(time.Duration(hash64("gwhm", n)%60) * time.Minute))
		rows = append(rows, chfTelemetryRow{
			ts:      ts,
			body:    "Hook: PostToolUse " + tool,
			traceID: md5Hex(s.name("gwhook-" + strconv.Itoa(i))),
			attrs:   attrs,
			urn:     "",
			service: "",
			chatID:  &chatID,
		})
	}
	return s.chfInsertTelemetry(ctx, ch, "gateway hook telemetry", rows)
}

var chfMeters = []string{
	"gram.agent_session.storage", "gram.mcp.bandwidth.ingress", "gram.mcp.bandwidth.egress",
	"gram.risk.scan.gitleaks", "gram.risk.scan.presidio", "gram.risk.scan.prompt_injection",
	"gram.risk.scan.prompt_policy", "gram.risk.scan.custom_rules", "gram.risk.scan.cli_destructive",
}

func chfPick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// chfMeterAttributes is the attributes map of meter reading `sample` for the
// 1-based meter index.
func (s *seeder) chfMeterAttributes(meterIndex, sample int, person string) map[string]string {
	switch meterIndex {
	case 1:
		return map[string]string{
			"codec":                         "o200k_base",
			"model":                         []string{"claude-sonnet-4", "gpt-4.1"}[sample%2],
			"provider":                      []string{"anthropic", "openai"}[sample%2],
			"hook_source":                   []string{"claude-code", "cursor"}[sample%2],
			"hook_hostname":                 s.name("device-" + strconv.Itoa(1+sample%8)),
			"account_type":                  chfPick(sample%3 == 0, "personal", "enterprise"),
			"billing_mode":                  chfPick(sample%3 == 0, "flat_rate", "usage_based"),
			"workload_source":               []string{"hook", "import", "assistant", "native"}[sample%4],
			"assistant_id":                  chfPick(sample%4 == 2, s.name("managed-agent-1"), ""),
			"billing_user_id":               s.userID(person),
			"billing_user_account_email":    s.email(person),
			"billing_user_division_name":    chfPick(sample%2 == 0, "Product", "Operations"),
			"billing_user_department_name":  []string{"Engineering", "Support", "Security", "Design", "Finance", "Sales", "Research", ""}[sample%8],
			"billing_user_job_title":        chfPick(sample%2 == 0, "Engineer", "Specialist"),
			"billing_user_employee_type":    chfPick(sample%3 == 0, "Contractor", "Full-time"),
			"billing_user_cost_center_name": "DEMO-" + strconv.Itoa(1+sample%8),
			"billing_user_rbac_roles":       chfPick(sample%2 == 0, `["member"]`, `["admin","member"]`),
			"billing_user_directory_groups": chfPick(sample%2 == 0, `["Engineering"]`, `["Operations","Security"]`),
		}
	case 2, 3:
		return map[string]string{
			"mcp_server_type": chfPick(sample%2 == 0, "externalmcp", "toolset"),
			"mcp_server_id":   s.name("meter-server-" + strconv.Itoa(1+sample%8)),
			"mcp_server_slug": s.spec.OrgSlug + "-server-" + strconv.Itoa(1+sample%4),
			"custom_domain":   chfPick(sample%3 == 0, "mcp.demo.getgram.ai", ""),
			"request_path":    "/mcp/" + s.spec.OrgSlug + "-server-" + strconv.Itoa(1+sample%4),
		}
	default:
		unlinked := sample%4 == 0
		llm := meterIndex == 6 || meterIndex == 7
		return map[string]string{
			"codec":                   "o200k_base",
			"risk_policy_id":          chfPick(unlinked, "", s.policyA().String()),
			"risk_policy_version":     chfPick(unlinked, "", strconv.Itoa(1+sample%2)),
			"risk_policy_link_status": chfPick(unlinked, "unlinked", "linked"),
			"risk_policy_link_reason": chfPick(unlinked, "direct_scan", ""),
			"scan_execution_path":     chfPick(sample%2 == 0, "inline", "background"),
			"message_type":            []string{"user", "assistant", "tool_result"}[sample%3],
			"message_link_status":     chfPick(unlinked, "unlinked", "linked"),
			"message_link_reason":     chfPick(unlinked, "no_message", ""),
			"hook_source":             chfPick(sample%2 == 0, "claude-code", "cursor"),
			"message_user_id":         s.userID(person),
			"model":                   chfPick(llm, "gpt-4.1-mini", ""),
			"provider":                chfPick(llm, "openai", ""),
			"tool_name":               chfPick(sample%3 == 2, "search_code", ""),
		}
	}
}

// chfBillingMeterReadings: meter usage is independent of telemetry-derived
// invoice estimates. Every meter has 96 immutable facts across 12 days, with
// enough facets for a remainder. Product-specific volumes keep all three
// estimated-spend series visible at PAYG list prices without changing the
// number or identity of the readings.
func (s *seeder) chfBillingMeterReadings(ctx context.Context, ch driver.Conn) error {
	now := s.now.UTC()
	if err := chfInsert(ctx, ch, "billing meter readings", `INSERT INTO billing_meter_readings_by_time
  (id, organization_id, project_id, meter_id, operation_id, unit,
   measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes)`, func(b driver.Batch) error {
		for n := range 864 {
			meterIndex := 1 + n%9
			sample := n / 9
			person := demoUsers[sample%6].handle
			bytesMeter := meterIndex == 2 || meterIndex == 3
			mult := uint64(10)
			switch {
			case bytesMeter:
				mult = 2048
			case meterIndex == 1:
				mult = 100
			}
			occurred := s.chfDay(sample / 8).Add(time.Duration(8+sample%8) * time.Hour)
			if limit := now.Add(-30 * time.Minute); limit.Before(occurred) {
				occurred = limit
			}
			if err := b.Append(
				s.det("meter-reading-"+strconv.Itoa(n)),
				s.orgID(),
				s.projectID(),
				chfMeters[meterIndex-1],
				s.name("meter-operation-"+strconv.Itoa(n)),
				chfPick(bytesMeter, "bytes", "stokens"),
				chfPick(bytesMeter, "http_body_bytes", "tiktoken_o200k_base"),
				int64((1000+hash64("meter-volume", n)%9000)*mult), //nolint:gosec // bounded well below MaxInt64
				occurred,
				now,
				(*uuid.UUID)(nil),
				s.chfMeterAttributes(meterIndex, sample, person),
			); err != nil {
				return fmt.Errorf("append billing meter reading: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// One identical physical redelivery per meter. Raw FINAL still collapses
	// the ledger read, while the incremental summary intentionally counts each
	// delivery.
	if err := ch.Exec(ctx, `INSERT INTO billing_meter_readings_by_time
  (id, organization_id, project_id, meter_id, operation_id, unit,
   measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes)
SELECT id, organization_id, project_id, meter_id, operation_id, unit,
       measurement_method, value, occurred_at, produced_at, corrects_reading_id, attributes
FROM billing_meter_readings_by_time FINAL
WHERE organization_id = ? AND reading_kind = 'usage'
ORDER BY meter_id, operation_id
LIMIT 1 BY meter_id
SETTINGS do_not_merge_across_partitions_select_final = 1`, s.orgID()); err != nil {
		return fmt.Errorf("insert demo billing meter redeliveries: %w", err)
	}
	return nil
}
