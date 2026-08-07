package riskfindings

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

func testFingerprinter(t *testing.T) risk.Fingerprinter {
	t.Helper()
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 32))
	keyring := fmt.Sprintf(`{"current":"v1","keys":{"v1":%q}}`, key)
	fp, err := risk.ParsePepperKeyRing([]byte(keyring))
	require.NoError(t, err)
	return fp
}

func anchoredTo(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: true}
}

func mustSpansJSON(t *testing.T, spans []risk_analysis.FindingSpan) []byte {
	t.Helper()
	b, err := json.Marshal(spans)
	require.NoError(t, err)
	return b
}

func TestTransformComputesFingerprintsAndMask(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	messageCreatedAt := time.Now().UTC().Add(-time.Minute)
	in := SourceRow{
		ID:                uuid.New(),
		CreatedAt:         time.Now().UTC(),
		OrganizationID:    "org_123",
		ProjectID:         uuid.New(),
		RiskPolicyID:      uuid.New(),
		RiskPolicyVersion: 7,
		ChatMessageID:     anchoredTo(uuid.New()),
		ContentPartID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Source:            "presidio",
		Found:             true,
		RuleID:            conv.PtrEmpty("pii.email_address"),
		Description:       conv.PtrEmpty("email"),
		Match:             conv.PtrEmpty("alice@example.com"),
		StartPos:          nil,
		EndPos:            nil,
		Confidence:        nil,
		Tags:              []string{"pii"},
		Spans:             nil,
		DeadLetterReason:  nil,
		ExcludedAt:        nil,
		ExclusionID:       nil,
		FalsePositiveAt:   nil,
		ChatID:            "",
		UserID:            "",
		ExternalUserID:    "",
		MessageCreatedAt:  messageCreatedAt,
		AssistantID:       "",
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	row := out[0]

	require.Equal(t, in.ID, row.ID)
	require.Equal(t, in.ProjectID.String(), row.ProjectID)
	require.Equal(t, in.ChatMessageID.UUID.String(), row.ChatMessageID)
	require.Empty(t, row.ContentPartID)
	require.Equal(t, "pii.email_address", row.RuleID)
	require.Equal(t, []string{"pii"}, row.Tags)
	require.Empty(t, row.RequestID)
	require.Equal(t, messageCreatedAt, row.MessageCreatedAt)
	require.Empty(t, row.AssistantID)

	require.NotEmpty(t, row.FingerprintGlobalHS256)
	require.NotEmpty(t, row.FingerprintTenantHS256)
	require.Equal(t, "v1", row.FingerprintPepperVersion)
	require.NotEqual(t, row.FingerprintGlobalHS256, row.FingerprintTenantHS256)

	require.Equal(t, uint32(len("alice@example.com")), row.MatchLen)
	// The partial-mask display: emails keep only the domain.
	require.Equal(t, "***@example.com", row.MatchRedacted)
	require.Equal(t, surfaceLegacyPresidio, row.Surface)
	require.Empty(t, row.Field)
	require.Empty(t, row.Path)
	require.Empty(t, row.ToolCallID)
}

func TestTransformMapsAttributionCategoryAndFalsePositive(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	falsePositiveAt := time.Now().UTC()
	chatID := uuid.New()
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "presidio", Found: true, RuleID: conv.PtrEmpty("pii.email_address"),
		Match: conv.PtrEmpty("alice@example.com"), Tags: []string{"pii"},
		FalsePositiveAt: &falsePositiveAt,
		ChatID:          chatID.String(),
		UserID:          "user_1",
		ExternalUserID:  "ext_1",
		AssistantID:     "assistant_1",
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	row := out[0]

	require.Equal(t, chatID.String(), row.ChatID)
	require.Equal(t, "user_1", row.UserID)
	require.Equal(t, "ext_1", row.ExternalUserID)
	require.Equal(t, "assistant_1", row.AssistantID)
	require.Equal(t, "pii", row.Category)
	require.NotNil(t, row.FalsePositiveAt)
	require.Equal(t, falsePositiveAt, *row.FalsePositiveAt)
}

func TestTransformContentPartAnchor(t *testing.T) {
	t.Parallel()

	// A content-part-anchored finding has no chat message: chat_message_id
	// stays empty (not the nil uuid), the part id is stamped, and the event
	// time is the finding's own created_at resolved by the source query.
	tf := NewTransformer(testFingerprinter(t))
	partID := uuid.New()
	createdAt := time.Now().UTC()
	in := SourceRow{
		ID: uuid.New(), CreatedAt: createdAt, OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(),
		ChatMessageID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ContentPartID: anchoredTo(partID),
		Source:        "presidio", Found: true, RuleID: conv.PtrEmpty("pii.email_address"),
		Match: conv.PtrEmpty("alice@example.com"), Tags: []string{"pii"},
		MessageCreatedAt: createdAt,
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Empty(t, out[0].ChatMessageID)
	require.Equal(t, partID.String(), out[0].ContentPartID)
	require.Equal(t, createdAt, out[0].MessageCreatedAt)
	require.Empty(t, out[0].AssistantID)
}

func TestTransformUnresolvedAttributionStaysEmpty(t *testing.T) {
	t.Parallel()

	// A finding whose chat message no longer resolves (deleted chat, missing
	// message) carries empty attribution rather than being dropped.
	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "gitleaks", Found: true, RuleID: conv.PtrEmpty("generic-api-key"),
		Match: conv.PtrEmpty("tok-123"), Tags: []string{},
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Empty(t, out[0].ChatID)
	require.Empty(t, out[0].UserID)
	require.Empty(t, out[0].ExternalUserID)
	require.Nil(t, out[0].FalsePositiveAt)
	require.Equal(t, "secrets", out[0].Category)
}

func TestTransformSurfaceBySource(t *testing.T) {
	t.Parallel()

	// Rows without span attribution stamp the surface from their source alone.
	tf := NewTransformer(testFingerprinter(t))
	cases := []struct {
		source string
		ruleID string
		want   string
	}{
		{source: "gitleaks", ruleID: "generic-api-key", want: surfaceScanSurface},
		{source: "presidio", ruleID: "pii.phone_number", want: surfaceLegacyPresidio},
		{source: "prompt_injection", ruleID: "injection.detected", want: surfaceNone},
		{source: "llm_judge", ruleID: "policy.custom", want: surfaceNone},
		{source: "shadow_mcp", ruleID: "shadow.server", want: surfaceDerived},
		{source: "account_identity", ruleID: "account.personal", want: surfaceDerived},
		{source: "destructive_tool", ruleID: "tool.destructive", want: surfaceDerived},
		{source: "cli_destructive", ruleID: "cli.rm_rf", want: surfaceDerived},
		{source: "custom", ruleID: "custom.pre_cel_rule", want: ""},
	}

	for _, tc := range cases {
		in := SourceRow{
			ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
			ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
			Source: tc.source, Found: true, RuleID: conv.PtrEmpty(tc.ruleID),
			Match: conv.PtrEmpty("some-match-value"), Tags: []string{},
		}
		out, err := tf.Transform(t.Context(), in)
		require.NoError(t, err)
		require.Len(t, out, 1)
		require.Equal(t, tc.want, out[0].Surface, "surface for source %q", tc.source)
		require.Empty(t, out[0].Field, "field stays empty for source %q", tc.source)
		require.Empty(t, out[0].Path, "path stays empty for source %q", tc.source)
		require.Empty(t, out[0].ToolCallID, "tool_call_id stays empty for source %q", tc.source)
	}
}

func TestTransformExplodesSpans(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	pgID := uuid.New()
	excludedAt := time.Now().UTC()
	exclusionID := uuid.New()
	messageCreatedAt := time.Now().UTC().Add(-time.Minute)
	spans := []risk_analysis.FindingSpan{
		{Match: "secret-token-abc", Field: "content", Path: "", StartPos: 10, EndPos: 26},
		{Match: "secret-token-def", Field: "tool.args", Path: "", StartPos: 5, EndPos: 21},
		{Match: "secret-token-ghi", Field: "tool.args", Path: "config.token", StartPos: 0, EndPos: 16},
	}
	in := SourceRow{
		ID: pgID, CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), RiskPolicyVersion: 3,
		ChatMessageID: anchoredTo(uuid.New()),
		Source:        "custom", Found: true, RuleID: conv.PtrEmpty("custom.internal_token"),
		Description: conv.PtrEmpty("internal token"),
		Match:       conv.PtrEmpty("secret-token-abc"),
		StartPos:    conv.PtrEmpty(int32(10)), EndPos: conv.PtrEmpty(int32(26)),
		Tags:       []string{},
		Spans:      mustSpansJSON(t, spans),
		ExcludedAt: &excludedAt, ExclusionID: &exclusionID,
		ChatID: uuid.NewString(), UserID: "user_1", ExternalUserID: "ext_1",
		MessageCreatedAt: messageCreatedAt, AssistantID: "assistant_1",
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, len(spans), "one ClickHouse row per span")

	// Span 0 keeps the Postgres row id; span i >= 1 derives a deterministic id.
	require.Equal(t, pgID, out[0].ID)
	for i := 1; i < len(spans); i++ {
		want := uuid.NewSHA1(uuid.NameSpaceURL, fmt.Appendf(nil, "gram:risk:finding:pgspan:%s:%d", pgID, i))
		require.Equal(t, want, out[i].ID, "span %d id", i)
	}

	wantSurfaces := []string{surfaceContent, surfaceToolArgs, surfaceJSONPath}
	for i, span := range spans {
		row := out[i]
		require.Equal(t, wantSurfaces[i], row.Surface, "span %d surface", i)
		require.Equal(t, span.Field, row.Field, "span %d field", i)
		require.Equal(t, span.Path, row.Path, "span %d path", i)
		require.Empty(t, row.ToolCallID, "span %d: PG spans carried no call id", i)
		require.Equal(t, int32(span.StartPos), row.StartPos, "span %d start", i)
		require.Equal(t, int32(span.EndPos), row.EndPos, "span %d end", i)
		require.Equal(t, uint32(len(span.Match)), row.MatchLen, "span %d match_len", i)
		require.NotEmpty(t, row.FingerprintGlobalHS256, "span %d global fingerprint", i)
		require.NotEmpty(t, row.FingerprintTenantHS256, "span %d tenant fingerprint", i)

		// Shared row-level state fans out to every span row.
		require.Equal(t, in.ChatID, row.ChatID, "span %d chat", i)
		require.Equal(t, "assistant_1", row.AssistantID, "span %d assistant", i)
		require.Equal(t, messageCreatedAt, row.MessageCreatedAt, "span %d message time", i)
		require.NotNil(t, row.ExcludedAt, "span %d excluded_at", i)
		require.Equal(t, exclusionID, *row.ExclusionID, "span %d exclusion", i)
		require.Equal(t, "custom", row.Category, "span %d category", i)
	}

	// Per-span masks over each span's own match: 16 runes -> first 4 + 10 stars + last 2.
	require.Equal(t, "secr**********bc", out[0].MatchRedacted)
	require.Equal(t, "secr**********ef", out[1].MatchRedacted)
	require.Equal(t, "secr**********hi", out[2].MatchRedacted)

	// Distinct matches produce distinct fingerprints.
	require.NotEqual(t, out[0].FingerprintGlobalHS256, out[1].FingerprintGlobalHS256)
	require.NotEqual(t, out[1].FingerprintGlobalHS256, out[2].FingerprintGlobalHS256)
}

func TestTransformSpanRowIDsAreDeterministic(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	pgID := uuid.New()
	spans := mustSpansJSON(t, []risk_analysis.FindingSpan{
		{Match: "value-one", Field: "content", Path: "", StartPos: 0, EndPos: 9},
		{Match: "value-two", Field: "content", Path: "", StartPos: 12, EndPos: 21},
	})
	in := SourceRow{
		ID: pgID, CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "custom", Found: true, RuleID: conv.PtrEmpty("custom.rule"),
		Match: conv.PtrEmpty("value-one"), Tags: []string{}, Spans: spans,
	}

	first, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	second, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)

	require.Equal(t, first[1].ID, second[1].ID, "re-runs must re-emit the same span row ids")
	require.NotEqual(t, first[0].ID, first[1].ID)
}

func TestTransformSingleSpanWithoutFieldKeepsSourceSurface(t *testing.T) {
	t.Parallel()

	// Non-custom scanners have recorded a one-element spans array with no field
	// attribution since the spans column landed; their offsets keep the same
	// per-source semantics as span-less rows.
	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "gitleaks", Found: true, RuleID: conv.PtrEmpty("generic-api-key"),
		Match: conv.PtrEmpty("AKIAIOSFODNN7EXAMPLE"), Tags: []string{},
		Spans: mustSpansJSON(t, []risk_analysis.FindingSpan{
			{Match: "AKIAIOSFODNN7EXAMPLE", Field: "", Path: "", StartPos: 4, EndPos: 24},
		}),
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, in.ID, out[0].ID)
	require.Equal(t, surfaceScanSurface, out[0].Surface)
	require.Empty(t, out[0].Field)
	require.Equal(t, "AKIA**************LE", out[0].MatchRedacted)
	require.Equal(t, int32(4), out[0].StartPos)
	require.Equal(t, int32(24), out[0].EndPos)
}

func TestTransformNullSpansJSONTakesSingleRowPath(t *testing.T) {
	t.Parallel()

	// A JSONB 'null' (as opposed to SQL NULL, which scans to nil) must behave
	// like no spans at all.
	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "presidio", Found: true, RuleID: conv.PtrEmpty("pii.phone_number"),
		Match: conv.PtrEmpty("555-0100"), Tags: []string{},
		Spans: []byte("null"),
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, surfaceLegacyPresidio, out[0].Surface)
}

func TestTransformMalformedSpansJSONErrors(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "custom", Found: true, RuleID: conv.PtrEmpty("custom.rule"),
		Match: conv.PtrEmpty("x"), Tags: []string{},
		Spans: []byte("{not json"),
	}

	_, err := tf.Transform(t.Context(), in)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse spans")
}

func TestTransformIsDeterministic(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_abc",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), RiskPolicyVersion: 1,
		ChatMessageID: anchoredTo(uuid.New()), Source: "gitleaks", Found: true, RuleID: conv.PtrEmpty("secret.token"),
		Match: conv.PtrEmpty("secret-token"), Tags: []string{"secret"},
	}

	first, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	second, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)

	require.Equal(t, first[0].FingerprintGlobalHS256, second[0].FingerprintGlobalHS256)
	require.Equal(t, first[0].FingerprintTenantHS256, second[0].FingerprintTenantHS256)
	require.Equal(t, first[0].MatchRedacted, second[0].MatchRedacted)
}

func TestTransformTenantFingerprintIsOrgScoped(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	base := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), ProjectID: uuid.New(),
		RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()), Source: "gitleaks",
		Found: true, RuleID: conv.PtrEmpty("secret.token"),
		Match: conv.PtrEmpty("same-secret"), Tags: []string{},
	}

	a := base
	a.OrganizationID = "org_a"
	b := base
	b.OrganizationID = "org_b"

	ra, err := tf.Transform(t.Context(), a)
	require.NoError(t, err)
	rb, err := tf.Transform(t.Context(), b)
	require.NoError(t, err)

	// Same secret, same global fingerprint; different org, different tenant one.
	require.Equal(t, ra[0].FingerprintGlobalHS256, rb[0].FingerprintGlobalHS256)
	require.NotEqual(t, ra[0].FingerprintTenantHS256, rb[0].FingerprintTenantHS256)
}

func TestTransformDropsDeadLetterSentinel(t *testing.T) {
	t.Parallel()

	// Dead-letter rows are found=false sentinels; the live path never emits them
	// to ClickHouse, so the transform drops them.
	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "presidio", Found: false, DeadLetterReason: conv.PtrEmpty("could not analyze"),
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestTransformDropsNonFinding(t *testing.T) {
	t.Parallel()

	// found=true but no rule_id, and found=false with a rule_id, are both dropped.
	tf := NewTransformer(testFingerprinter(t))
	noRule := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "none", Found: true, RuleID: nil,
	}
	notFound := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "none", Found: false, RuleID: conv.PtrEmpty("pii.email_address"),
	}

	outNoRule, err := tf.Transform(t.Context(), noRule)
	require.NoError(t, err)
	require.Empty(t, outNoRule)

	outNotFound, err := tf.Transform(t.Context(), notFound)
	require.NoError(t, err)
	require.Empty(t, outNotFound)
}

func TestTransformMapsExclusion(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	excludedAt := time.Now().UTC()
	exclusionID := uuid.New()
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "presidio", Found: true, RuleID: conv.PtrEmpty("pii.email_address"),
		Match: conv.PtrEmpty("x"), ExcludedAt: &excludedAt, ExclusionID: &exclusionID,
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.NotNil(t, out[0].ExcludedAt)
	require.Equal(t, excludedAt, *out[0].ExcludedAt)
	require.NotNil(t, out[0].ExclusionID)
	require.Equal(t, exclusionID, *out[0].ExclusionID)
}

func TestTransformNilTagsBecomeEmptySlice(t *testing.T) {
	t.Parallel()

	tf := NewTransformer(testFingerprinter(t))
	in := SourceRow{
		ID: uuid.New(), CreatedAt: time.Now().UTC(), OrganizationID: "org_123",
		ProjectID: uuid.New(), RiskPolicyID: uuid.New(), ChatMessageID: anchoredTo(uuid.New()),
		Source: "presidio", Found: true, RuleID: conv.PtrEmpty("pii.email_address"),
		Match: conv.PtrEmpty("x"), Tags: nil,
	}

	out, err := tf.Transform(t.Context(), in)
	require.NoError(t, err)
	require.NotNil(t, out[0].Tags)
	require.Empty(t, out[0].Tags)
}
