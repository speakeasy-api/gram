package platformmcp

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func newRecallService(t *testing.T, conn *pgxpool.Pool, enabled *bool) *SessionRecallService {
	t.Helper()
	return NewSessionRecallService(testenv.NewLogger(t), conn, platformrepo.New(conn), audit.NewLogger(), func(_ context.Context, _ string) (bool, error) {
		return *enabled, nil
	}, allowBudget())
}

// seedRecallChat inserts a captured chat the way hook ingest records one: the
// chat id derives from the session id, and the session id is stored as the
// external chat id.
func seedRecallChat(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, userID, sessionID, title string, userAccountID uuid.NullUUID) uuid.UUID {
	t.Helper()
	chatID := chat.SessionIDToChatID(sessionID)
	_, err := testrepo.New(conn).SeedCapturedAgentChatFixture(ctx, testrepo.SeedCapturedAgentChatFixtureParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
		ExternalChatID: conv.ToPGText(sessionID),
		Title:          conv.ToPGText(title),
		Cwd:            conv.ToPGText("/home/dev/code/api"),
		UserAccountID:  userAccountID,
	})
	require.NoError(t, err)
	return chatID
}

type recallMessageSeed struct {
	role            string
	content         string
	at              time.Time
	generation      int32
	toolCalls       string
	source          string
	analyzed        bool
	contentAssetURL string
}

func seedRecallMessage(t *testing.T, ctx context.Context, conn *pgxpool.Pool, chatID, projectID uuid.UUID, seed recallMessageSeed) uuid.UUID {
	t.Helper()
	var toolCalls []byte
	if seed.toolCalls != "" {
		toolCalls = []byte(seed.toolCalls)
	}
	analyzedAt := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if seed.analyzed {
		analyzedAt = conv.ToPGTimestamptz(seed.at.Add(time.Minute))
	}
	id, err := testrepo.New(conn).SeedCapturedAgentChatMessageFixture(ctx, testrepo.SeedCapturedAgentChatMessageFixtureParams{
		ChatID:          chatID,
		ProjectID:       uuid.NullUUID{UUID: projectID, Valid: true},
		Role:            seed.role,
		Content:         seed.content,
		Generation:      seed.generation,
		ToolCalls:       toolCalls,
		Source:          conv.ToPGTextEmpty(seed.source),
		ContentAssetUrl: conv.ToPGTextEmpty(seed.contentAssetURL),
		RiskAnalyzedAt:  analyzedAt,
		CreatedAt:       conv.ToPGTimestamptz(seed.at),
	})
	require.NoError(t, err)
	return id
}

// seedRecallFinding records one open finding against a message, under an
// enabled policy, with the given content span.
func seedRecallFinding(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID string, messageID uuid.UUID, source, ruleID, match string, start, end int) {
	t.Helper()
	policyID, err := testrepo.New(conn).SeedRiskPolicyFixture(ctx, testrepo.SeedRiskPolicyFixtureParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		Name:           "recall test policy " + uuid.NewString()[:8],
		Sources:        []string{source},
	})
	require.NoError(t, err)
	spans := `[{"match":"` + match + `","field":"content","start_pos":` + itoa(start) + `,"end_pos":` + itoa(end) + `}]`
	_, err = testrepo.New(conn).SeedRiskResultFixture(ctx, testrepo.SeedRiskResultFixtureParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		RiskPolicyID:   policyID,
		ChatMessageID:  uuid.NullUUID{UUID: messageID, Valid: true},
		Source:         source,
		RuleID:         conv.ToPGText(ruleID),
		Match:          conv.ToPGText(match),
		StartPos:       pgtype.Int4{Int32: int32(start), Valid: true}, // #nosec G115 -- test offsets are tiny.
		EndPos:         pgtype.Int4{Int32: int32(end), Valid: true},   // #nosec G115 -- test offsets are tiny.
		Spans:          []byte(spans),
	})
	require.NoError(t, err)
}

// seedRecallUserAccount inserts a provider account row; accountType is the
// raw column value, so an invalid pgtype.Text seeds an unclassified (NULL
// account_type) account.
func seedRecallUserAccount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, accountType pgtype.Text) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(conn).SeedUserAccountFixture(ctx, testrepo.SeedUserAccountFixtureParams{
		OrganizationID:      organizationID,
		ExternalAccountUuid: "acct-" + uuid.NewString(),
		AccountType:         accountType,
	})
	require.NoError(t, err)
	return id
}

func countRecallLinks(t *testing.T, ctx context.Context, conn *pgxpool.Pool, parentChatID uuid.UUID) int64 {
	t.Helper()
	count, err := testrepo.New(conn).CountChatSessionLinksByKindFixture(ctx, testrepo.CountChatSessionLinksByKindFixtureParams{
		ParentChatID: parentChatID,
		Kind:         "recall",
	})
	require.NoError(t, err)
	return count
}

// Case 1: the list serves exactly the principal's own non-deleted sessions on
// non-personal accounts — a foreign owner, an unattributed (empty user_id)
// chat, a personal-account chat, and an unclassified-account (NULL
// account_type) chat are all excluded.
func TestSessionRecallListReturnsOnlyOwnedSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_list")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc := newRecallService(t, conn, &enabled)

	ownedSession := "ses_" + uuid.NewString()
	ownedChatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, ownedSession, "fix flaky auth test", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	teamAccountID := seedRecallUserAccount(t, ctx, conn, principal.OrganizationID, conv.ToPGText("team"))
	personalAccountID := seedRecallUserAccount(t, ctx, conn, principal.OrganizationID, conv.ToPGText("personal"))
	unclassifiedAccountID := seedRecallUserAccount(t, ctx, conn, principal.OrganizationID, conv.ToPGTextEmpty(""))

	teamSession := uuid.NewString()
	teamChatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, teamSession, "team account session", uuid.NullUUID{UUID: teamAccountID, Valid: true})

	// None of these may appear in the listing. The unclassified (NULL
	// account_type) account is excluded deliberately: it might be personal,
	// and personal attribution is not transcript-grade.
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, "user_"+uuid.NewString(), "ses_"+uuid.NewString(), "not yours", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, "", "ses_"+uuid.NewString(), "unattributed", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, "ses_"+uuid.NewString(), "personal account session", uuid.NullUUID{UUID: personalAccountID, Valid: true})
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, "ses_"+uuid.NewString(), "unclassified account session", uuid.NullUUID{UUID: unclassifiedAccountID, Valid: true})

	// A deleted chat the caller owns: the c.deleted IS FALSE predicate must
	// drop it from the list and refuse a direct continue.
	deletedSession := "ses_" + uuid.NewString()
	deletedChatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, deletedSession, "deleted own session", uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	require.NoError(t, testrepo.New(conn).ForceSoftDeleteChat(ctx, deletedChatID))

	out, err := svc.ListMySessions(ctx, principal, ListMySessionsInput{Limit: 0})
	require.NoError(t, err)
	require.Len(t, out.Sessions, 2)

	byChatID := map[string]RecallableSession{}
	for _, session := range out.Sessions {
		byChatID[session.ChatID] = session
	}
	require.Contains(t, byChatID, ownedChatID.String())
	require.Contains(t, byChatID, teamChatID.String())
	require.Equal(t, ownedSession, byChatID[ownedChatID.String()].SessionID, "the harness-native session id is served, not the chat uuid")
	require.Equal(t, "fix flaky auth test", byChatID[ownedChatID.String()].Title)
	require.Equal(t, project.Slug, byChatID[ownedChatID.String()].ProjectSlug)
	require.NotContains(t, byChatID, deletedChatID.String())

	_, err = svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: deletedSession})
	require.ErrorIs(t, err, errSessionNotFound, "a deleted own session refuses exactly like a foreign one")
}

// Cases 2, 3, and 4: the digest carries the lineage byline and the
// findings-masked content but never the raw match; the lineage edge and the
// content-free audit record commit together; and a repeat recall records a
// second distinct NULL-child edge.
func TestSessionRecallContinueMasksAndRecords(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_continue")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc := newRecallService(t, conn, &enabled)

	sessionID := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "fix flaky auth test", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	// AWS's documented example key id — the one fake the secret scanners
	// (and GitHub push protection) recognize as fake; a novel AKIA string
	// blocks the push.
	const secret = "AKIAIOSFODNN7EXAMPLE"
	userContent := "Fix the login flow. My token is " + secret + " — rotate it."
	base := time.Now().UTC().Add(-time.Hour)
	userMessageID := seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: userContent, at: base, generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "assistant", content: "Working on it.", at: base.Add(time.Minute), generation: 0,
		toolCalls: `[{"id":"call_1","type":"function","function":{"name":"Edit","arguments":"{\"file_path\":\"/repo/auth/login.go\"}"}}]`,
		source:    "claude-code", analyzed: true, contentAssetURL: "",
	})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "tool", content: "edit applied", at: base.Add(2 * time.Minute), generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "assistant", content: "Done. The login flow now refreshes tokens.", at: base.Add(3 * time.Minute), generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: "also check the signup page", at: base.Add(4 * time.Minute), generation: 0, toolCalls: "", source: "claude-code", analyzed: false, contentAssetURL: "",
	})

	start := strings.Index(userContent, secret)
	seedRecallFinding(t, ctx, conn, project.ID, principal.OrganizationID, userMessageID, "gitleaks", "secret.aws_access_token", secret, start, start+len(secret))

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionChatSessionRecall)
	require.NoError(t, err)

	out, err := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)

	// Case 2: byline lineage markers, masked span, no raw match, and nothing
	// derived from tool inputs — the files-touched section is omitted under
	// redaction and unanalyzed prose is withheld.
	require.Contains(t, out.Digest, "source session "+sessionID)
	require.Contains(t, out.Digest, "gram chat "+chatID.String())
	masked := maskdisplay.Display("gitleaks", "secret.aws_access_token", secret)
	require.Contains(t, out.Digest, masked, "the digest shows exactly the dashboard's partial-mask form")
	require.NotContains(t, out.Digest, secret)
	require.Contains(t, out.Digest, "- → Edit\n", "tool names are retained without input summaries")
	require.NotContains(t, out.Digest, "/repo/auth/login.go", "paths come from tool inputs, which redaction must not read")
	require.Contains(t, out.Digest, unanalyzedContentPlaceholder, "unanalyzed prose is withheld, not served")
	require.Equal(t, sessionID, out.SourceSessionID)
	require.Equal(t, chatID.String(), out.ChatID)
	require.Contains(t, out.NotCarriedOver, "tool inputs and outputs (redacted; tool names retained)")
	require.Contains(t, out.NotCarriedOver, "files touched (derived from tool inputs, which are redacted)")
	require.Contains(t, out.Notes, "1 messages are awaiting risk analysis; their content was withheld")

	// Case 3: one NULL-child recall edge and one content-free audit record,
	// committed together.
	edge, err := testrepo.New(conn).GetChatSessionLinkByParentFixture(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, "recall", edge.Kind)
	require.False(t, edge.ChildChatID.Valid, "a v1 recall edge always has a NULL child")
	require.Equal(t, sessionID, edge.ParentSessionID)
	require.Equal(t, "platform-mcp", edge.TargetHarness)
	require.Equal(t, principal.OrganizationID, edge.OrganizationID)
	require.Equal(t, project.ID, edge.ProjectID)

	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionChatSessionRecall)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	record, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionChatSessionRecall)
	require.NoError(t, err)
	require.Equal(t, chatID.String(), record.SubjectID)
	require.Equal(t, "chat_session", record.SubjectType)
	require.Equal(t, project.ID, record.ProjectID.UUID)
	require.Equal(t, principal.UserID, record.ActorID)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, true, metadata["redact_tool_payloads"])
	require.EqualValues(t, 1, metadata["findings_masked"])
	require.EqualValues(t, 1, metadata["unanalyzed_messages"])
	require.EqualValues(t, len(out.Digest), metadata["digest_bytes"])
	require.EqualValues(t, 6, metadata["turns_included"])
	require.EqualValues(t, 0, metadata["turns_dropped"])
	require.Equal(t, sessionID, metadata["source_session_id"])
	require.NotContains(t, string(record.Metadata), secret, "the audit record is content-free")

	// Case 4: each recall is a distinct event — a second continue inserts a
	// second NULL-child edge rather than deduplicating.
	_, err = svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)
	require.EqualValues(t, 2, countRecallLinks(t, ctx, conn, chatID))
}

// Case 4b: a finding whose offsets cannot be applied exactly withholds the
// whole message rather than serving prose known to carry an unmasked finding,
// and the fidelity notes say so.
func TestSessionRecallContinueWithholdsWhenFindingCannotBeMasked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_withheld")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc := newRecallService(t, conn, &enabled)

	sessionID := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "stale offsets session", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	base := time.Now().UTC().Add(-time.Hour)
	content := "prose with a finding the offsets no longer locate"
	messageID := seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: content, at: base, generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	// Offsets beyond the content: the mask cannot be applied exactly.
	seedRecallFinding(t, ctx, conn, project.ID, principal.OrganizationID, messageID, "gitleaks", "secret.generic", "stale", 1000, 1006)

	out, err := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)

	require.NotContains(t, out.Digest, content, "prose with an un-applied finding must never be served")
	require.Contains(t, out.Digest, withheldContentPlaceholder)
	require.Contains(t, out.Notes, "1 messages had findings that could not be masked precisely; their content was withheld")
}

// Case 5: with the feature disabled both tools refuse, and a refused continue
// writes neither the edge nor the audit record.
func TestSessionRecallRefusesWhenFeatureDisabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_disabled")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := false
	svc := newRecallService(t, conn, &enabled)

	sessionID := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "gated session", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionChatSessionRecall)
	require.NoError(t, err)

	_, err = svc.ListMySessions(ctx, principal, ListMySessionsInput{Limit: 0})
	require.ErrorIs(t, err, errSessionRecallDisabled)
	_, err = svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.ErrorIs(t, err, errSessionRecallDisabled)

	require.EqualValues(t, 0, countRecallLinks(t, ctx, conn, chatID))
	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionChatSessionRecall)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// Case 6: another user's session, a personal-account session, an
// unclassified-account session, and an unknown id all refuse with the same
// not-found sentinel — no existence leak.
func TestSessionRecallContinueHidesForeignAndUnknownSessions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_not_found")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc := newRecallService(t, conn, &enabled)

	foreignSession := "ses_" + uuid.NewString()
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, "user_"+uuid.NewString(), foreignSession, "not yours", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	personalAccountID := seedRecallUserAccount(t, ctx, conn, principal.OrganizationID, conv.ToPGText("personal"))
	personalSession := "ses_" + uuid.NewString()
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, personalSession, "personal account session", uuid.NullUUID{UUID: personalAccountID, Valid: true})

	unclassifiedAccountID := seedRecallUserAccount(t, ctx, conn, principal.OrganizationID, conv.ToPGTextEmpty(""))
	unclassifiedSession := "ses_" + uuid.NewString()
	seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, unclassifiedSession, "unclassified account session", uuid.NullUUID{UUID: unclassifiedAccountID, Valid: true})

	_, foreignErr := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: foreignSession})
	require.ErrorIs(t, foreignErr, errSessionNotFound)
	_, personalErr := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: personalSession})
	require.ErrorIs(t, personalErr, errSessionNotFound)
	_, unclassifiedErr := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: unclassifiedSession})
	require.ErrorIs(t, unclassifiedErr, errSessionNotFound, "an unclassified (NULL account_type) account gets the personal-account fail-closed treatment")
	_, unknownErr := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: uuid.NewString()})
	require.ErrorIs(t, unknownErr, errSessionNotFound)
	require.Equal(t, foreignErr.Error(), unknownErr.Error(), "an existing foreign session and an unknown id are indistinguishable")
}

// Case 7: only the latest generation renders — superseded rows from before a
// compaction or edit never reach the digest.
// A session longer than the row cap reads only its newest rows: the evicted
// head never reaches the digest and the fidelity notes say the view is
// partial.
func TestSessionRecallContinueCapsRowRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_rowcap")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc := newRecallService(t, conn, &enabled)

	sessionID := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "very long session", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	base := time.Now().UTC().Add(-24 * time.Hour)
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: "EVICTED-HEAD original ask", at: base, generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	for i := range recallMessageRowCap {
		seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
			role: "user", content: "filler turn " + strconv.Itoa(i), at: base.Add(time.Duration(i+1) * time.Minute), generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
		})
	}

	out, err := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)
	require.NotContains(t, out.Digest, "EVICTED-HEAD", "rows beyond the cap are never read")
	require.Contains(t, out.Notes, fmt.Sprintf("only the most recent %d messages were considered", recallMessageRowCap))
}

func TestSessionRecallContinueServesLatestGenerationOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_session_recall_generation")
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	enabled := true
	svc := newRecallService(t, conn, &enabled)

	sessionID := "ses_" + uuid.NewString()
	chatID := seedRecallChat(t, ctx, conn, project.ID, principal.OrganizationID, principal.UserID, sessionID, "compacted session", uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	base := time.Now().UTC().Add(-time.Hour)
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: "SUPERSEDED-GENERATION-ZERO ask", at: base, generation: 0, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "user", content: "current generation ask", at: base.Add(time.Minute), generation: 1, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})
	seedRecallMessage(t, ctx, conn, chatID, project.ID, recallMessageSeed{
		role: "assistant", content: "current generation answer", at: base.Add(2 * time.Minute), generation: 1, toolCalls: "", source: "claude-code", analyzed: true, contentAssetURL: "",
	})

	out, err := svc.ContinueSession(ctx, principal, ContinueSessionInput{SessionID: sessionID})
	require.NoError(t, err)
	require.Contains(t, out.Digest, "current generation ask")
	require.Contains(t, out.Digest, "current generation answer")
	require.NotContains(t, out.Digest, "SUPERSEDED-GENERATION-ZERO")
}
