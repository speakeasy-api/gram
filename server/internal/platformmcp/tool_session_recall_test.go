package platformmcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/maskdisplay"
)

type fakeSessionRecaller struct {
	listOutput     ListMySessionsOutput
	listErr        error
	continueOutput ContinueSessionOutput
	continueErr    error

	lastPrincipal Principal
	lastContinue  ContinueSessionInput
}

func (f *fakeSessionRecaller) ListMySessions(_ context.Context, principal Principal, _ ListMySessionsInput) (ListMySessionsOutput, error) {
	f.lastPrincipal = principal
	return f.listOutput, f.listErr
}

func (f *fakeSessionRecaller) ContinueSession(_ context.Context, principal Principal, input ContinueSessionInput) (ContinueSessionOutput, error) {
	f.lastPrincipal = principal
	f.lastContinue = input
	return f.continueOutput, f.continueErr
}

func sessionRecallRegistrar(svc SessionRecaller) *Registrar {
	registrar := newRegistrar(newTestMCPServer())
	registerSessionRecallTools(registrar, svc)
	return registrar
}

func TestListMySessionsToolServesSessions(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionRecaller{listOutput: ListMySessionsOutput{Sessions: []RecallableSession{{
		SessionID:   "ses_abc",
		ChatID:      uuid.NewString(),
		Title:       "fix flaky auth test",
		Summary:     "",
		ProjectName: "Default",
		ProjectSlug: "default",
		Cwd:         "/home/dev/code/api",
		LastActive:  "2026-08-20T10:00:00Z",
	}}}}
	registrar := sessionRecallRegistrar(fake)

	principal := registrationServicePrincipal()
	result, err := descriptorByName(t, registrar, "list_my_sessions").Invoke(
		ContextWithPrincipal(t.Context(), principal),
		json.RawMessage(`{}`),
	)

	require.NoError(t, err)
	output, ok := result.(ListMySessionsOutput)
	require.True(t, ok)
	require.Len(t, output.Sessions, 1)
	require.Equal(t, "ses_abc", output.Sessions[0].SessionID)
	require.Equal(t, principal, fake.lastPrincipal)
}

func TestContinueSessionToolServesDigest(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionRecaller{continueOutput: ContinueSessionOutput{
		Digest:          "# Session handoff — fix flaky auth test\n",
		SourceSessionID: "ses_abc",
		ChatID:          uuid.NewString(),
		NotCarriedOver:  []string{"assistant thinking (not captured)"},
		Notes:           nil,
	}}
	registrar := sessionRecallRegistrar(fake)

	result, err := descriptorByName(t, registrar, "continue_session").Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"session_id":"ses_abc"}`),
	)

	require.NoError(t, err)
	output, ok := result.(ContinueSessionOutput)
	require.True(t, ok)
	require.Equal(t, "ses_abc", output.SourceSessionID)
	require.Equal(t, "ses_abc", fake.lastContinue.SessionID)
}

func TestSessionRecallToolsRefuseWhenFeatureDisabled(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionRecaller{listErr: errSessionRecallDisabled, continueErr: errSessionRecallDisabled}
	registrar := sessionRecallRegistrar(fake)
	ctx := ContextWithPrincipal(t.Context(), registrationServicePrincipal())

	for name, arguments := range map[string]string{
		"list_my_sessions": `{}`,
		"continue_session": `{"session_id":"ses_abc"}`,
	} {
		_, err := descriptorByName(t, registrar, name).Invoke(ctx, json.RawMessage(arguments))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, "tool %q must refuse with a structured payload", name)
		require.JSONEq(t, `{"code":"feature_unavailable","feature":"session_portability","message":"Session recall is not enabled for this organization."}`, refusal.Payload)
	}
}

func TestContinueSessionToolRefusesUnknownSession(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionRecaller{continueErr: errSessionNotFound}
	registrar := sessionRecallRegistrar(fake)

	_, err := descriptorByName(t, registrar, "continue_session").Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"session_id":"ses_missing"}`),
	)

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.JSONEq(t, `{"code":"not_found","message":"This session was not found among your own captured sessions. Use list_my_sessions to see the sessions you can continue."}`, refusal.Payload)
}

func TestContinueSessionToolMapsBudgetErrors(t *testing.T) {
	t.Parallel()

	fake := &fakeSessionRecaller{continueErr: ErrOperationRateLimited}
	registrar := sessionRecallRegistrar(fake)

	_, err := descriptorByName(t, registrar, "continue_session").Invoke(
		ContextWithPrincipal(t.Context(), registrationServicePrincipal()),
		json.RawMessage(`{"session_id":"ses_abc"}`),
	)

	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"rate_limited"`)
}

func TestSessionRecallStubsRefuse(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(newTestMCPServer())
	registerUnavailableSessionRecallTools(registrar)
	ctx := ContextWithPrincipal(t.Context(), registrationServicePrincipal())

	for name, arguments := range map[string]string{
		"list_my_sessions": `{}`,
		"continue_session": `{"session_id":"ses_abc"}`,
	} {
		descriptor := descriptorByName(t, registrar, name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, "stub for %q must declare the live tool's audiences", name)
		_, err := descriptor.Invoke(ctx, json.RawMessage(arguments))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.JSONEq(t, `{"code":"feature_unavailable","feature":"session_recall","message":"This is not switched on for your organization yet."}`, refusal.Payload)
	}
}

func TestSessionRecallToolsRequirePrincipal(t *testing.T) {
	t.Parallel()

	registrar := sessionRecallRegistrar(&fakeSessionRecaller{})

	for name, arguments := range map[string]string{
		"list_my_sessions": `{}`,
		"continue_session": `{"session_id":"ses_abc"}`,
	} {
		_, err := descriptorByName(t, registrar, name).Invoke(t.Context(), json.RawMessage(arguments))
		require.ErrorIs(t, err, ErrUnauthorized, "tool %q must refuse a principal-less call", name)
	}
}

func recallMessageRow(id uuid.UUID, role, content string) platformrepo.ListOwnedChatTranscriptMessagesForRecallRow {
	return platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{
		ID:             id,
		Role:           role,
		Content:        content,
		CreatedAt:      pgtype.Timestamptz{Time: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC), InfinityModifier: pgtype.Finite, Valid: true},
		RiskAnalyzedAt: pgtype.Timestamptz{Time: time.Date(2026, 8, 20, 11, 0, 0, 0, time.UTC), InfinityModifier: pgtype.Finite, Valid: true},
	}
}

func TestMaskFindingSpansReplacesEverySpan(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "token AKIAIOSFODNN7EXAMPLE and email dev@acme.example end"
	tokenStart := strings.Index(content, "AKIA")
	emailStart := strings.Index(content, "dev@")
	spans := `[
		{"match":"AKIAIOSFODNN7EXAMPLE","field":"content","start_pos":` + itoa(tokenStart) + `,"end_pos":` + itoa(tokenStart+20) + `},
		{"match":"dev@acme.example","field":"","start_pos":` + itoa(emailStart) + `,"end_pos":` + itoa(emailStart+16) + `}
	]`

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.aws_access_token", Valid: true},
			Match:         pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: true},
			Spans:         []byte(spans),
		}},
	)

	require.Equal(t, 2, masks.neutralized)
	require.Equal(t, 0, masks.withheld)
	tokenMask := maskdisplay.Display("gitleaks", "secret.aws_access_token", "AKIAIOSFODNN7EXAMPLE")
	emailMask := maskdisplay.Display("gitleaks", "secret.aws_access_token", "dev@acme.example")
	require.Equal(t, "token "+tokenMask+" and email "+emailMask+" end", masks.content[messageID])
	require.NotContains(t, masks.content[messageID], "AKIAIOSFODNN7EXAMPLE")
}

func TestMaskFindingSpansWithholdsOnOverlapAndHostileOffsets(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"
	spans := `[
		{"match":"SECRET","field":"content","start_pos":4,"end_pos":10},
		{"match":"SECRETbb","field":"content","start_pos":4,"end_pos":12},
		{"match":"out of range","field":"content","start_pos":100,"end_pos":120},
		{"match":"negative","field":"content","start_pos":-3,"end_pos":2}
	]`

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.generic", Valid: true},
			Spans:         []byte(spans),
		}},
	)

	// Overlapping and out-of-range offsets cannot all be honored, so the
	// whole message is withheld rather than served with a finding unmasked.
	require.Equal(t, 1, masks.withheld)
	require.Equal(t, 4, masks.neutralized)
	require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
	require.NotContains(t, masks.content[messageID], "SECRET")
}

// Offsets that land in range but select different bytes than the finding's
// recorded match mean the content drifted since the scan — masking those
// bytes would leave the actual finding served elsewhere in the message, so
// the whole message is withheld.
func TestMaskFindingSpansWithholdsOnStaleOffsets(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaDRIFTYbbbb SECRET moved here"
	spans := `[{"match":"SECRET","field":"content","start_pos":4,"end_pos":10}]`

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.generic", Valid: true},
			Spans:         []byte(spans),
		}},
	)

	require.Equal(t, 1, masks.withheld)
	require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
	require.NotContains(t, masks.content[messageID], "SECRET")
}

// A content span with no recorded match text anywhere (neither in the span
// entry nor the finding's primary match) cannot be verified against the
// content, so it withholds rather than masking on trust.
func TestMaskFindingSpansWithholdsOnMissingMatchText(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"
	spans := `[{"field":"content","start_pos":4,"end_pos":10}]`

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.generic", Valid: true},
			Spans:         []byte(spans),
		}},
	)

	require.Equal(t, 1, masks.withheld)
	require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
}

func TestMaskFindingSpansCollapsesIdenticalDuplicateSpans(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"
	rows := []platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)}
	span := `[{"match":"SECRET","field":"content","start_pos":4,"end_pos":10}]`
	finding := func(rule string) platformrepo.ListRiskFindingSpansForRecallRow {
		return platformrepo.ListRiskFindingSpansForRecallRow{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: rule, Valid: true},
			Spans:         []byte(span),
		}
	}

	// Two policies flagging the same region mask it once — never a withhold.
	masks := maskFindingSpans(rows, []platformrepo.ListRiskFindingSpansForRecallRow{finding("secret.generic"), finding("secret.other")})

	require.Equal(t, 0, masks.withheld)
	require.Equal(t, 2, masks.neutralized)
	require.NotContains(t, masks.content[messageID], "SECRET")
	require.True(t, strings.HasPrefix(masks.content[messageID], "aaaa"))
	require.True(t, strings.HasSuffix(masks.content[messageID], "bbbb"))
}

func TestMaskFindingSpansWithholdsOnUnappliableSpanData(t *testing.T) {
	t.Parallel()

	content := "prose that carries a finding somewhere"
	cases := map[string]platformrepo.ListRiskFindingSpansForRecallRow{
		// A .get(...) span's offsets index the extracted sub-value, not the
		// outer content.
		"nested path": {Spans: []byte(`[{"match":"SECRET","field":"content","path":"config.token","start_pos":0,"end_pos":6}]`)},
		// An unrecognized field could anchor anywhere.
		"unknown field": {Spans: []byte(`[{"match":"SECRET","field":"mystery","start_pos":0,"end_pos":6}]`)},
		// Span data that exists but cannot be decoded.
		"undecodable spans": {Spans: []byte(`{"not":"an array"}`)},
		// A finding with no span data at all cannot be masked.
		"no span data": {Spans: nil},
	}
	for name, finding := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			messageID := uuid.New()
			finding.ChatMessageID = uuid.NullUUID{UUID: messageID, Valid: true}
			finding.Source = "gitleaks"
			finding.RuleID = pgtype.Text{String: "secret.generic", Valid: true}

			masks := maskFindingSpans(
				[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
				[]platformrepo.ListRiskFindingSpansForRecallRow{finding},
			)

			require.Equal(t, 1, masks.withheld)
			require.Equal(t, 1, masks.neutralized)
			require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
		})
	}
}

func TestMaskFindingSpansKeepsMultibyteContentIntact(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	prefix := "héllo wörld — "
	secret := "SECRETVALUE123"
	content := prefix + secret + " — döne"
	start := strings.Index(content, secret)

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.generic", Valid: true},
			Spans:         []byte(`[{"match":"` + secret + `","field":"content","start_pos":` + itoa(start) + `,"end_pos":` + itoa(start+len(secret)) + `}]`),
		}},
	)

	require.Equal(t, 1, masks.neutralized)
	require.Equal(t, 0, masks.withheld)
	display := maskdisplay.Display("gitleaks", "secret.generic", secret)
	require.Equal(t, prefix+display+" — döne", masks.content[messageID])
}

func TestMaskFindingSpansFallsBackToPrimarySpan(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "the secret is HUSH42 today"
	start := strings.Index(content, "HUSH42")

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.generic", Valid: true},
			Match:         pgtype.Text{String: "HUSH42", Valid: true},
			Spans:         nil,
			StartPos:      pgtype.Int4{Int32: int32(start), Valid: true},
			EndPos:        pgtype.Int4{Int32: int32(start + 6), Valid: true},
		}},
	)

	require.Equal(t, 1, masks.neutralized)
	require.Equal(t, 0, masks.withheld)
	require.NotContains(t, masks.content[messageID], "HUSH42")
}

func TestMaskFindingSpansIgnoresToolFieldSpans(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "prose without the matched value"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "assistant", content)},
		[]platformrepo.ListRiskFindingSpansForRecallRow{{
			ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
			Source:        "gitleaks",
			RuleID:        pgtype.Text{String: "secret.generic", Valid: true},
			Spans: []byte(`[
				{"match":"SECRET","field":"tool.args","start_pos":0,"end_pos":6},
				{"match":"delete_all","field":"tool.name","start_pos":0,"end_pos":10}
			]`),
		}},
	)

	// A tool-field span's offsets index the tool payload, not the content
	// column, and tool payloads are redacted unconditionally anyway.
	require.Equal(t, 0, masks.neutralized)
	require.Equal(t, 0, masks.withheld)
	require.Empty(t, masks.content)
}

// Derived-source findings (a flagged tool name, an account email, a judge
// verdict) are persisted with empty field and zero offsets: their match is
// derived metadata, not a slice of the prose, so the prose serves unmasked —
// same as the dashboard — rather than being withheld.
func TestMaskFindingSpansServesDerivedFindingsUnmasked(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"destructive_tool", "shadow_mcp", "llm_judge"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			messageID := uuid.New()
			masks := maskFindingSpans(
				[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "assistant", "prose the finding does not anchor into")},
				[]platformrepo.ListRiskFindingSpansForRecallRow{{
					ChatMessageID: uuid.NullUUID{UUID: messageID, Valid: true},
					Source:        source,
					RuleID:        pgtype.Text{String: "rule.derived", Valid: true},
					Match:         pgtype.Text{String: "derived-metadata", Valid: true},
					Spans:         []byte(`[{"match":"derived-metadata","field":"","start_pos":0,"end_pos":0}]`),
				}},
			)

			require.Equal(t, 0, masks.neutralized)
			require.Equal(t, 0, masks.withheld)
			require.Empty(t, masks.content)
		})
	}
}

func TestTurnsFromRowsAdaptsTranscript(t *testing.T) {
	t.Parallel()

	userID, assistantID, toolID, systemID, offloadedID, unanalyzedID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()

	userRow := recallMessageRow(userID, "user", "<message-context>surface: claude-code</message-context>Fix the login flow")
	userRow.Source = pgtype.Text{String: "claude-code", Valid: true}

	assistantRow := recallMessageRow(assistantID, "assistant", "Working on it.")
	assistantRow.ToolCalls = []byte(`[
		{"id":"call_1","type":"function","function":{"name":"Edit","arguments":"{\"file_path\":\"/repo/auth/login.go\"}"}},
		{"id":"call_2","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"go test ./...\"}"}}
	]`)

	toolRow := recallMessageRow(toolID, "tool", "edit applied")

	systemRow := recallMessageRow(systemID, "system", "system prompt that must not surface")

	offloadedRow := recallMessageRow(offloadedID, "user", "")
	offloadedRow.ContentAssetUrl = pgtype.Text{String: "https://assets.example/blob", Valid: true}

	unanalyzedRow := recallMessageRow(unanalyzedID, "user", "also check signup")
	unanalyzedRow.RiskAnalyzedAt = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

	extract := turnsFromRows([]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{
		userRow, assistantRow, toolRow, systemRow, offloadedRow, unanalyzedRow,
	}, nil)

	require.Equal(t, 1, extract.offloaded)
	require.Equal(t, 1, extract.unanalyzed)
	require.Equal(t, "claude-code", extract.source)

	require.Len(t, extract.turns, 6)
	require.Equal(t, "user", extract.turns[0].Role)
	require.Equal(t, "Fix the login flow", extract.turns[0].Text, "harness envelopes are stripped from user prose")
	require.Equal(t, "Working on it.", extract.turns[1].Text)
	require.Equal(t, "Edit", extract.turns[2].ToolName)
	require.JSONEq(t, `{"file_path":"/repo/auth/login.go"}`, extract.turns[2].ToolInput, "arguments are unquoted to the inner JSON text")
	require.Equal(t, "Bash", extract.turns[3].ToolName)
	require.Equal(t, "edit applied", extract.turns[4].ToolResult)
	require.Equal(t, unanalyzedContentPlaceholder, extract.turns[5].Text,
		"prose without a completed risk analysis is withheld, not served with a warning")
}

// truncateDigest is the hard backstop over the renderer's per-section bounds:
// the returned document never exceeds the cap, cuts on a rune boundary, and
// always ends with the explicit truncation marker.
func TestTruncateDigestEnforcesHardCap(t *testing.T) {
	t.Parallel()

	small, truncated := truncateDigest("short digest")
	require.False(t, truncated)
	require.Equal(t, "short digest", small)

	// Multibyte content across the cut point must back off to a rune start:
	// with a 3-byte rune the cap arithmetic lands the cut mid-rune, so the
	// back-off branch is actually exercised.
	huge := strings.Repeat("€", recallDigestByteCap)
	capped, truncated := truncateDigest(huge)
	require.True(t, truncated)
	require.LessOrEqual(t, len(capped), recallDigestByteCap)
	require.True(t, strings.HasSuffix(capped, recallDigestTruncationMarker))
	require.True(t, utf8.ValidString(capped), "truncation must not split a rune")
}

func TestTurnsFromRowsUnwrapsStringEncodedToolCalls(t *testing.T) {
	t.Parallel()

	array := `[{"id":"call_1","type":"function","function":{"name":"Edit","arguments":"{\"file_path\":\"/repo/auth/login.go\"}"}}]`
	// The whole array stored double-encoded, as a JSON string.
	wrapped, err := json.Marshal(array)
	require.NoError(t, err)

	row := recallMessageRow(uuid.New(), "assistant", "Working on it.")
	row.ToolCalls = wrapped

	extract := turnsFromRows([]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{row}, nil)

	require.Equal(t, 0, extract.undecodedToolCalls)
	require.Len(t, extract.turns, 2)
	require.Equal(t, "Edit", extract.turns[1].ToolName)
	require.JSONEq(t, `{"file_path":"/repo/auth/login.go"}`, extract.turns[1].ToolInput)
}

// Which tool_calls payload shapes count as lost tool activity: payloads that
// carry entries but decode to no usable calls are counted (never dropped
// silently), while genuinely empty payloads are not.
func TestTurnsFromRowsCountsUndecodableToolCalls(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		payload       []byte
		wantUndecoded int
	}{
		"not an array":         {payload: []byte(`{"not":"an array"}`), wantUndecoded: 1},
		"entries lack names":   {payload: []byte(`[{"id":"call_1"}]`), wantUndecoded: 1},
		"garbage":              {payload: []byte(`not json at all`), wantUndecoded: 1},
		"absent":               {payload: nil, wantUndecoded: 0},
		"json null":            {payload: []byte(`null`), wantUndecoded: 0},
		"empty array":          {payload: []byte(`[]`), wantUndecoded: 0},
		"wrapped empty array":  {payload: []byte(`"[]"`), wantUndecoded: 0},
		"wrapped empty string": {payload: []byte(`""`), wantUndecoded: 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			row := recallMessageRow(uuid.New(), "assistant", "Working on it.")
			row.ToolCalls = tc.payload

			extract := turnsFromRows([]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{row}, nil)

			require.Equal(t, tc.wantUndecoded, extract.undecodedToolCalls)
			require.Len(t, extract.turns, 1, "only the prose turn survives")
			require.Empty(t, extract.turns[0].ToolName)
		})
	}
}

// A payload mixing valid and malformed entries keeps the valid calls in the
// digest while still reporting the malformed ones as lost tool activity.
func TestTurnsFromRowsKeepsValidCallsAmongMalformed(t *testing.T) {
	t.Parallel()

	row := recallMessageRow(uuid.New(), "assistant", "Working on it.")
	row.ToolCalls = []byte(`[
		{"id":"call_1","type":"function","function":{"name":"Edit","arguments":"{}"}},
		{"id":"call_2","type":"function","function":{"arguments":"{}"}},
		42
	]`)

	extract := turnsFromRows([]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{row}, nil)

	require.Equal(t, 1, extract.undecodedToolCalls)
	require.Len(t, extract.turns, 2)
	require.Equal(t, "Edit", extract.turns[1].ToolName)
}

func TestTurnsFromRowsSubstitutesMaskedContent(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	row := recallMessageRow(messageID, "user", "raw secret content")

	extract := turnsFromRows(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{row},
		map[uuid.UUID]string{messageID: "masked content"},
	)

	require.Len(t, extract.turns, 1)
	require.Equal(t, "masked content", extract.turns[0].Text)
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
