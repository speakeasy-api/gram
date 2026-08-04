package risk_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// testPepperVersion / testPepperKey are the raw keyring material backing the
// Fingerprinter the writer uses in these tests. Expected fingerprints are
// recomputed from this same material via the wantHMAC / wantTenantedHMAC
// helpers in fingerprint_test.go (same package).
const testPepperVersion = "v1"

var testPepperKey = []byte("finding-test-pepper-key-material")

// wantGlobalFingerprint mirrors the writer's global fingerprint encoding:
// base64url(HMAC-SHA256(pepper, match)).
func wantGlobalFingerprint(match string) string {
	return base64.RawURLEncoding.EncodeToString(wantHMAC(testPepperKey, []byte(match)))
}

// wantTenantFingerprint mirrors the tenant-scoped fingerprint: base64url of an
// HMAC keyed by the per-tenant key derived from the pepper via HKDF.
func wantTenantFingerprint(t *testing.T, tenantID, match string) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString(wantTenantedHMAC(t, testPepperKey, tenantID, []byte(match)))
}

// finding builds a Finding with every field populated except the dead-letter
// reason, so the happy path (fingerprints computed) is exercised by default.
// Tests selectively clear or override fields to probe edge cases.
func finding() *riskv1.Finding {
	return riskv1.Finding_builder{
		Id:                new("finding-1"),
		RequestId:         new("req-1"),
		ChatMessageId:     new("chat-1"),
		ContentPartId:     nil,
		ProjectId:         new("proj-1"),
		OrganizationId:    new("org-1"),
		RiskPolicyId:      new("policy-1"),
		RiskPolicyVersion: new(int64(7)),
		CreatedAt:         new("2026-06-27T12:30:00Z"),
		RuleId:            new("rule-1"),
		Description:       new("a secret leaked"),
		Match:             new("hunter2"),
		StartPos:          new(int32(3)),
		EndPos:            new(int32(10)),
		Tags:              []string{"pii", "secret"},
		Source:            new("input"),
		Confidence:        new(0.95),
		Surface:           new("json_path"),
		Field:             new("tool.args"),
		Path:              new("command.0"),
		ToolCallId:        new("call_abc123"),
	}.Build()
}

// fakeCHInserter captures the rows the writer built so tests can inspect the
// ClickHouse payload. err lets tests exercise the shadow-mode error path.
type fakeCHInserter struct {
	mu    sync.Mutex
	calls int
	rows  []chrepo.RiskFindingRow
	err   error
}

func (f *fakeCHInserter) InsertRiskFindings(_ context.Context, rows []chrepo.RiskFindingRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.rows = rows
	return f.err
}

func newCHWriter(t *testing.T) (*risk.FindingCHWriter, *fakeCHInserter) {
	t.Helper()
	return newCHWriterWithMeter(t, testenv.NewMeterProvider(t))
}

func newCHWriterWithMeter(t *testing.T, mp metric.MeterProvider) (*risk.FindingCHWriter, *fakeCHInserter) {
	t.Helper()
	ins := &fakeCHInserter{}
	fp, err := risk.ParsePepperKeyRing(keyRingJSON(t, testPepperVersion, map[string][]byte{testPepperVersion: testPepperKey}))
	require.NoError(t, err)
	// nil exclusions DB: these unit-test findings carry non-UUID project/policy
	// ids, so exclusion resolution fails-open before any DB access. Exclusion
	// filtering against a real DB is covered by the integration test below.
	w := risk.NewFindingCHWriter(testenv.NewLogger(t), nil, mp, ins, fp)
	return w, ins
}

// chRows asserts InsertRiskFindings was called and returns the captured rows.
func chRows(t *testing.T, ins *fakeCHInserter) []chrepo.RiskFindingRow {
	t.Helper()
	require.NotNil(t, ins.rows, "InsertRiskFindings was never called")
	return ins.rows
}

// chFinding is finding() with a real UUIDv7 id. The CH writer parses id into a
// UUID column and skips findings whose id is not a valid UUID, so CH tests need
// a valid one (finding()'s "finding-1" is deliberately kept for the BQ tests).
func chFinding() *riskv1.Finding {
	f := finding()
	f.SetId(uuid.Must(uuid.NewV7()).String())
	return f
}

func TestFindingCHWriter_HandleBatch_MapsAllFields(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	f := finding()
	id := uuid.Must(uuid.NewV7())
	f.SetId(id.String())

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	require.Equal(t, 1, ins.calls)
	all := chRows(t, ins)
	require.Len(t, all, 1)
	row := all[0]

	require.Equal(t, id, row.ID)
	require.Equal(t, "req-1", row.RequestID)
	require.Equal(t, "chat-1", row.ChatMessageID)
	require.Equal(t, "proj-1", row.ProjectID)
	require.Equal(t, "org-1", row.OrganizationID)
	require.Equal(t, "policy-1", row.RiskPolicyID)
	require.Equal(t, int64(7), row.RiskPolicyVersion)
	require.Equal(t, "rule-1", row.RuleID)
	require.Equal(t, "a secret leaked", row.Description)
	require.Equal(t, "input", row.Source)
	require.InDelta(t, 0.95, row.Confidence, 0)
	require.Equal(t, []string{"pii", "secret"}, row.Tags)
	require.Equal(t, int32(3), row.StartPos)
	require.Equal(t, int32(10), row.EndPos)
	require.Empty(t, row.DeadLetterReason)
	require.True(t, time.Date(2026, 6, 27, 12, 30, 0, 0, time.UTC).Equal(row.CreatedAt))
	// Unresolvable chat_message_id: message_created_at falls back to the scan
	// time and no assistant is stamped.
	require.True(t, row.CreatedAt.Equal(row.MessageCreatedAt))
	require.Empty(t, row.AssistantID)

	// "chat-1" is not a UUID, so attribution resolution is skipped and the
	// denormalized fields stay empty. The classifier still runs: ("input",
	// "rule-1") matches nothing and falls back to custom.
	require.Empty(t, row.ChatID)
	require.Empty(t, row.UserID)
	require.Empty(t, row.ExternalUserID)
	require.Equal(t, "custom", row.Category)

	// The raw match is never stored: only its length, the shared partial-mask
	// display (maskdisplay: 7 runes = first 2 + stars + last 1), and one-way
	// fingerprints.
	require.Equal(t, uint32(len("hunter2")), row.MatchLen)
	require.Equal(t, "hu****2", row.MatchRedacted)
	require.NotContains(t, row.MatchRedacted, "hunter2")
	require.Equal(t, wantGlobalFingerprint("hunter2"), row.FingerprintGlobalHS256)
	require.Equal(t, wantTenantFingerprint(t, "org-1", "hunter2"), row.FingerprintTenantHS256)
	require.Equal(t, testPepperVersion, row.FingerprintPepperVersion)

	// Reveal metadata passes through verbatim from the message.
	require.Equal(t, "json_path", row.Surface)
	require.Equal(t, "tool.args", row.Field)
	require.Equal(t, "command.0", row.Path)
	require.Equal(t, "call_abc123", row.ToolCallID)
}

// match_redacted is the shared maskdisplay partial mask, per source: an
// account_identity email keeps only the domain (the local part — the PII —
// is never stored), a judge match displays as nothing, and shadow_mcp passes
// its non-secret server identifier through verbatim as maskdisplay's
// documented carve-out. Storing boundary characters of real matches is the
// signed-off relaxation from the reveal-from-ClickHouse design.
func TestFindingCHWriter_HandleBatch_MaskDisplayPerSource(t *testing.T) {
	t.Parallel()

	accountIdentity := chFinding()
	accountIdentity.SetSource(ra.SourceAccountIdentity)
	accountIdentity.SetMatch("user@example.com")

	judge := chFinding()
	judge.SetSource("prompt_injection")
	judge.SetMatch("the whole scanned content")

	shadowMCP := chFinding()
	shadowMCP.SetSource(shadowmcp.SourceShadowMCP)
	shadowMCP.SetMatch("mcp.internal.example")

	w, ins := newCHWriter(t)
	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{accountIdentity, judge, shadowMCP}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 3)
	byID := map[uuid.UUID]chrepo.RiskFindingRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	require.Equal(t, "***@example.com", byID[uuid.MustParse(accountIdentity.GetId())].MatchRedacted,
		"account_identity stores only the email domain")
	require.Empty(t, byID[uuid.MustParse(judge.GetId())].MatchRedacted,
		"judge matches are rendered artifacts; nothing worth displaying")
	require.Equal(t, "mcp.internal.example", byID[uuid.MustParse(shadowMCP.GetId())].MatchRedacted,
		"shadow_mcp server identifiers are the documented verbatim carve-out")
}

func TestFindingCHWriter_HandleBatch_NoMatchYieldsNoFingerprintsOrRedaction(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	f := chFinding()
	f.SetMatch("")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := chRows(t, ins)[0]
	require.Empty(t, row.FingerprintGlobalHS256)
	require.Empty(t, row.FingerprintTenantHS256)
	require.Empty(t, row.MatchRedacted)
	require.Equal(t, uint32(0), row.MatchLen)
}

func TestFindingCHWriter_HandleBatch_DeadLetterSuppressesFingerprintsAndRedaction(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	f := chFinding()
	f.SetDeadLetterReason("malformed")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := chRows(t, ins)[0]
	require.Equal(t, "malformed", row.DeadLetterReason)
	require.Empty(t, row.FingerprintGlobalHS256)
	require.Empty(t, row.FingerprintTenantHS256)
	require.Empty(t, row.MatchRedacted)
	require.Equal(t, uint32(0), row.MatchLen)
	require.Empty(t, row.Category, "dead-letter sentinels get no category, not the custom fallback")
}

func TestFindingCHWriter_HandleBatch_ClassifiesCategory(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	pii := chFinding()
	pii.SetSource("presidio")
	pii.SetRuleId("pii.email_address")

	secret := chFinding()
	secret.SetSource("gitleaks")
	secret.SetRuleId("secret.aws_access_key")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{pii, secret}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 2)
	byID := map[uuid.UUID]chrepo.RiskFindingRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	require.Equal(t, "pii", byID[uuid.MustParse(pii.GetId())].Category)
	require.Equal(t, "secrets", byID[uuid.MustParse(secret.GetId())].Category)
}

func TestFindingCHWriter_HandleBatch_TenantFingerprintRequiresOrg(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	f := chFinding()
	f.SetOrganizationId("   ") // trims to empty

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := chRows(t, ins)[0]
	require.Equal(t, wantGlobalFingerprint("hunter2"), row.FingerprintGlobalHS256)
	require.Empty(t, row.FingerprintTenantHS256, "no org id means no tenant-qualified fingerprint")
}

func TestFindingCHWriter_HandleBatch_InvalidTimestampSkipsFinding(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	bad := chFinding()
	bad.SetCreatedAt("not-a-timestamp")

	good := finding()
	goodID := uuid.Must(uuid.NewV7())
	good.SetId(goodID.String())

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{bad, good}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 1, "only the finding with a valid timestamp is inserted")
	require.Equal(t, goodID, rows[0].ID, "the surviving row must be the valid finding, not the skipped one")
}

// TestFindingCHWriter_HandleBatch_InvalidFalsePositiveAtSkipsFinding guards
// against a real regression: a false_positive_at that fails to parse used to
// fall through with falsePositiveAt left nil, appending the row as if it
// were an active (non-dismissed) finding — silently resurrecting a
// previously-dismissed finding in ClickHouse's dedup instead of dropping the
// malformed message, matching how an invalid created_at is already handled.
func TestFindingCHWriter_HandleBatch_InvalidFalsePositiveAtSkipsFinding(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	bad := chFinding()
	bad.SetFalsePositiveAt("not-a-timestamp")

	good := finding()
	goodID := uuid.Must(uuid.NewV7())
	good.SetId(goodID.String())

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{bad, good}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 1, "only the finding with a valid (or absent) false_positive_at is inserted")
	require.Equal(t, goodID, rows[0].ID, "the surviving row must be the valid finding, not the skipped one")
}

func TestFindingCHWriter_HandleBatch_EmptyBatchSkipsInsert(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	require.NoError(t, w.HandleBatch(context.Background(), nil, nil))

	require.Zero(t, ins.calls, "an empty batch should not issue an insert")
	require.Nil(t, ins.rows)
}

func TestFindingCHWriter_HandleBatch_InserterErrorIsSwallowed(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)
	ins.err = errors.New("clickhouse unavailable")

	// Shadow mode: the writer logs but does not surface insert failures.
	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{chFinding()}, nil))
}

func TestFindingCHWriter_HandleBatch_RecordsInsertedMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inserterErr error
		wantOutcome string
	}{
		{name: "success", inserterErr: nil, wantOutcome: "success"},
		{name: "failure", inserterErr: errors.New("clickhouse down"), wantOutcome: "failure"},
	}

	for _, tt := range tests {
		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

		w, ins := newCHWriterWithMeter(t, mp)
		ins.err = tt.inserterErr

		require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{chFinding(), chFinding()}, nil))

		point := chMessagesInsertedPoint(t, reader)
		require.Equal(t, int64(2), point.Value)

		outcome, ok := point.Attributes.Value(attr.OutcomeKey)
		require.True(t, ok, "outcome attribute should be present")
		require.Equal(t, tt.wantOutcome, outcome.AsString())
	}
}

// Integration test against a real Postgres: a going-forward exclusion must drop
// the matching finding before it reaches ClickHouse, mirroring the Postgres
// scan path. Findings from the shadow path are otherwise unfiltered.
func TestFindingCHWriter_HandleBatch_AnnotatesExcludedFindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Global exact exclusion suppressing the secret "hunter2" for this project.
	exclusion, err := riskrepo.New(ti.conn).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		RiskPolicyID:   uuid.NullUUID{}, // global: applies to every policy
		MatchType:      "exact",
		MatchValue:     "hunter2",
		Enabled:        true,
	})
	require.NoError(t, err)

	ins := &fakeCHInserter{}
	fp, err := risk.ParsePepperKeyRing(keyRingJSON(t, testPepperVersion, map[string][]byte{testPepperVersion: testPepperKey}))
	require.NoError(t, err)
	w := risk.NewFindingCHWriter(testenv.NewLogger(t), ti.conn, testenv.NewMeterProvider(t), ins, fp)

	policyID := uuid.Must(uuid.NewV7()).String()

	// Excluded: match "hunter2" (== the exclusion value).
	excluded := chFinding()
	excluded.SetProjectId(authCtx.ProjectID.String())
	excluded.SetRiskPolicyId(policyID)
	excluded.SetMatch("hunter2")

	// Not excluded: a different value the exclusion does not cover.
	kept := chFinding()
	kept.SetProjectId(authCtx.ProjectID.String())
	kept.SetRiskPolicyId(policyID)
	kept.SetMatch("different-secret")

	require.NoError(t, w.HandleBatch(ctx, []*riskv1.Finding{excluded, kept}, nil))

	// Both rows are inserted: excluded findings are annotated, not dropped.
	rows := chRows(t, ins)
	require.Len(t, rows, 2, "excluded findings are annotated, not dropped")

	byID := map[uuid.UUID]chrepo.RiskFindingRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	excludedRow := byID[uuid.MustParse(excluded.GetId())]
	require.NotNil(t, excludedRow.ExclusionID, "excluded finding must carry the exclusion id")
	require.Equal(t, exclusion.ID, *excludedRow.ExclusionID)
	require.NotNil(t, excludedRow.ExcludedAt, "excluded finding must carry excluded_at")

	keptRow := byID[uuid.MustParse(kept.GetId())]
	require.Nil(t, keptRow.ExclusionID, "non-excluded finding must not carry an exclusion id")
	require.Nil(t, keptRow.ExcludedAt, "non-excluded finding must not carry excluded_at")
}

// Integration test against a real Postgres: the writer batch-resolves the
// denormalized attribution (chat id, user ids) for findings that reference a
// real chat message. Message-level ids win over chat-level ids; unresolvable
// message ids leave the attribution empty without dropping the finding.
func TestFindingCHWriter_HandleBatch_ResolvesAttribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	queries := riskrepo.New(ti.conn)
	chatID, err := queries.CreateChatForTest(t.Context(), riskrepo.CreateChatForTestParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText("chat-user"),
		ExternalUserID: conv.ToPGText("chat-user@example.com"),
	})
	require.NoError(t, err)

	// Message with its own attribution: message-level ids win over the chat's.
	msgOwn, err := queries.CreateChatMessageForTest(t.Context(), riskrepo.CreateChatMessageForTestParams{
		ChatID:         chatID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Content:        "hello",
		UserID:         conv.ToPGText("msg-user"),
		ExternalUserID: conv.ToPGText("msg-user@example.com"),
	})
	require.NoError(t, err)

	// Message without its own attribution: falls back to the chat's ids.
	msgFallback, err := queries.CreateChatMessageForTest(t.Context(), riskrepo.CreateChatMessageForTestParams{
		ChatID:         chatID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Content:        "hello again",
		UserID:         conv.ToPGTextEmpty(""),
		ExternalUserID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	// Live assistant link for the chat: stamped as assistant_id on both
	// resolved findings.
	assistantID, err := queries.CreateAssistantForTest(t.Context(), riskrepo.CreateAssistantForTestParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "attribution-assistant",
	})
	require.NoError(t, err)
	_, err = queries.CreateAssistantThreadForTest(t.Context(), riskrepo.CreateAssistantThreadForTestParams{
		AssistantID:   assistantID,
		ProjectID:     *authCtx.ProjectID,
		CorrelationID: "attribution-thread",
		ChatID:        chatID,
	})
	require.NoError(t, err)

	ins := &fakeCHInserter{}
	fp, err := risk.ParsePepperKeyRing(keyRingJSON(t, testPepperVersion, map[string][]byte{testPepperVersion: testPepperKey}))
	require.NoError(t, err)
	w := risk.NewFindingCHWriter(testenv.NewLogger(t), ti.conn, testenv.NewMeterProvider(t), ins, fp)

	own := chFinding()
	own.SetChatMessageId(msgOwn.String())

	fallback := chFinding()
	fallback.SetChatMessageId(msgFallback.String())

	// Well-formed UUID that matches no chat message: enrichment resolves
	// nothing, the finding still inserts with empty attribution.
	unknown := chFinding()
	unknown.SetChatMessageId(uuid.Must(uuid.NewV7()).String())

	require.NoError(t, w.HandleBatch(ctx, []*riskv1.Finding{own, fallback, unknown}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 3)
	byID := map[uuid.UUID]chrepo.RiskFindingRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}

	ownRow := byID[uuid.MustParse(own.GetId())]
	require.Equal(t, chatID.String(), ownRow.ChatID)
	require.Equal(t, "msg-user", ownRow.UserID)
	require.Equal(t, "msg-user@example.com", ownRow.ExternalUserID)
	require.Equal(t, assistantID.String(), ownRow.AssistantID)
	// The message was inserted moments ago; a resolved finding carries the
	// message's event time, not the finding's fixed scan time.
	require.WithinDuration(t, time.Now().UTC(), ownRow.MessageCreatedAt, time.Minute)

	fallbackRow := byID[uuid.MustParse(fallback.GetId())]
	require.Equal(t, chatID.String(), fallbackRow.ChatID)
	require.Equal(t, "chat-user", fallbackRow.UserID)
	require.Equal(t, "chat-user@example.com", fallbackRow.ExternalUserID)
	require.Equal(t, assistantID.String(), fallbackRow.AssistantID)

	unknownRow := byID[uuid.MustParse(unknown.GetId())]
	require.Empty(t, unknownRow.ChatID)
	require.Empty(t, unknownRow.UserID)
	require.Empty(t, unknownRow.ExternalUserID)
	require.Empty(t, unknownRow.AssistantID)
	// Unresolved attribution: message_created_at falls back to the finding's
	// own scan time.
	require.True(t, unknownRow.CreatedAt.Equal(unknownRow.MessageCreatedAt))
}

func TestFindingCHWriter_HandleBatch_ResolvesContentPartAttribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	queries := riskrepo.New(ti.conn)
	chatID, err := queries.CreateChatForTest(t.Context(), riskrepo.CreateChatForTestParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText("chat-user"),
		ExternalUserID: conv.ToPGText("chat-user@example.com"),
	})
	require.NoError(t, err)

	parentMessageID, err := queries.CreateChatMessageForTest(t.Context(), riskrepo.CreateChatMessageForTestParams{
		ChatID:         chatID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Content:        "prompt text",
		UserID:         conv.ToPGText("parent-user"),
		ExternalUserID: conv.ToPGText("parent-user@example.com"),
	})
	require.NoError(t, err)

	contentPartID, err := queries.CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              chatID,
		ProjectID:           uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Kind:                "prompt_attachment",
		ContentAssetUrl:     "file:///attachment.txt",
		ParentChatMessageID: uuid.NullUUID{UUID: parentMessageID, Valid: true},
	})
	require.NoError(t, err)
	attributionRows, err := queries.GetChatContentPartAttribution(t.Context(), riskrepo.GetChatContentPartAttributionParams{
		Ids:        []uuid.UUID{contentPartID},
		ProjectIds: []uuid.UUID{*authCtx.ProjectID},
	})
	require.NoError(t, err)
	require.Len(t, attributionRows, 1)
	require.Equal(t, chatID, attributionRows[0].ChatID)
	require.Equal(t, "parent-user", attributionRows[0].UserID)

	// Scoping to a different project returns nothing at all, so a caller that
	// forgot the per-finding project check still cannot read across tenants.
	otherScoped, err := queries.GetChatContentPartAttribution(t.Context(), riskrepo.GetChatContentPartAttributionParams{
		Ids:        []uuid.UUID{contentPartID},
		ProjectIds: []uuid.UUID{uuid.New()},
	})
	require.NoError(t, err)
	require.Empty(t, otherScoped)

	ins := &fakeCHInserter{}
	fp, err := risk.ParsePepperKeyRing(keyRingJSON(t, testPepperVersion, map[string][]byte{testPepperVersion: testPepperKey}))
	require.NoError(t, err)
	w := risk.NewFindingCHWriter(testenv.NewLogger(t), ti.conn, testenv.NewMeterProvider(t), ins, fp)

	f := chFinding()
	f.ClearChatMessageId()
	f.SetContentPartId(contentPartID.String())
	f.SetProjectId(authCtx.ProjectID.String())
	f.SetRiskPolicyId(uuid.NewString())

	require.NoError(t, w.HandleBatch(ctx, []*riskv1.Finding{f}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].ChatMessageID)
	require.Equal(t, contentPartID.String(), rows[0].ContentPartID)
	require.Equal(t, chatID.String(), rows[0].ChatID)
	require.Equal(t, "parent-user", rows[0].UserID)
	require.Equal(t, "parent-user@example.com", rows[0].ExternalUserID)

	// A finding claiming a part that belongs to another project resolves the
	// row but must not inherit its chat or user ids. A findings batch can span
	// projects, so this is enforced per finding rather than by scoping the query.
	otherProject := &riskv1.Finding{}
	otherProject.SetId(uuid.NewString())
	otherProject.SetCreatedAt(time.Now().UTC().Format(time.RFC3339))
	otherProject.SetOrganizationId(authCtx.ActiveOrganizationID)
	otherProject.SetProjectId(uuid.NewString())
	otherProject.SetContentPartId(contentPartID.String())
	otherProject.SetRiskPolicyId(uuid.NewString())

	ins2 := &fakeCHInserter{}
	w2 := risk.NewFindingCHWriter(testenv.NewLogger(t), ti.conn, testenv.NewMeterProvider(t), ins2, fp)
	require.NoError(t, w2.HandleBatch(ctx, []*riskv1.Finding{otherProject}, nil))

	crossRows := chRows(t, ins2)
	require.Len(t, crossRows, 1)
	require.Equal(t, contentPartID.String(), crossRows[0].ContentPartID)
	require.Empty(t, crossRows[0].ChatID)
	require.Empty(t, crossRows[0].UserID)
	require.Empty(t, crossRows[0].ExternalUserID)
}

// A part whose parent_chat_message_id points outside its own chat must not
// inherit that message's user ids. The join is what enforces this, so a stale
// or forged parent id falls back to the part's own chat instead of leaking the
// other chat's identifiers.
func TestFindingCHWriter_HandleBatch_IgnoresParentMessageFromAnotherChat(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	queries := riskrepo.New(ti.conn)
	ownChatID, err := queries.CreateChatForTest(t.Context(), riskrepo.CreateChatForTestParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText("own-chat-user"),
		ExternalUserID: conv.ToPGText("own-chat-user@example.com"),
	})
	require.NoError(t, err)

	foreignChatID, err := queries.CreateChatForTest(t.Context(), riskrepo.CreateChatForTestParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText("foreign-chat-user"),
		ExternalUserID: conv.ToPGText("foreign-chat-user@example.com"),
	})
	require.NoError(t, err)

	foreignMessageID, err := queries.CreateChatMessageForTest(t.Context(), riskrepo.CreateChatMessageForTestParams{
		ChatID:         foreignChatID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Content:        "prompt in another chat",
		UserID:         conv.ToPGText("foreign-message-user"),
		ExternalUserID: conv.ToPGText("foreign-message-user@example.com"),
	})
	require.NoError(t, err)

	contentPartID, err := queries.CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              ownChatID,
		ProjectID:           uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Kind:                "prompt_attachment",
		ContentAssetUrl:     "file:///attachment.txt",
		ParentChatMessageID: uuid.NullUUID{UUID: foreignMessageID, Valid: true},
	})
	require.NoError(t, err)

	rows, err := queries.GetChatContentPartAttribution(t.Context(), riskrepo.GetChatContentPartAttributionParams{
		Ids:        []uuid.UUID{contentPartID},
		ProjectIds: []uuid.UUID{*authCtx.ProjectID},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, ownChatID, rows[0].ChatID)
	require.Equal(t, "own-chat-user", rows[0].UserID)
	require.Equal(t, "own-chat-user@example.com", rows[0].ExternalUserID)
}

// Nothing in the schema keeps a part's project_id in step with its chat's, so a
// part claiming one project while its chat sits in another must resolve no
// attribution at all: otherwise the caller's per-finding project check would be
// comparing against a project id the chat never belonged to.
func TestFindingCHWriter_HandleBatch_RejectsPartWhoseChatIsInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "other-attr-" + uuid.New().String()[:8]
	otherProject, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	queries := riskrepo.New(ti.conn)
	// The chat lives in the other project while the part claims the caller's.
	foreignChatID, err := queries.CreateChatForTest(t.Context(), riskrepo.CreateChatForTestParams{
		ProjectID:      otherProject.ID,
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText("foreign-project-user"),
		ExternalUserID: conv.ToPGText("foreign-project-user@example.com"),
	})
	require.NoError(t, err)

	contentPartID, err := queries.CreateChatContentPartForTest(t.Context(), riskrepo.CreateChatContentPartForTestParams{
		ChatID:              foreignChatID,
		ProjectID:           uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Kind:                "prompt_attachment",
		ContentAssetUrl:     "file:///attachment.txt",
		ParentChatMessageID: uuid.NullUUID{},
	})
	require.NoError(t, err)

	rows, err := queries.GetChatContentPartAttribution(t.Context(), riskrepo.GetChatContentPartAttributionParams{
		Ids:        []uuid.UUID{contentPartID},
		ProjectIds: []uuid.UUID{*authCtx.ProjectID},
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// chMessagesInsertedPoint returns the single data point for the CH
// messages-inserted counter, failing the test if it is missing.
func chMessagesInsertedPoint(t *testing.T, reader *sdkmetric.ManualReader) metricdata.DataPoint[int64] {
	t.Helper()

	const metricName = "gram.risk_findings.ch_messages_inserted"

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.Truef(t, ok, "metric %q is %T, not Sum[int64]", metricName, m.Data)
			require.Len(t, sum.DataPoints, 1)
			return sum.DataPoints[0]
		}
	}

	require.Failf(t, "metric not found", "missing metric %q", metricName)
	return metricdata.DataPoint[int64]{}
}
