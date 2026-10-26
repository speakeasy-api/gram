package platformmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
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

func recallFinding(messageID uuid.UUID, surface, match string, start int) chrepo.RiskFindingUnmaskRow {
	return chrepo.RiskFindingUnmaskRow{
		ChatMessageID: messageID.String(),
		Source:        "gitleaks",
		RuleID:        "secret.generic",
		StartPos:      int32(start),              // #nosec G115 -- fixture offsets are small constants.
		EndPos:        int32(start + len(match)), // #nosec G115 -- fixture offsets are small constants.
		MatchLen:      uint32(len(match)),        // #nosec G115 -- fixture matches are bounded literals.
		MatchRedacted: maskdisplay.Display("gitleaks", "secret.generic", match),
		Surface:       surface,
	}
}

// ClickHouse stores no raw match: in-range offsets over drifted content must
// fail the ingest-time display check and withhold the message.
func TestMaskFindingSpansWithholdsOnStaleOffsets(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaDRIFTYbbbb SECRET moved here"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{recallFinding(messageID, "content", "SECRET", 4)},
	)

	require.Equal(t, 1, masks.withheld)
	require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
}

// Rows written before the surface column derive it like the Postgres path.
func TestMaskFindingSpansDerivesMissingSurface(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{recallFinding(messageID, "", "SECRET", 4)},
	)

	require.Equal(t, 0, masks.withheld)
	require.Equal(t, "aaaa"+maskdisplay.Display("gitleaks", "secret.generic", "SECRET")+"bbbb", masks.content[messageID])
}

func TestMaskFindingSpansReplacesEverySpan(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "token AKIAIOSFODNN7EXAMPLE and key HUSH42 end"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{
			recallFinding(messageID, "content", "AKIAIOSFODNN7EXAMPLE", strings.Index(content, "AKIA")),
			recallFinding(messageID, "content", "HUSH42", strings.Index(content, "HUSH42")),
		},
	)

	require.Equal(t, 2, masks.neutralized)
	require.Equal(t, 0, masks.withheld)
	tokenMask := maskdisplay.Display("gitleaks", "secret.generic", "AKIAIOSFODNN7EXAMPLE")
	keyMask := maskdisplay.Display("gitleaks", "secret.generic", "HUSH42")
	require.Equal(t, "token "+tokenMask+" and key "+keyMask+" end", masks.content[messageID])
}

// Overlapping spans cannot both be honored, so the whole message is withheld
// rather than served with a finding unmasked.
func TestMaskFindingSpansWithholdsOnOverlap(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{
			recallFinding(messageID, "content", "SECRET", 4),
			recallFinding(messageID, "content", "SECRETbb", 4),
		},
	)

	require.Equal(t, 1, masks.withheld)
	require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
}

// Two policies flagging the same region mask it once, never a withhold.
func TestMaskFindingSpansCollapsesIdenticalDuplicateSpans(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"
	finding := recallFinding(messageID, "content", "SECRET", 4)

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{finding, finding},
	)

	require.Equal(t, 0, masks.withheld)
	require.Equal(t, 2, masks.neutralized)
	require.Equal(t, "aaaa"+maskdisplay.Display("gitleaks", "secret.generic", "SECRET")+"bbbb", masks.content[messageID])
}

func TestMaskFindingSpansKeepsMultibyteContentIntact(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	prefix := "héllo wörld — "
	secret := "SECRETVALUE123"
	content := prefix + secret + " — döne"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{recallFinding(messageID, "content", secret, strings.Index(content, secret))},
	)

	require.Equal(t, 0, masks.withheld)
	require.Equal(t, prefix+maskdisplay.Display("gitleaks", "secret.generic", secret)+" — döne", masks.content[messageID])
}

// Tool payloads are redacted unconditionally, and derived findings anchor no
// prose, so neither masks nor withholds the message.
func TestMaskFindingSpansSkipsToolAndDerivedSurfaces(t *testing.T) {
	t.Parallel()

	for _, surface := range []string{"tool_args", "derived", "none"} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()

			messageID := uuid.New()
			masks := maskFindingSpans(
				[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "assistant", "prose the finding does not anchor into")},
				[]chrepo.RiskFindingUnmaskRow{recallFinding(messageID, surface, "prose", 0)},
			)

			require.Equal(t, 0, masks.neutralized)
			require.Equal(t, 0, masks.withheld)
			require.Empty(t, masks.content)
		})
	}
}

// A surface the masker cannot place, such as a json_path span whose offsets
// index an extracted sub-value, withholds the message rather than serving it.
func TestMaskFindingSpansWithholdsOnUnknownSurface(t *testing.T) {
	t.Parallel()

	messageID := uuid.New()
	content := "aaaaSECRETbbbb"

	masks := maskFindingSpans(
		[]platformrepo.ListOwnedChatTranscriptMessagesForRecallRow{recallMessageRow(messageID, "user", content)},
		[]chrepo.RiskFindingUnmaskRow{recallFinding(messageID, "json_path", "SECRET", 4)},
	)

	require.Equal(t, 1, masks.withheld)
	require.Equal(t, withheldContentPlaceholder, masks.content[messageID])
}
