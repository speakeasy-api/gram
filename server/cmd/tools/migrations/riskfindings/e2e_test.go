package riskfindings

import (
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/pipeline"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

// chFindingRow is the subset of a ClickHouse risk_findings row the e2e test
// asserts on: identity, anchors, attribution, reveal metadata, and the masked
// match columns.
type chFindingRow struct {
	chatMessageID    string
	contentPartID    string
	chatID           string
	messageCreatedAt time.Time
	assistantID      string
	surface          string
	field            string
	path             string
	toolCallID       string
	startPos         int32
	endPos           int32
	matchLen         uint32
	matchRedacted    string
	globalHS256      string
	tenantHS256      string
	category         string
	ruleID           string
	source           string
}

func queryFindings(t *testing.T, conn clickhouse.Conn, orgID string) map[uuid.UUID]chFindingRow {
	t.Helper()

	rows, err := conn.Query(t.Context(), `
		SELECT id, chat_message_id, content_part_id, chat_id,
		       message_created_at, assistant_id,
		       surface, field, path, tool_call_id,
		       start_pos, end_pos, match_len, match_redacted,
		       fingerprint_global_hs256, fingerprint_tenant_hs256,
		       category, rule_id, source
		FROM risk_findings
		WHERE organization_id = ?
	`, orgID)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	out := map[uuid.UUID]chFindingRow{}
	for rows.Next() {
		var (
			id  uuid.UUID
			row chFindingRow
		)
		require.NoError(t, rows.Scan(
			&id, &row.chatMessageID, &row.contentPartID, &row.chatID,
			&row.messageCreatedAt, &row.assistantID,
			&row.surface, &row.field, &row.path, &row.toolCallID,
			&row.startPos, &row.endPos, &row.matchLen, &row.matchRedacted,
			&row.globalHS256, &row.tenantHS256,
			&row.category, &row.ruleID, &row.source,
		))
		out[id] = row
	}
	require.NoError(t, rows.Err())
	return out
}

// TestPipelineInsertsRevealMetadata drives the full source -> transform -> sink
// pipeline against real Postgres and ClickHouse and asserts the new reveal
// columns round-trip: anchors and attribution, message_created_at/assistant_id,
// surface/field/path/tool_call_id, the span explode (one ClickHouse row per
// recorded span, deterministic ids), and the partial-mask match_redacted
// format.
func TestPipelineInsertsRevealMetadata(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	// Recent relative dates: risk_findings has a 90-day created_at TTL, so
	// hardcoded old dates would expire at insert.
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-48 * time.Hour)
	scanAt := base.Add(2 * time.Hour)

	chatID := tn.newChat(t)
	assistantID, _ := tn.linkAssistant(t, chatID)
	msgID := tn.newMessage(t, chatID, base)

	// A: a span-less presidio email finding.
	emailFinding := tn.newFinding(t, msgID, scanAt, findingSpec{
		source: "presidio", ruleID: "pii.email_address",
		match: "alice@example.com", startPos: 6, endPos: 23,
	})

	// B: a span-less gitleaks secret.
	gitleaksFinding := tn.newFinding(t, msgID, scanAt, findingSpec{
		source: "gitleaks", ruleID: "generic-api-key",
		match: "AKIAIOSFODNN7EXAMPLE", startPos: 4, endPos: 24,
	})

	// C: a custom-rule finding with three recorded spans — one per surface kind.
	spans := []risk_analysis.FindingSpan{
		{Match: "secret-token-abc", Field: "content", Path: "", StartPos: 10, EndPos: 26},
		{Match: "secret-token-def", Field: "tool.args", Path: "", StartPos: 5, EndPos: 21},
		{Match: "secret-token-ghi", Field: "tool.args", Path: "config.token", StartPos: 0, EndPos: 16},
	}
	spanFinding := tn.newFinding(t, msgID, scanAt, findingSpec{
		source: "custom", ruleID: "custom.internal_token",
		match: spans[0].Match, startPos: int32(spans[0].StartPos), endPos: int32(spans[0].EndPos),
		spans: spans,
	})

	// D: a content-part-anchored finding (no chat message).
	partFinding, partID := tn.newContentPartFinding(t, chatID, scanAt)

	source := NewSource(tn.pool)
	sink := NewSink(conn, 16, 4, false, true)
	require.NoError(t, pipeline.Run[SourceRow, FindingRow](
		t.Context(), source, NewTransformer(testFingerprinter(t)), sink,
		pipeline.Criteria{CriteriaOrgID: tn.orgID}, 16,
	))

	require.EqualValues(t, 4, source.Scanned())
	require.EqualValues(t, 6, sink.Inserted(), "3 single rows + 3 exploded span rows")
	require.NotEqual(t, uuid.Nil, sink.LastCommitted())

	got := queryFindings(t, conn, tn.orgID)
	require.Len(t, got, 6)

	// A: presidio email — full anchor + attribution, legacy_presidio surface,
	// domain-only mask.
	a := got[emailFinding]
	require.Equal(t, msgID.String(), a.chatMessageID)
	require.Empty(t, a.contentPartID)
	require.Equal(t, chatID.String(), a.chatID)
	require.True(t, base.Equal(a.messageCreatedAt), "email row stamps the message event time, got %s", a.messageCreatedAt)
	require.Equal(t, assistantID.String(), a.assistantID)
	require.Equal(t, surfaceLegacyPresidio, a.surface)
	require.Empty(t, a.field)
	require.Empty(t, a.path)
	require.Empty(t, a.toolCallID)
	require.Equal(t, int32(6), a.startPos)
	require.Equal(t, int32(23), a.endPos)
	require.Equal(t, uint32(len("alice@example.com")), a.matchLen)
	require.Equal(t, "***@example.com", a.matchRedacted)
	require.NotEmpty(t, a.globalHS256)
	require.NotEmpty(t, a.tenantHS256)
	require.Equal(t, "pii", a.category)

	// B: gitleaks — scan_surface, general-tier mask.
	b := got[gitleaksFinding]
	require.Equal(t, surfaceScanSurface, b.surface)
	require.Equal(t, "AKIA**************LE", b.matchRedacted)
	require.Equal(t, "secrets", b.category)
	require.Equal(t, assistantID.String(), b.assistantID)

	// C: one row per span. Span 0 keeps the Postgres id; spans 1..n derive
	// deterministic ids the reveal path can recompute.
	spanIDs := []uuid.UUID{spanFinding}
	for i := 1; i < len(spans); i++ {
		spanIDs = append(spanIDs, uuid.NewSHA1(uuid.NameSpaceURL, fmt.Appendf(nil, "gram:risk:finding:pgspan:%s:%d", spanFinding, i)))
	}
	wantSurfaces := []string{surfaceContent, surfaceToolArgs, surfaceJSONPath}
	wantMasks := []string{"secr**********bc", "secr**********ef", "secr**********hi"}
	for i, span := range spans {
		row, ok := got[spanIDs[i]]
		require.True(t, ok, "span %d row missing (id %s)", i, spanIDs[i])
		require.Equal(t, wantSurfaces[i], row.surface, "span %d surface", i)
		require.Equal(t, span.Field, row.field, "span %d field", i)
		require.Equal(t, span.Path, row.path, "span %d path", i)
		require.Empty(t, row.toolCallID, "span %d tool_call_id", i)
		require.Equal(t, int32(span.StartPos), row.startPos, "span %d start_pos", i)
		require.Equal(t, int32(span.EndPos), row.endPos, "span %d end_pos", i)
		require.Equal(t, uint32(len(span.Match)), row.matchLen, "span %d match_len", i)
		require.Equal(t, wantMasks[i], row.matchRedacted, "span %d mask", i)
		require.NotEmpty(t, row.tenantHS256, "span %d tenant fingerprint", i)
		// The shared row-level state fans out to every span row.
		require.Equal(t, msgID.String(), row.chatMessageID, "span %d anchor", i)
		require.Equal(t, assistantID.String(), row.assistantID, "span %d assistant", i)
		require.True(t, base.Equal(row.messageCreatedAt), "span %d message time, got %s", i, row.messageCreatedAt)
		require.Equal(t, "custom", row.category, "span %d category", i)
		require.Equal(t, "custom.internal_token", row.ruleID, "span %d rule", i)
	}
	// Distinct span matches keep distinct fingerprints.
	require.NotEqual(t, got[spanIDs[0]].globalHS256, got[spanIDs[1]].globalHS256)

	// D: content-part anchor — no chat message, so the message anchor stays
	// empty, attribution is unresolved, and the event time falls back to the
	// finding's own created_at (the ClickHouse column DEFAULT semantics).
	d := got[partFinding]
	require.Empty(t, d.chatMessageID)
	require.Equal(t, partID.String(), d.contentPartID)
	require.Empty(t, d.chatID)
	require.Empty(t, d.assistantID)
	require.True(t, scanAt.Equal(d.messageCreatedAt), "part row falls back to scan time, got %s", d.messageCreatedAt)
	require.Equal(t, surfaceLegacyPresidio, d.surface)
	require.Equal(t, "***@example.com", d.matchRedacted)
}

// TestPipelineRespectsFromBound proves the -from bound holds end to end: a
// finding created before the window stays out of ClickHouse.
func TestPipelineRespectsFromBound(t *testing.T) {
	t.Parallel()

	tn := seedTenant(t)
	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-48 * time.Hour)
	chatID := tn.newChat(t)
	msgID := tn.newMessage(t, chatID, base)

	before := tn.newFinding(t, msgID, base.Add(-time.Hour), findingSpec{
		source: "presidio", ruleID: "pii.email_address", match: "old@example.com", startPos: 0, endPos: 15,
	})
	inside := tn.newFinding(t, msgID, base.Add(time.Hour), findingSpec{
		source: "presidio", ruleID: "pii.email_address", match: "new@example.com", startPos: 0, endPos: 15,
	})

	source := NewSource(tn.pool)
	sink := NewSink(conn, 16, 4, false, true)
	require.NoError(t, pipeline.Run[SourceRow, FindingRow](
		t.Context(), source, NewTransformer(testFingerprinter(t)), sink,
		pipeline.Criteria{CriteriaOrgID: tn.orgID, CriteriaFrom: base}, 16,
	))

	require.EqualValues(t, 1, source.Scanned())
	require.EqualValues(t, 1, sink.Inserted())

	got := queryFindings(t, conn, tn.orgID)
	require.Len(t, got, 1)
	require.Contains(t, got, inside)
	require.NotContains(t, got, before)
}
