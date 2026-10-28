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

	// activityNoAgent marks a session a human drove, which carries no agent.
	var activityNoAgent uuid.NullUUID

	insertChat := func(id uuid.UUID, owner demoUser, title string, surface pgtype.Text, agent uuid.NullUUID, created, updated pgtype.Interval) error {
		if err := s.q.ActivityInsertChat(ctx, repo.ActivityInsertChatParams{
			ID:             id,
			ProjectID:      proj,
			OrganizationID: org,
			UserID:         s.userID(owner.handle),
			ExternalUserID: s.email(owner.handle),
			Title:          title,
			SessionSurface: surface,
			AgentID:        agent,
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
	if err := insertChat(chatID, amara, "Claude inference conversation", activityNull, activityNoAgent, activityMinutes(20), activityMinutes(18)); err != nil {
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

	// The Anthropic compliance import labels Claude iOS and Android app chats
	// as the mobile surface.
	chatID = s.det("claude-mobile-chat")
	if err := insertChat(chatID, jonas, "Rollout notes from the train", activityNull, activityMinutes(45), activityMinutes(43)); err != nil {
		return err
	}
	mobileSrc := activityText("claude-chat-mobile")
	if err := insertMessages(chatID, []activityMessage{
		{"claude-mobile-prompt", "user", "Draft a short status update for the rollout.",
			activityTextRaw("Draft a short status update for the rollout."), nil, mobileSrc, model, activityMinutes(45)},
		{"claude-mobile-reply", "assistant", "Rollout is on track: tests are green and the rollback plan is reviewed.",
			activityTextRaw("Rollout is on track: tests are green and the rollback plan is reviewed."), nil, mobileSrc, model, activityMinutes(43)},
	}); err != nil {
		return err
	}

	// Claude Tag preserves the wake/tool transcript for the Raw view toggle.
	// The channel session and its helper have no assistant record, so the
	// acting agent is what the session list names for them — the same agent
	// hook capture would stamp from the authenticated actor.
	releaseAgent := uuid.NullUUID{UUID: s.det("managed-agent-1"), Valid: true}
	chatID = s.det("claude-tag-chat")
	if err := insertChat(chatID, amara, "Claude Tag in #demo-releases", activityNull, releaseAgent, activityMinutes(10), activityMinutes(9)); err != nil {
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
	if err := insertChat(helperID, amara, "Release checklist helper", tagSrc, releaseAgent, activityMinutes(6), activityMinutes(5)); err != nil {
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
		if err := insertChat(s.det("assistant-session-"+sess.fixture), jonas, sess.title, activityNull, activityNoAgent, activityMinutes(40), activityMinutes(38)); err != nil {
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
