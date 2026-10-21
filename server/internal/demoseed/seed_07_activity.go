package demoseed

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// activityMinutes, activityHours and activityDays are intervals subtracted
// from now() in SQL. Days stay calendar days (not 24h) so the arithmetic is
// the same as the old interval literals in every session time zone.
func activityMinutes(n int) pgtype.Interval {
	return pgtype.Interval{Microseconds: int64(n) * int64(time.Minute/time.Microsecond), Days: 0, Months: 0, Valid: true}
}

func activityHours(n int) pgtype.Interval {
	return pgtype.Interval{Microseconds: int64(n) * int64(time.Hour/time.Microsecond), Days: 0, Months: 0, Valid: true}
}

func activityDays(n int) pgtype.Interval {
	return pgtype.Interval{Microseconds: 0, Days: int32(n), Months: 0, Valid: true} //nolint:gosec // small fixed day counts
}

func activityText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }

// activityNull is a NULL text column.
var activityNull = pgtype.Text{String: "", Valid: false}

// activityTextRaw is the content_raw JSON of a single text part.
func activityTextRaw(text string) []byte {
	return []byte(`[{"type":"text","text":"` + text + `"}]`)
}

const activityModel = "claude-sonnet-4-6"

const activityClaudeTagPrompt = `<system-reminder source="demo">Harness notes stay in the Raw view.</system-reminder>
<session-context nonce="demo-context">
Channel: #demo-releases (id: ` + "`DEMO_CHANNEL`" + `)
Workspace: ` + "`T0DEMO0001`" + `
## Session notes
Channel context stays in the Raw view.
</session-context nonce="demo-context">
<wake reason="channel-activity"><channel id="DEMO_CHANNEL" type="group"><thread ts="demo-message-1"><message from="human" author="Demo User" author-id="U0DEMO00001" id="demo-message-1" trigger="true">Help summarize the release</message></thread></channel></wake>`

const activityClaudeTagReplyCalls = `[{"id":"demo-tag-reply","type":"function","function":{"name":"mcp__slackbot__reply","arguments":"{\"text\":\"The release improves session transcripts and channel visibility.\",\"thread_ts\":\"demo-message-1\"}"}}]`

// seedActivity: Audit feed, hooks, trials, spend rules, widgets.
func (s *seeder) seedActivity(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()

	// Audit activity feed (org home per-project latest action + right rail).
	// Inserted in chronological order: the feed orders by seq DESC.
	if err := s.q.ActivityDeleteAuditLogs(ctx, org); err != nil {
		return fmt.Errorf("delete demo audit logs: %w", err)
	}
	for i := 12; i >= 1; i-- {
		u := demoUsers[i%6]
		subject := "Acme Support Tools"
		if i%2 == 0 {
			subject = "Acme Ops"
		}
		if err := s.q.ActivityInsertProjectAuditLog(ctx, repo.ActivityInsertProjectAuditLogParams{
			ID:                 s.det("audit-" + strconv.Itoa(i)),
			OrganizationID:     org,
			ProjectID:          proj,
			ActorID:            s.userID(u.handle),
			ActorDisplayName:   u.name,
			Action:             auditActions[i%len(auditActions)],
			SubjectID:          "demo-subject-" + strconv.Itoa(i),
			SubjectDisplayName: subject,
			Age:                activityHours(19 * i),
		}); err != nil {
			return fmt.Errorf("insert demo audit log %d: %w", i, err)
		}
	}

	// A pending hook demonstrates setup without provisioning usable credentials.
	if err := s.q.ActivityDeleteAnthropicInferenceConfig(ctx, org); err != nil {
		return fmt.Errorf("delete demo anthropic inference config: %w", err)
	}
	if err := s.q.ActivityInsertAnthropicInferenceConfig(ctx, repo.ActivityInsertAnthropicInferenceConfigParams{
		ID:             s.det("anthropic-inference-config"),
		OrganizationID: org,
		ProjectID:      proj,
	}); err != nil {
		return fmt.Errorf("insert demo anthropic inference config: %w", err)
	}

	amara := demoUsers[0]
	jonas := demoUsers[1]

	insertChat := func(id uuid.UUID, owner demoUser, title string, surface pgtype.Text, created, updated pgtype.Interval) error {
		if err := s.q.ActivityInsertChat(ctx, repo.ActivityInsertChatParams{
			ID:             id,
			ProjectID:      proj,
			OrganizationID: org,
			UserID:         s.userID(owner.handle),
			ExternalUserID: s.email(owner.handle),
			Title:          title,
			SessionSurface: surface,
			CreatedAge:     created,
			UpdatedAge:     updated,
		}); err != nil {
			return fmt.Errorf("insert demo chat %q: %w", title, err)
		}
		return nil
	}
	type activityMessage struct {
		key        string
		role       string
		content    string
		contentRaw []byte
		toolCalls  []byte
		source     pgtype.Text
		model      pgtype.Text
		age        pgtype.Interval
	}
	insertMessages := func(chatID uuid.UUID, msgs []activityMessage) error {
		for _, m := range msgs {
			if err := s.q.ActivityInsertChatMessage(ctx, repo.ActivityInsertChatMessageParams{
				ID:         s.det(m.key),
				ChatID:     chatID,
				ProjectID:  proj,
				Role:       m.role,
				Content:    m.content,
				ContentRaw: m.contentRaw,
				ToolCalls:  m.toolCalls,
				Source:     m.source,
				Model:      m.model,
				Age:        m.age,
			}); err != nil {
				return fmt.Errorf("insert demo chat message %s: %w", m.key, err)
			}
		}
		return nil
	}

	// A signed inference hook archives the conversation before the next model call.
	chatID := s.det("anthropic-inference-chat")
	if err := insertChat(chatID, amara, "Claude inference conversation", activityNull, activityMinutes(20), activityMinutes(18)); err != nil {
		return err
	}
	chatSrc, model := activityText("claude-chat"), activityText(activityModel)
	if err := insertMessages(chatID, []activityMessage{
		{"anthropic-inference-prompt", "user", "Summarize the release checklist.",
			activityTextRaw("Summarize the release checklist."), nil, chatSrc, model, activityMinutes(20)},
		{"anthropic-inference-reply", "assistant", "The checklist covers tests, rollout, and rollback readiness.",
			activityTextRaw("The checklist covers tests, rollout, and rollback readiness."), nil, chatSrc, model, activityMinutes(19)},
		{"anthropic-inference-followup", "user", "Which checks remain before rollout?",
			activityTextRaw("Which checks remain before rollout?"), nil, chatSrc, model, activityMinutes(18)},
	}); err != nil {
		return err
	}

	// Claude Tag preserves the wake/tool transcript for the Raw view toggle.
	chatID = s.det("claude-tag-chat")
	if err := insertChat(chatID, amara, "Claude Tag in #demo-releases", activityNull, activityMinutes(10), activityMinutes(9)); err != nil {
		return err
	}
	tagSrc := activityText("claude-tag")
	if err := insertMessages(chatID, []activityMessage{
		{"claude-tag-prompt", "user", activityClaudeTagPrompt, nil, nil, activityText("Claude In Slack"), model, activityMinutes(10)},
		{"claude-tag-reply", "assistant", "", nil, []byte(activityClaudeTagReplyCalls), tagSrc, model, activityMinutes(9)},
		{"claude-tag-ack", "assistant", "Replied in the thread.", nil, nil, tagSrc, model, activityMinutes(9)},
		// Standing-owner deliveries preserve sender provenance for each turn.
		{"claude-tag-owner-1", "user", `<system-reminder>Delivery notes stay in the Raw view.</system-reminder>
<standing_owner_message sender="U0DEMO00001" ts="demo-standing-1" originating-ask="true">Check the rollout status.</standing_owner_message>`,
			nil, nil, tagSrc, model, activityMinutes(8)},
		{"claude-tag-owner-2", "user", `<standing_owner_message sender="U0DEMO00003" ts="demo-standing-2" originating-ask="true">Review the rollback steps.</standing_owner_message>`,
			nil, nil, tagSrc, model, activityMinutes(7)},
	}); err != nil {
		return err
	}
	if err := s.q.ActivitySetClaudeTagChatSlack(ctx, repo.ActivitySetClaudeTagChatSlackParams{ChatID: chatID, ProjectID: proj}); err != nil {
		return fmt.Errorf("set demo claude tag chat slack: %w", err)
	}
	if err := s.q.ActivityInsertClaudeTagParticipants(ctx, repo.ActivityInsertClaudeTagParticipantsParams{
		ProjectID:       proj,
		ChatID:          chatID,
		Owner1MessageID: s.det("claude-tag-owner-1"),
		Member1ID:       s.det("slackmember-1"),
		Owner2MessageID: s.det("claude-tag-owner-2"),
		Member3ID:       s.det("slackmember-3"),
		PromptMessageID: s.det("claude-tag-prompt"),
		OrganizationID:  org,
	}); err != nil {
		return fmt.Errorf("insert demo claude tag participants: %w", err)
	}

	helperID := s.det("claude-tag-helper")
	if err := insertChat(helperID, amara, "Release checklist helper", tagSrc, activityMinutes(6), activityMinutes(5)); err != nil {
		return err
	}
	if err := insertMessages(helperID, []activityMessage{
		{"claude-tag-helper-reply", "assistant", "The rollback checklist is ready for review.", nil, nil, tagSrc, model, activityMinutes(5)},
	}); err != nil {
		return err
	}
	if err := s.q.ActivityInsertClaudeTagSessionLink(ctx, repo.ActivityInsertClaudeTagSessionLinkParams{
		ProjectID:      proj,
		OrganizationID: org,
		ParentChatID:   chatID,
		ChildChatID:    helperID,
	}); err != nil {
		return fmt.Errorf("insert demo claude tag session link: %w", err)
	}

	// One dashboard session per assistant: the bound one names its agent and the
	// member it acted for; the legacy one links to the assistant itself.
	sessions := []struct{ fixture, title string }{
		{"bound", "Weekly project activity summary"},
		{"legacy", "Open risk findings recap"},
	}
	for _, sess := range sessions {
		if err := insertChat(s.det("assistant-session-"+sess.fixture), jonas, sess.title, activityNull, activityMinutes(40), activityMinutes(38)); err != nil {
			return err
		}
	}
	sessionMessages := []struct{ fixture, role, content string }{
		{"bound", "user", "Summarize this week's project activity."},
		{"bound", "assistant", "Tool usage rose this week and two new MCP servers were connected."},
		{"legacy", "user", "Recap the open risk findings."},
		{"legacy", "assistant", "Three findings remain open, all in coding agent sessions."},
	}
	for _, m := range sessionMessages {
		age := activityMinutes(40)
		if m.role == "assistant" {
			age = activityMinutes(39)
		}
		if err := insertMessages(s.det("assistant-session-"+m.fixture), []activityMessage{
			{"assistant-session-" + m.fixture + "-" + m.role, m.role, m.content, nil, nil, activityNull, activityNull, age},
		}); err != nil {
			return err
		}
	}
	for _, fixture := range []string{"bound", "legacy"} {
		if err := s.q.ActivityInsertAssistantThread(ctx, repo.ActivityInsertAssistantThreadParams{
			ID:            s.det("assistant-thread-" + fixture),
			AssistantID:   s.det("assistant-" + fixture),
			ProjectID:     proj,
			CorrelationID: "demo-dashboard-" + fixture,
			ChatID:        s.det("assistant-session-" + fixture),
		}); err != nil {
			return fmt.Errorf("insert demo assistant thread %s: %w", fixture, err)
		}
	}
	if err := s.seedActivityFleet(ctx); err != nil {
		return err
	}

	// Historical shortened trial: audit history shows both dates, not an extension.
	if err := s.q.ActivityInsertTrialEndChangedAuditLog(ctx, repo.ActivityInsertTrialEndChangedAuditLogParams{
		ID:               s.det("audit-trial-end-changed"),
		OrganizationID:   org,
		ActorID:          s.userID(amara.handle),
		ActorDisplayName: amara.name,
	}); err != nil {
		return fmt.Errorf("insert demo trial end audit log: %w", err)
	}

	// Quarantine lifecycle events use their own audit subject and action rather
	// than reusing a generic policy-block row.
	if err := s.q.ActivityInsertQuarantineAuditLog(ctx, repo.ActivityInsertQuarantineAuditLogParams{
		ID:               s.det("audit-session-quarantine-open"),
		OrganizationID:   org,
		ProjectID:        proj,
		ActorID:          s.userID(jonas.handle),
		ActorDisplayName: jonas.name,
		SubjectID:        s.det("session-quarantine-1").String(),
		SessionID:        s.name("quarantine-session-1"),
		RiskPolicyID:     s.policyQ().String(),
	}); err != nil {
		return fmt.Errorf("insert demo quarantine audit log: %w", err)
	}

	// Killswitch lifecycle history mirrors the transaction hook: mutation rows
	// carry a bounded after snapshot plus their replay operation, while expiry
	// carries the version and database-time deadline. Internal notes stay out of
	// organization-visible audit snapshots.
	lucas := demoUsers[5]
	killswitchRows := []struct {
		key, prescription, action, state, operation string
		version                                     int32
		age                                         pgtype.Interval
	}{
		{"active-selected-v1", "active-selected", "killswitch:activate", "active", "activate", 1, activityDays(8)},
		{"active-all-v1", "active-all", "killswitch:activate", "active", "activate", 1, activityDays(7)},
		{"scheduled-all-v1", "scheduled-all", "killswitch:activate", "active", "activate", 1, activityHours(1)},
		{"changed-v1", "changed", "killswitch:activate", "active", "activate", 1, activityDays(6)},
		{"changed-v2", "changed", "killswitch:change", "active", "change", 2, activityDays(2)},
		{"lifted-v1", "lifted", "killswitch:activate", "active", "activate", 1, activityDays(5)},
		{"lifted-v2", "lifted", "killswitch:deactivate", "inactive", "deactivate", 2, activityHours(12)},
		{"expired-v1", "expired", "killswitch:activate", "active", "activate", 1, activityDays(4)},
	}
	for _, k := range killswitchRows {
		if err := s.q.ActivityInsertKillswitchAuditLog(ctx, repo.ActivityInsertKillswitchAuditLogParams{
			ID:               s.det("audit-killswitch-" + k.key),
			OrganizationID:   org,
			ActorID:          s.userID(lucas.handle),
			ActorDisplayName: lucas.name,
			Action:           k.action,
			SubjectID:        s.det("killswitch-" + k.prescription).String(),
			Version:          k.version,
			State:            k.state,
			Operation:        k.operation,
			OperationID:      s.det("killswitch-operation-" + k.key),
			Age:              k.age,
		}); err != nil {
			return fmt.Errorf("insert demo killswitch audit log %s: %w", k.key, err)
		}
	}
	for _, k := range agentKillswitches {
		if err := s.q.ActivityInsertKillswitchAuditLog(ctx, repo.ActivityInsertKillswitchAuditLogParams{
			ID:               s.det("audit-killswitch-agent-" + k.key),
			OrganizationID:   org,
			ActorID:          s.userID(lucas.handle),
			ActorDisplayName: lucas.name,
			Action:           "killswitch:activate",
			SubjectID:        s.det("killswitch-agent-" + k.key).String(),
			Version:          1,
			State:            "active",
			Operation:        "activate",
			OperationID:      s.det("killswitch-operation-agent-" + k.key),
			Age:              activityHours(2),
		}); err != nil {
			return fmt.Errorf("insert demo agent killswitch audit log %s: %w", k.key, err)
		}
	}
	if err := s.q.ActivityInsertKillswitchExpiredAuditLog(ctx, repo.ActivityInsertKillswitchExpiredAuditLogParams{
		ID:             s.det("audit-killswitch-expired-marker"),
		OrganizationID: org,
		SubjectID:      s.det("killswitch-expired").String(),
	}); err != nil {
		return fmt.Errorf("insert demo killswitch expired audit log: %w", err)
	}

	// Spend rules calibrated to the seeded ClickHouse usage (top spenders in
	// the low thousands of dollars per month) so the rules page shows one
	// breach and one warning per rule.
	// Events are pre-inserted because the evaluator is flag-gated; the dedupe
	// key (rule, type, email, window_start) keeps a live evaluator idempotent.
	if err := s.q.ActivityDeleteSpendRuleEvents(ctx, org); err != nil {
		return fmt.Errorf("delete demo spend rule events: %w", err)
	}
	if err := s.q.ActivityDeleteSpendRules(ctx, org); err != nil {
		return fmt.Errorf("delete demo spend rules: %w", err)
	}
	if err := s.q.ActivityInsertSpendRules(ctx, repo.ActivityInsertSpendRulesParams{
		RuleMonthly:       s.ruleMonthly(),
		OrganizationID:    org,
		MonthlyTargetExpr: `email.endsWith("` + s.spec.EmailDomain + `")`,
		RuleWeekly:        s.ruleWeekly(),
	}); err != nil {
		return fmt.Errorf("insert demo spend rules: %w", err)
	}
	if err := s.q.ActivityInsertSpendRuleEvents(ctx, repo.ActivityInsertSpendRuleEventsParams{
		OrganizationID: org,
		RuleMonthly:    s.ruleMonthly(),
		MateoID:        s.userID("mateo"),
		MateoEmail:     s.email("mateo"),
		LucasID:        s.userID("lucas"),
		LucasEmail:     s.email("lucas"),
		RuleWeekly:     s.ruleWeekly(),
		JonasID:        s.userID("jonas"),
		JonasEmail:     s.email("jonas"),
		AmaraID:        s.userID("amara"),
		AmaraEmail:     s.email("amara"),
	}); err != nil {
		return fmt.Errorf("insert demo spend rule events: %w", err)
	}

	// Explore widgets: see ActivityInsertWidgets for why each row looks as it does.
	if err := s.q.ActivityInsertWidgets(ctx, repo.ActivityInsertWidgetsParams{
		Widget1ID:      s.det("explore-widget-1"),
		ProjectID:      proj,
		OrganizationID: org,
		AmaraID:        s.userID("amara"),
		Widget2ID:      s.det("explore-widget-2"),
		PriyaID:        s.userID("priya"),
		Widget3ID:      s.det("explore-widget-3"),
		JonasID:        s.userID("jonas"),
		Widget4ID:      s.det("explore-widget-4"),
		MateoID:        s.userID("mateo"),
		Widget5ID:      s.det("explore-widget-5"),
		HanaID:         s.userID("hana"),
		Widget6ID:      s.det("explore-widget-6"),
	}); err != nil {
		return fmt.Errorf("insert demo explore widgets: %w", err)
	}

	return nil
}

// fleetAssistant is one configured assistant Fleet lists. creator is a
// demoUsers handle.
type fleetAssistant struct {
	n            int
	creator      string
	name         string
	instructions string
	status       string
}

var fleetAssistants = []fleetAssistant{
	{1, "jonas", "Support handoff assistant", "Summarize support cases, separate evidence from hypotheses, and propose the next human follow-up.", "active"},
	{2, "priya", "Deployment review assistant", "Review rollout evidence and rollback readiness. Explain uncertainty and never execute deployment changes.", "active"},
	{3, "hana", "Billing review assistant", "Reconcile synthetic invoice entries and draft a review checklist. Never initiate a payment or refund.", "paused"},
}

// fleetCapture is one captured conversation Fleet lists. assistant links it
// through an explicit assistant thread (0 for none). user is the demoUsers
// handle the capture is attributed to; an unattributed capture keeps
// external as its external user id, or NULL when that is empty too.
type fleetCapture struct {
	n         int
	assistant int
	user      string
	external  string
	title     string
	age       time.Duration
	source    string
	turns     [4]string
}

// The Fleet captures without a directory user: an external contractor, an
// external id that happens to equal a directory user id, and no attribution.
const (
	fleetExternalCapture  = 5
	fleetCollisionCapture = 6
	fleetUnknownCapture   = 7
)

// fleetContractor is the external user id of the contractor capture.
func (s *seeder) fleetContractor() string { return s.name("external-contractor") }

// fleetFindingMatch is the synthetic contact address in the deployment review
// prompt. The reserved example domain matches the email detector without
// carrying a credential.
const fleetFindingMatch = "billing-review@example.com"

// fleetCaptures builds the captures. Capture users deliberately differ from
// assistant creators, and external ids never resolve to a directory or
// registered-agent owner.
func (s *seeder) fleetCaptures() []fleetCapture {
	return []fleetCapture{
		{1, 1, "amara", "", "Support handoff: duplicate charge", 4 * time.Minute, "dashboard", [4]string{
			"Order DEMO-4823 shows two authorization holds. Draft a support handoff without promising a refund.",
			"The two holds share a checkout attempt, but only one capture is recorded. Ask billing to verify settlement before offering a refund.",
			"Add the next owner and a customer-safe reply.",
			"Billing will check settlement. Suggested reply: we are reviewing the duplicate hold and will confirm the result before taking action.",
		}},
		{2, 2, "mateo", "", "Deployment review: checkout retry logs", 6 * time.Minute, "dashboard", [4]string{
			"Draft a rollout review for " + fleetFindingMatch + " using the synthetic retry log.",
			"Keep the contact address out of the shared review. The retry change needs a staging check and a rollback owner.",
			"Record the review outcome without deploying.",
			"Review only: rollout is pending the staging retry check and contact redaction. No deployment action was taken.",
		}},
		{3, 3, "lucas", "", "Billing review: invoice reconciliation", 8 * time.Minute, "dashboard", [4]string{
			"Compare synthetic invoice DEMO-1042 with the ledger and draft a review note.",
			"The subtotal matches. A credit memo remains unapplied, so the balance needs human review before collection.",
			"Who should follow up?",
			"Billing Operations should confirm the credit memo. This is a review note, not a payment instruction.",
		}},
		{4, 1, "jonas", "", "Support handoff: webhook retry", 10 * time.Minute, "dashboard", [4]string{
			"Summarize the webhook retry report for the next support shift.",
			"Delivery resumed after the retry window. Two earlier events still need reconciliation against the receiver log.",
			"Keep the unresolved checks visible.",
			"Handoff: compare the two event ids, confirm receipt with the receiver, then close the case. Recovery is observed; completeness is not yet confirmed.",
		}},
		{fleetExternalCapture, 0, "", s.fleetContractor(), "External capture: cache investigation", 12 * time.Minute, "cursor", [4]string{
			"Compare the synthetic cache hit rates before and after the rollout.",
			"The sample improves from 82 to 91 percent. The observation window is small; keep monitoring before changing capacity.",
			"List one follow-up.",
			"Compare the next full-hour sample with the same-hour baseline and check whether the traffic mix changed.",
		}},
		{fleetCollisionCapture, 0, "", s.userID("priya"), "External capture: migration checklist", 14 * time.Minute, "codex", [4]string{
			"Draft a read-only checklist for the test schema migration.",
			"Verify the backup, inspect the migration plan, and identify the rollback owner before any write.",
			"Did you run it?",
			"No commands were run; this is a review checklist.",
		}},
		{fleetUnknownCapture, 0, "", "", "Unattributed capture: incident notes", 16 * time.Minute, "claude-code", [4]string{
			"Summarize the synthetic incident notes without attributing them to a person.",
			"The notes describe elevated queue latency followed by recovery. The capture has no user attribution.",
			"What remains unknown?",
			"The initiating identity and runtime state are not present in this captured transcript.",
		}},
		{8, 3, "hana", "", "Billing review: previous weekly close", 48 * time.Hour, "dashboard", [4]string{
			"Archive the prior synthetic weekly close review.",
			"The previous batch reconciled after one credit adjustment. This older conversation is retained for history.",
			"Any new activity?",
			"None is represented by this historical transcript.",
		}},
	}
}

// fleetChatID is capture n's chat id.
func (s *seeder) fleetChatID(n int) uuid.UUID { return s.det("fleet-chat-" + strconv.Itoa(n)) }

// seedActivityFleet writes the Fleet assistants and captures: configured
// identities with archived conversations, not live runtimes, so no triggers,
// deliveries, runs or runtime rows. Explicit assistant_threads establish a
// capture's assistantId. It ends with one synthetic contact-data finding,
// mirrored in ClickHouse by chfFleetRiskFinding.
func (s *seeder) seedActivityFleet(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()

	for _, a := range fleetAssistants {
		updated := 24 * time.Hour
		if a.status == "paused" {
			updated = 2 * time.Minute
		}
		if err := s.q.ActivityInsertFleetAssistant(ctx, repo.ActivityInsertFleetAssistantParams{
			ID:              s.det("fleet-assistant-" + strconv.Itoa(a.n)),
			ProjectID:       proj,
			OrganizationID:  org,
			CreatedByUserID: s.userID(a.creator),
			Name:            a.name,
			Instructions:    a.instructions,
			Status:          a.status,
			UpdatedAt:       s.ago(updated),
		}); err != nil {
			return fmt.Errorf("insert demo fleet assistant %d: %w", a.n, err)
		}
	}

	captures := s.fleetCaptures()
	chats := make([]repo.RiskCopyChatsParams, 0, len(captures))
	msgs := make([]repo.RiskCopyChatMessagesParams, 0, 4*len(captures))
	for _, c := range captures {
		userID, external := activityNull, activityNull
		switch {
		case c.user != "":
			userID, external = activityText(s.userID(c.user)), activityText(s.email(c.user))
		case c.external != "":
			external = activityText(c.external)
		}
		chats = append(chats, repo.RiskCopyChatsParams{
			ID:             s.fleetChatID(c.n),
			ProjectID:      proj,
			OrganizationID: org,
			UserID:         userID,
			ExternalUserID: external,
			Title:          activityText(c.title),
			CreatedAt:      s.ago(c.age + 3*time.Minute),
			UpdatedAt:      s.ago(c.age),
		})
		for i, content := range c.turns {
			m := i + 1
			role := "assistant"
			if m%2 == 1 {
				role = "user"
			}
			msgs = append(msgs, repo.RiskCopyChatMessagesParams{
				ID:               s.det(fmt.Sprintf("fleet-message-%d-%d", c.n, m)),
				ChatID:           s.fleetChatID(c.n),
				ProjectID:        uuid.NullUUID{UUID: proj, Valid: true},
				Role:             role,
				Content:          content,
				Model:            activityText(activityModel),
				MessageID:        activityNull,
				ToolCallID:       activityNull,
				Source:           activityText(c.source),
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				CreatedAt:        s.ago(c.age + time.Duration(4-m)*time.Minute),
				RiskAnalyzedAt:   s.ago(0),
			})
		}
	}
	if _, err := s.q.RiskCopyChats(ctx, chats); err != nil {
		return fmt.Errorf("insert demo fleet chats: %w", err)
	}
	if _, err := s.q.RiskCopyChatMessages(ctx, msgs); err != nil {
		return fmt.Errorf("insert demo fleet chat messages: %w", err)
	}

	for _, c := range captures {
		if c.assistant == 0 {
			continue
		}
		num := strconv.Itoa(c.n)
		if err := s.q.ActivityInsertFleetAssistantThread(ctx, repo.ActivityInsertFleetAssistantThreadParams{
			ID:            s.det("fleet-thread-" + num),
			AssistantID:   s.det("fleet-assistant-" + strconv.Itoa(c.assistant)),
			ProjectID:     proj,
			CorrelationID: s.name("fleet-correlation-" + num),
			ChatID:        s.fleetChatID(c.n),
			LastEventAt:   s.ago(c.age),
			CreatedAt:     s.ago(c.age + 3*time.Minute),
		}); err != nil {
			return fmt.Errorf("insert demo fleet assistant thread %d: %w", c.n, err)
		}
	}

	prompt := captures[1].turns[0]
	start := riskStrpos(prompt, fleetFindingMatch)
	if _, err := s.q.RiskCopyRiskResults(ctx, []repo.RiskCopyRiskResultsParams{{
		ID:                  s.det("fleet-risk"),
		ProjectID:           proj,
		OrganizationID:      org,
		RiskPolicyID:        s.policyCD(),
		RiskPolicyVersion:   1,
		ChatMessageID:       uuid.NullUUID{UUID: s.det("fleet-message-2-1"), Valid: true},
		Source:              "presidio",
		Found:               true,
		RuleID:              activityText("pii.email_address"),
		Description:         activityText(fleetFindingDescription),
		Match:               activityText(fleetFindingMatch),
		StartPos:            pgtype.Int4{Int32: int32(start), Valid: true},                          //nolint:gosec // short fixed strings
		EndPos:              pgtype.Int4{Int32: int32(start + len(fleetFindingMatch)), Valid: true}, //nolint:gosec // short fixed strings
		Confidence:          pgtype.Float8{Float64: 0.88, Valid: true},
		Tags:                []string{"pii"},
		CreatedAt:           s.ago(6 * time.Minute),
		ExcludedAt:          s.agoOrNull(0),
		ExcludedExclusionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		FalsePositiveAt:     s.agoOrNull(0),
		FalsePositiveReason: activityNull,
	}}); err != nil {
		return fmt.Errorf("insert demo fleet risk result: %w", err)
	}
	return nil
}

// fleetFindingDescription names the Fleet finding in both stores.
const fleetFindingDescription = "Synthetic contact address in a deployment review prompt"
