package risk_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/captureredact"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

// This fixed synthetic value exercises GitHub's provider format; it is not an
// issued credential and avoids the scanner's known example-token allowlists.
const captureSyntheticPAT = "ghp_b847c2d93a5e4f6071cb9368d014a2e75f69"

func newCaptureInspector(t *testing.T, ti *testInstance) *risk.CaptureInspector {
	t.Helper()
	scanner := newScannerWithDispatcher(t, ti, nil, ti.flags, nil)
	t.Cleanup(func() { _ = scanner.Shutdown(context.WithoutCancel(t.Context())) })
	fp, err := risk.ParsePepperKeyRing(keyRingJSON(t, testPepperVersion, map[string][]byte{testPepperVersion: testPepperKey}))
	require.NoError(t, err)
	return risk.NewCaptureInspector(scanner, risk.NewSafeFindingPreparer(ti.conn, fp, testPresetLibrary(t)))
}

func captureInput(t *testing.T, ctx context.Context) chat.CaptureInput {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return chat.CaptureInput{
		OrgID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		ChatID: uuid.New(), MessageID: uuid.New(), ContentPartID: uuid.Nil,
		UserID: authCtx.UserID, Source: "codex", Role: "user",
		Content:   "結果\nMISE_GITHUB_TOKEN=" + captureSyntheticPAT,
		ToolCalls: nil, CreatedAt: time.Now().UTC(),
	}
}

func createCapturePolicy(t *testing.T, ctx context.Context, ti *testInstance, sources, disabled []string, config []byte) uuid.UUID {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	id := uuid.New()
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID: id, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		Name: "capture findings", Sources: sources, DisabledRules: disabled,
		AnalyzerConfig: config, Enabled: true, Action: "flag", AudienceType: "everyone",
	})
	require.NoError(t, err)
	return id
}

func TestCaptureInspectorPreservesSafeFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	policyID := createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	before := input.Content
	inspector := newCaptureInspector(t, ti)

	findings, err := inspector.InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	finding := findings[0]
	require.Equal(t, before, input.Content, "inspection must not mutate execution input")
	require.Equal(t, "gitleaks", finding.GetSource())
	require.Equal(t, "secret.github_pat", finding.GetRuleId())
	require.Equal(t, policyID.String(), finding.GetRiskPolicyId())
	require.Equal(t, input.MessageID.String(), finding.GetChatMessageId())
	require.False(t, finding.HasMatch())
	require.True(t, finding.GetSafeMatch().GetCaptureRedacted())
	require.Contains(t, finding.GetTags(), "capture_redaction")
	require.Contains(t, finding.GetDescription(), "Credential removed from captured content")
	require.Equal(t, uint32(len(captureSyntheticPAT)), finding.GetSafeMatch().GetMatchLen())
	require.Equal(t, wantGlobalFingerprint(captureSyntheticPAT), finding.GetSafeMatch().GetFingerprintGlobalHs256())
	require.Equal(t, wantTenantFingerprint(t, input.OrgID, captureSyntheticPAT), finding.GetSafeMatch().GetFingerprintTenantHs256())
	encoded, err := protojson.Marshal(finding)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), captureSyntheticPAT)

	// The event retains the async request and finding identity, even though
	// the raw matched value is no longer present in the published payload.
	raw, err := gitleaks.NewScanner().Scan(ctx, input.Content)
	require.NoError(t, err)
	var selected []scanners.Finding
	for _, detected := range raw.Findings {
		if captureredact.RedactsRule(detected.RuleID) {
			selected = append(selected, detected)
		}
	}
	meta := scanners.FindingMetadata{
		RequestID: finding.GetRequestId(), ChatMessageID: input.MessageID.String(),
		ContentPartID: "", ProjectID: input.ProjectID.String(), OrganizationID: input.OrgID,
		RiskPolicyID: policyID.String(), RiskPolicyVersion: finding.GetRiskPolicyVersion(), Shadow: false,
	}
	require.Equal(t, scanners.FindingIDs(meta, selected)[0].String(), finding.GetId())
	again, err := inspector.InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Equal(t, finding.GetId(), again[0].GetId(), "redelivery must retain finding identity")
}

func TestCaptureInspectorUsesExistingCategoryScope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	input.Role = "assistant"

	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Empty(t, findings, "recommended scope excludes assistant free text")
}

func TestCaptureInspectorPreservesDisabledRuleFilter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, []string{"secret.github_pat"}, nil)

	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, captureInput(t, ctx))
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestCaptureInspectorPreservesRegexExclusion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	policyID := createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	exclusion, err := riskrepo.New(ti.conn).CreateRiskExclusion(ctx, riskrepo.CreateRiskExclusionParams{
		ProjectID: input.ProjectID, OrganizationID: input.OrgID,
		RiskPolicyID: uuid.NullUUID{UUID: policyID, Valid: true}, MatchType: "regex",
		MatchValue: "^ghp_", SourceFilter: pgtype.Text{String: "gitleaks", Valid: true}, Enabled: true,
	})
	require.NoError(t, err)

	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Len(t, findings, 1, "excluded findings remain auditable")
	require.Equal(t, exclusion.ID.String(), findings[0].GetSafeMatch().GetExclusionId())
	require.Equal(t, "rule", findings[0].GetSafeMatch().GetExcludedReason())
	require.NotEmpty(t, findings[0].GetSafeMatch().GetExcludedAt())
	require.False(t, findings[0].HasMatch())
}

func TestCaptureInspectorScansFullToolArgumentSurface(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	input.Role = "assistant"
	input.Content = "execute"
	arguments := strings.Repeat("x", 60*1024) + "\nMISE_GITHUB_TOKEN=" + captureSyntheticPAT
	input.ToolCalls, _ = json.Marshal([]map[string]any{{"id": "tool-1", "function": map[string]any{"name": "shell", "arguments": arguments}}})
	before := string(input.ToolCalls)

	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	finding := findings[0]
	require.Greater(t, finding.GetStartPos(), int32(50*1024))
	require.Equal(t, len(captureSyntheticPAT), int(finding.GetEndPos()-finding.GetStartPos()))
	require.Equal(t, before, string(input.ToolCalls))
}

func TestCaptureInspectorRedactionEvidenceIsIndependentOfLLMMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	ti.flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, input.OrgID, feature.VariantRiskLLMLLM)

	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.True(t, findings[0].GetSafeMatch().GetCaptureRedacted())
	require.Equal(t, "gitleaks", findings[0].GetSource(), "redaction evidence is not an LLM verdict")
}

func TestCaptureInspectorSkipsUnselectedSecretPolicy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourcePresidio}, nil, nil)

	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, captureInput(t, ctx))
	require.NoError(t, err)
	require.Empty(t, findings)
}

func TestCaptureInspectorPreservesEscapedCredentialEvidence(t *testing.T) {
	t.Parallel()
	for _, toolArguments := range []bool{false, true} {
		name := "message"
		if toolArguments {
			name = "nested tool arguments"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestRiskService(t)
			createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
			input := captureInput(t, ctx)
			escaped := `{"token":"\u0067hp_` + strings.TrimPrefix(captureSyntheticPAT, "ghp_") + `"}`
			if toolArguments {
				inner, err := json.Marshal(map[string]string{"nested": escaped})
				require.NoError(t, err)
				input.Role, input.Content = "assistant", "execute"
				input.ToolCalls, err = json.Marshal([]map[string]any{{"id": "encoded-call", "function": map[string]any{"name": "shell", "arguments": string(inner)}}})
				require.NoError(t, err)
			} else {
				input.Content = escaped
			}
			findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
			require.NoError(t, err)
			require.Len(t, findings, 1)
			finding := findings[0]
			require.False(t, finding.HasMatch())
			require.Equal(t, captureredact.DecodedJSONField, finding.GetField(), "decoded matches need the separate field marker")
			require.Equal(t, scanners.SurfaceNone, finding.GetSurface())
			require.Contains(t, finding.GetPath(), "$strings[")
			require.Equal(t, wantGlobalFingerprint(captureSyntheticPAT), finding.GetSafeMatch().GetFingerprintGlobalHs256())
			require.Equal(t, wantTenantFingerprint(t, input.OrgID, captureSyntheticPAT), finding.GetSafeMatch().GetFingerprintTenantHs256())
			encoded, err := protojson.Marshal(finding)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), strings.TrimPrefix(captureSyntheticPAT, "ghp_"))
		})
	}
}

func TestCaptureInspectorCountsLiteralAndEscapedCredentialSeparately(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	input.Content = `{"literal":"` + captureSyntheticPAT + `","escaped":"\u0067hp_` + strings.TrimPrefix(captureSyntheticPAT, "ghp_") + `"}`
	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Len(t, findings, 2)
	require.NotEqual(t, findings[0].GetId(), findings[1].GetId())
	for _, finding := range findings {
		require.False(t, finding.HasMatch())
		require.Equal(t, wantGlobalFingerprint(captureSyntheticPAT), finding.GetSafeMatch().GetFingerprintGlobalHs256())
	}
}

func TestCaptureInspectorPreservesLiteralJSONFindingIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	createCapturePolicy(t, ctx, ti, []string{ra.SourceGitleaks}, nil, nil)
	input := captureInput(t, ctx)
	input.Content = `{"prefix":"\uD83D\uDC15\n","token":"` + captureSyntheticPAT + `"}`
	findings, err := newCaptureInspector(t, ti).InspectCapture(ctx, input)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	finding := findings[0]
	require.Empty(t, finding.GetField())
	require.Equal(t, scanners.SurfaceContent, finding.GetSurface())
	raw, err := gitleaks.NewScanner().Scan(ctx, input.Content)
	require.NoError(t, err)
	selected := slices.DeleteFunc(raw.Findings, func(f scanners.Finding) bool { return !captureredact.RedactsRule(f.RuleID) })
	meta := scanners.FindingMetadata{
		RequestID: finding.GetRequestId(), ChatMessageID: input.MessageID.String(), ContentPartID: "",
		ProjectID: input.ProjectID.String(), OrganizationID: input.OrgID, RiskPolicyID: finding.GetRiskPolicyId(),
		RiskPolicyVersion: finding.GetRiskPolicyVersion(), Shadow: false,
	}
	require.Equal(t, scanners.FindingIDs(meta, selected)[0].String(), finding.GetId())
}
