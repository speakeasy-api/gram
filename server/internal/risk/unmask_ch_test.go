package risk_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// unmaskFinding is one ClickHouse risk_findings fixture row for the reveal
// tests, carrying the reveal metadata columns (surface/field/path/tool_call_id)
// that chrepo.InsertRiskFindings does not yet write (the live write path for
// them is a parallel change). Inserted via a direct INSERT in insertUnmaskFinding.
type unmaskFinding struct {
	id               uuid.UUID
	orgID            string
	projectID        string
	chatMessageID    string
	contentPartID    string
	chatID           string
	source           string
	ruleID           string
	startPos         int32
	endPos           int32
	matchLen         uint32
	matchRedacted    string
	surface          string
	field            string
	path             string
	toolCallID       string
	deadLetterReason string
	excludedAt       *time.Time
	falsePositiveAt  *time.Time
}

// insertUnmaskFinding writes the fixture straight into risk_findings. Raw SQL
// on purpose: ClickHouse fixtures are exempt from the no-raw-SQL test rule,
// and the sanctioned insert helper cannot carry the reveal columns yet.
func insertUnmaskFinding(t *testing.T, ti *testInstance, f unmaskFinding) uuid.UUID {
	t.Helper()

	if f.id == uuid.Nil {
		f.id = uuid.Must(uuid.NewV7())
	}
	if f.source == "" {
		f.source = "gitleaks"
	}
	if f.ruleID == "" {
		f.ruleID = "secret.test_rule"
	}
	// Relative timestamps: the table's 90-day created_at TTL would silently
	// expire hardcoded dates once the calendar catches up.
	createdAt := time.Now().UTC().AddDate(0, 0, -2)

	nullableTime := func(v *time.Time) any {
		if v == nil {
			return nil
		}
		return *v
	}

	require.NoError(t, ti.chConn.Exec(t.Context(), `
		INSERT INTO risk_findings (
			id, created_at, organization_id, project_id,
			chat_message_id, content_part_id, chat_id,
			risk_policy_id, risk_policy_version, rule_id, source, category, confidence, tags,
			start_pos, end_pos, dead_letter_reason,
			match_len, match_redacted,
			excluded_at, false_positive_at, message_created_at,
			surface, field, path, tool_call_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		f.id, createdAt, f.orgID, f.projectID,
		f.chatMessageID, f.contentPartID, f.chatID,
		uuid.NewString(), int64(1), f.ruleID, f.source, "secrets", 1.0, []string{},
		f.startPos, f.endPos, f.deadLetterReason,
		f.matchLen, f.matchRedacted,
		nullableTime(f.excludedAt), nullableTime(f.falsePositiveAt), createdAt,
		f.surface, f.field, f.path, f.toolCallID,
	))
	return f.id
}

func TestUnmaskRiskResult_ClickHouseContentSurface(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "AKIAIOSFODNN7EXAMPLE"
	content := "please rotate " + secret + " before the audit"
	start := strings.Index(content, secret)

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		matchRedacted: "AKIA**************LE",
		surface:       "content",
	})

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, rowID.String(), res.ID)
	require.Equal(t, secret, res.Match)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "clickhouse-served reveal records the same audit event as the postgres path")

	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionRiskResultUnmask)
	require.NoError(t, err)
	require.Equal(t, "risk_result", rec.SubjectType)
	require.Equal(t, chatID.String(), rec.SubjectSlug, "audit records which chat the revealed value came from")
}

// TestUnmaskRiskResult_ClickHouseForbiddenWithoutChatRead mirrors the Postgres
// Forbidden test: org:admin alone (able to browse the redacted listing) must
// not unlock a ClickHouse-served reveal.
func TestUnmaskRiskResult_ClickHouseForbiddenWithoutChatRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "SECRET_FORBIDDEN_VALUE"
	chatID, msgID := seedChatMessage(t, ti, projectID, orgID)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      0,
		endPos:        int32(len(secret)),
		matchLen:      uint32(len(secret)),
		surface:       "content",
	})

	_, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestUnmaskRiskResult_ClickHouseScanSurface: sync-era gitleaks offsets index
// the composed message+tool-args scan surface; the reveal recomposes it byte
// for byte and serves the slice landing inside the arguments region.
func TestUnmaskRiskResult_ClickHouseScanSurface(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "tok_scan_surface_9876543210"
	content := "Deploying with stored credentials"
	args := `{"api_key":"` + secret + `","region":"eu-west-1"}`
	toolCalls := `[{"id":"call_1","function":{"name":"deploy_service","arguments":` + strconv.Quote(args) + `}}]`
	// Mirror batchMessage.scanSurface: content, newline, first non-empty args.
	surface := content + "\n" + args
	start := strings.Index(surface, secret)
	require.Greater(t, start, len(content), "fixture secret must sit in the tool-args region")

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "assistant",
		Content:   content,
		ToolCalls: []byte(toolCalls),
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		surface:       "scan_surface",
	})

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, secret, res.Match)
}

// TestUnmaskRiskResult_ClickHouseToolArgsAndJSONPath covers the span-recorded
// surfaces: tool_args offsets index one call's raw arguments (narrowed by the
// recorded call id), json_path offsets index the gjson-decoded value the
// scanner extracted.
func TestUnmaskRiskResult_ClickHouseToolArgsAndJSONPath(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "sk_live_json_path_12345"
	args := `{"query":"select 1","credential":"` + secret + `"}`
	toolCalls := `[{"id":"call_77","function":{"name":"run_query","arguments":` + strconv.Quote(args) + `}}]`

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "assistant",
		Content:   "running a query",
		ToolCalls: []byte(toolCalls),
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	base := unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		matchLen:      uint32(len(secret)),
	}

	// tool_args: offsets index the call's raw arguments string, narrowed by
	// the recorded call id.
	argsStart := strings.Index(args, secret)
	toolArgsRow := base
	toolArgsRow.surface = "tool_args"
	toolArgsRow.field = "tool.args"
	toolArgsRow.toolCallID = "call_77"
	toolArgsRow.startPos = int32(argsStart)
	toolArgsRow.endPos = int32(argsStart + len(secret))
	toolArgsRowID := insertUnmaskFinding(t, ti, toolArgsRow)

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: toolArgsRowID.String()})
	require.NoError(t, err)
	require.Equal(t, secret, res.Match)

	// json_path: offsets index the decoded extracted string ([0, len)).
	jsonPathRow := base
	jsonPathRow.surface = "json_path"
	jsonPathRow.field = "tool.args"
	jsonPathRow.path = "credential"
	jsonPathRow.startPos = 0
	jsonPathRow.endPos = int32(len(secret))
	jsonPathRowID := insertUnmaskFinding(t, ti, jsonPathRow)

	res, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: jsonPathRowID.String()})
	require.NoError(t, err)
	require.Equal(t, secret, res.Match)
}

// TestUnmaskRiskResult_ClickHouseToolArgsMultiCallFallback: custom-rule rows
// carry no recorded tool call id (the write path decides by call name only,
// permanently), so the reveal tries every call's arguments; the bounds check
// plus the match-length gate pick the call whose arguments actually cover the
// offsets.
func TestUnmaskRiskResult_ClickHouseToolArgsMultiCallFallback(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "tok_multi_call_arg_00042"
	shortArgs := `{"a":1}`
	longArgs := `{"padding":"xxxxxxxxxxxxxxxxxxxxxxxx","token":"` + secret + `"}`
	toolCalls := `[` +
		`{"id":"call_a","function":{"name":"first_tool","arguments":` + strconv.Quote(shortArgs) + `}},` +
		`{"id":"call_b","function":{"name":"second_tool","arguments":` + strconv.Quote(longArgs) + `}}]`
	start := strings.Index(longArgs, secret)
	require.Greater(t, start, len(shortArgs), "offsets must be out of bounds for the first call's arguments")

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "assistant",
		Content:   "",
		ToolCalls: []byte(toolCalls),
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		source:        "custom_rules",
		ruleID:        "custom.token_rule",
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		surface:       "tool_args",
		field:         "tool.args",
		toolCallID:    "",
	})

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.NoError(t, err)
	require.Equal(t, secret, res.Match)
}

// TestUnmaskRiskResult_ClickHouseDerivedSources covers the derived surface:
// account_identity re-resolves the chat's AI-account email, destructive
// findings re-derive tool call names (the match-length gate picks the bare
// MCP function name destructive_tool stores), and shadow_mcp serves its
// verbatim match_redacted carve-out.
func TestUnmaskRiskResult_ClickHouseDerivedSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	email := "synthetic-account@test.example"
	toolName := "mcp__db__drop_table"
	bareToolName := "drop_table"
	serverURL := "https://shadow.mcp.test.example/api"

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	accountID, err := repo.CreateUserAccountForTest(ctx, riskrepo.CreateUserAccountForTestParams{
		OrganizationID:      orgID,
		ExternalAccountUuid: uuid.NewString(),
		Email:               pgtype.Text{String: email, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, repo.LinkChatUserAccountForTest(ctx, riskrepo.LinkChatUserAccountForTestParams{
		UserAccountID: uuid.NullUUID{UUID: accountID, Valid: true},
		ChatID:        chatID,
	}))
	msgID, err := repo.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "assistant",
		Content:   "",
		ToolCalls: []byte(`[{"id":"call_3","function":{"name":"` + toolName + `","arguments":"{}"}}]`),
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	base := unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		surface:       "derived",
	}

	accountRow := base
	accountRow.source = "account_identity"
	accountRow.ruleID = "account.personal_account"
	accountRow.matchLen = uint32(len(email))
	accountRowID := insertUnmaskFinding(t, ti, accountRow)

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: accountRowID.String()})
	require.NoError(t, err)
	require.Equal(t, email, res.Match)

	// destructive_tool records the RESOLVED bare function name as its match,
	// not the full recorded tool name; the reveal offers both forms and the
	// bare one is the only length-consistent candidate here.
	destructiveRow := base
	destructiveRow.source = "destructive_tool"
	destructiveRow.ruleID = "guard.destructive_tool"
	destructiveRow.matchLen = uint32(len(bareToolName))
	destructiveRowID := insertUnmaskFinding(t, ti, destructiveRow)

	res, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: destructiveRowID.String()})
	require.NoError(t, err)
	require.Equal(t, bareToolName, res.Match)

	// shadow_mcp: match_redacted stores the server identifier verbatim (the
	// documented carve-out), so the stored display string IS the match.
	shadowRow := base
	shadowRow.source = "shadow_mcp"
	shadowRow.ruleID = "guard.shadow_mcp"
	shadowRow.matchLen = uint32(len(serverURL))
	shadowRow.matchRedacted = serverURL
	shadowRowID := insertUnmaskFinding(t, ti, shadowRow)

	res, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: shadowRowID.String()})
	require.NoError(t, err)
	require.Equal(t, serverURL, res.Match)
}

// TestUnmaskRiskResult_ClickHouseHiddenRowsNotFound: the point read applies
// the same gates as every other risk_findings read — excluded, dismissed,
// dead-letter and foreign-tenant rows all read as absent.
func TestUnmaskRiskResult_ClickHouseHiddenRowsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "HIDDEN_ROW_SECRET_VAL"
	chatID, msgID := seedChatMessage(t, ti, projectID, orgID)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	base := unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      0,
		endPos:        int32(len(secret)),
		matchLen:      uint32(len(secret)),
		surface:       "content",
	}

	when := time.Now().UTC().AddDate(0, 0, -1)

	excluded := base
	excluded.excludedAt = &when
	excludedID := insertUnmaskFinding(t, ti, excluded)

	falsePositive := base
	falsePositive.falsePositiveAt = &when
	falsePositiveID := insertUnmaskFinding(t, ti, falsePositive)

	deadLetter := base
	deadLetter.deadLetterReason = "could-not-analyze"
	deadLetterID := insertUnmaskFinding(t, ti, deadLetter)

	foreign := base
	foreign.projectID = uuid.NewString()
	foreignID := insertUnmaskFinding(t, ti, foreign)

	for _, id := range []uuid.UUID{excludedID, falsePositiveID, deadLetterID, foreignID} {
		_, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: id.String()})
		require.Error(t, err, "row %s must read as absent", id)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	}
}

// TestUnmaskRiskResult_ClickHouseLengthMismatchRefused: the match-length gate
// is the reveal's integrity check — a reconstruction whose byte length
// disagrees with the recorded match_len (edited content shifting offsets, or
// offsets past the end of the stored text) is refused, not served.
func TestUnmaskRiskResult_ClickHouseLengthMismatchRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "SECRET_LENGTH_GATE_VAL"
	content := "prefix " + secret + " suffix"
	start := strings.Index(content, secret)

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	base := unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		surface:       "content",
	}

	// Recorded length disagrees with the slice the offsets produce.
	lengthMismatch := base
	lengthMismatch.startPos = int32(start)
	lengthMismatch.endPos = int32(start + len(secret))
	lengthMismatch.matchLen = uint32(len(secret) + 5)
	lengthMismatchID := insertUnmaskFinding(t, ti, lengthMismatch)

	// Offsets run past the end of the stored content (message was edited or
	// truncated since the scan) so no candidate can be sliced at all.
	outOfBounds := base
	outOfBounds.startPos = int32(len(content) + 10)
	outOfBounds.endPos = int32(len(content) + 10 + len(secret))
	outOfBounds.matchLen = uint32(len(secret))
	outOfBoundsID := insertUnmaskFinding(t, ti, outOfBounds)

	for _, id := range []uuid.UUID{lengthMismatchID, outOfBoundsID} {
		_, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: id.String()})
		require.Error(t, err)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeNotFound, oopsErr.Code)
		require.Contains(t, oopsErr.Error(), "no longer available")
	}
}

// TestUnmaskRiskResult_ClickHouseEmptySurfaceRefused: the table is truncated
// and fully re-backfilled with stamped surfaces before this path serves
// traffic, so an empty surface is treated like an unknown one — refused
// rather than guessed at.
func TestUnmaskRiskResult_ClickHouseEmptySurfaceRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "EMPTY_SURFACE_SECRET"
	content := "holds " + secret + " here"
	start := strings.Index(content, secret)

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		surface:       "",
	})

	_, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

// TestUnmaskRiskResult_ClickHouseNoMatchContentRefused: llm_judge-style rows
// (no match, no redaction) refuse cleanly instead of panicking or returning
// an empty string.
func TestUnmaskRiskResult_ClickHouseNoMatchContentRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	chatID, msgID := seedChatMessage(t, ti, projectID, orgID)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		source:        "llm_judge",
		ruleID:        "llm_judge",
		matchLen:      0,
		matchRedacted: "",
		surface:       "none",
	})

	_, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "no revealable match content")
}

// TestUnmaskRiskResult_FlagOffIgnoresClickHouse: with the listing flag off the
// unmask path is byte-for-byte the Postgres lookup — a row that exists only in
// ClickHouse must NOT resolve.
func TestUnmaskRiskResult_FlagOffIgnoresClickHouse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "FLAG_OFF_SECRET_VALUE"
	chatID, msgID := seedChatMessage(t, ti, projectID, orgID)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      0,
		endPos:        int32(len(secret)),
		matchLen:      uint32(len(secret)),
		surface:       "content",
	})

	_, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code, "flag off must resolve against postgres only")
}

// TestUnmaskRiskResult_ClickHouseDismissedAfterLiveRowNotFound pins the
// state-gate ordering in the point-read: a finding whose newest event marks it
// excluded (or a false positive) must not be revealable just because its
// original, still-clean event is also in the append-only table.
func TestUnmaskRiskResult_ClickHouseDismissedAfterLiveRowNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "AKIAIOSFODNN7EXAMPLE"
	content := "rotate " + secret + " today"
	start := strings.Index(content, secret)

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	base := unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		matchRedacted: "AKIA**************LE",
		surface:       "content",
	}
	rowID := insertUnmaskFinding(t, ti, base)

	// The dismissal event: same id, written later, carrying false_positive_at.
	dismissedAt := time.Now().UTC()
	base.id = rowID
	base.falsePositiveAt = &dismissedAt
	insertUnmaskFinding(t, ti, base)

	_, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestUnmaskRiskResult_ClickHouseForeignChatAnchorNotFound covers the
// consistency assertion between the chat the caller was authorized against
// (the ingest-stamped chat id) and the chat the reconstructed content actually
// comes from: a divergence must refuse rather than serve another chat's text.
func TestUnmaskRiskResult_ClickHouseForeignChatAnchorNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	secret := "AKIAIOSFODNN7EXAMPLE"
	content := "rotate " + secret + " today"
	start := strings.Index(content, secret)

	repo := riskrepo.New(ti.conn)
	grantedChatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	otherChatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	// The anchored message lives in the OTHER chat, while the finding row
	// claims the chat the caller holds chat:read for.
	msgID, err := repo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
		ChatID: otherChatID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, Content: content,
		UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, grantedChatID.String()),
	)

	rowID := insertUnmaskFinding(t, ti, unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        grantedChatID.String(),
		startPos:      int32(start),
		endPos:        int32(start + len(secret)),
		matchLen:      uint32(len(secret)),
		matchRedacted: "AKIA**************LE",
		surface:       "content",
	})

	_, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: rowID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestUnmaskRiskResult_ClickHouseCustomDerivedFieldScoped pins that a custom
// rule's derived reveal is scoped to the field the finding recorded: with a
// tool name whose server and function halves are the same length, the length
// gate alone cannot tell them apart, so the field must.
func TestUnmaskRiskResult_ClickHouseCustomDerivedFieldScoped(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ti.flags.SetFlag(feature.FlagRiskListFromClickHouse, authCtx.ActiveOrganizationID, true)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	// Equal-length server and function halves: "payments" and "transfer" are
	// both 8 bytes, so match_len cannot discriminate.
	toolName := "mcp__payments__transfer"
	server := toolref.MCPServerOf(toolName)
	function := toolref.MCPFunctionOf(toolName)
	require.Len(t, server, len(function), "fixture needs equal-length halves to exercise the gate")

	repo := riskrepo.New(ti.conn)
	chatID, err := repo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: projectID, OrganizationID: orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	msgID, err := repo.CreateChatMessageWithToolCallsForTest(ctx, riskrepo.CreateChatMessageWithToolCallsForTestParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "assistant",
		Content:   "",
		ToolCalls: []byte(`[{"id":"call_1","function":{"name":"` + toolName + `","arguments":"{}"}}]`),
	})
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, orgID)},
		authz.NewGrant(authz.ScopeChatRead, chatID.String()),
	)

	base := unmaskFinding{
		orgID:         orgID,
		projectID:     projectID.String(),
		chatMessageID: msgID.String(),
		chatID:        chatID.String(),
		source:        "custom",
		ruleID:        "custom.tool_guard",
		matchLen:      uint32(len(server)),
		matchRedacted: "paym**ts",
		surface:       "derived",

		field: "tool.server"}
	serverRow := insertUnmaskFinding(t, ti, base)

	base.id = uuid.Nil
	base.field = "tool.function"
	functionRow := insertUnmaskFinding(t, ti, base)

	res, err := ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: serverRow.String()})
	require.NoError(t, err)
	require.Equal(t, server, res.Match, "a tool.server finding reveals the server half")

	res, err = ti.service.UnmaskRiskResult(ctx, &gen.UnmaskRiskResultPayload{ID: functionRow.String()})
	require.NoError(t, err)
	require.Equal(t, function, res.Match, "a tool.function finding reveals the function half")
}
