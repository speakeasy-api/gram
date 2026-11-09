package risk

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type safeFindingInserter struct {
	rows []chrepo.RiskFindingRow
}

func (s *safeFindingInserter) InsertRiskFindings(_ context.Context, rows []chrepo.RiskFindingRow) error {
	s.rows = append(s.rows, rows...)
	return nil
}

func safeFindingFixture(t *testing.T) (*SafeFindingPreparer, *riskv1.Finding) {
	t.Helper()
	projectID, policyID := uuid.New(), uuid.New()
	now := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	fp := Fingerprinter{currentVersion: "test", keys: map[string][]byte{"test": []byte("synthetic-finding-pepper-material")}}
	lib, err := presetlib.New()
	require.NoError(t, err)
	preparer := NewSafeFindingPreparer(&missingPolicyDB{}, fp, lib)
	preparer.now = func() time.Time { return now }
	preparer.policyFilters.cache.Add(findingPolicyFilterKey{projectID: projectID, policyID: policyID}, emptyFindingPolicyFilters())
	preparer.exclusions.cache.Add(findingExclusionKey{projectID: projectID, policyID: policyID}, risk_analysis.NewExclusionSet(nil))
	finding := riskv1.Finding_builder{
		Id:                new(uuid.NewString()),
		RequestId:         new("capture-request"),
		ProjectId:         new(projectID.String()),
		OrganizationId:    new("org_synthetic_capture"),
		RiskPolicyId:      new(policyID.String()),
		RiskPolicyVersion: new(int64(3)),
		CreatedAt:         new(now.Format(time.RFC3339)),
		RuleId:            new("secret.github_pat"),
		Description:       new("A credential was captured"),
		Match:             new("synthetic-captured-credential-value"),
		StartPos:          new(int32(6)),
		EndPos:            new(int32(41)),
		Source:            new("gitleaks"),
		Confidence:        new(1.0),
		Tags:              []string{"secret"},
		EventKind:         new(chrepo.EventKindFinding),
	}.Build()
	return preparer, finding
}

func safeFixtureWriter(t *testing.T, preparer *SafeFindingPreparer, useRawDependencies bool) (*FindingCHWriter, *safeFindingInserter) {
	t.Helper()
	inserter := &safeFindingInserter{}
	fp := Fingerprinter{}
	if useRawDependencies {
		fp = preparer.fingerprinter
	}
	writer := NewFindingCHWriter(testenv.NewLogger(t), nil, testenv.NewMeterProvider(t), inserter, fp, preparer.builtinPresets, nil)
	if useRawDependencies {
		writer.policyFilters, writer.exclusions = preparer.policyFilters, preparer.exclusions
	}
	return writer, inserter
}

func TestSafeFindingMatchesRawClickHouseRow(t *testing.T) {
	t.Parallel()
	preparer, finding := safeFindingFixture(t)
	raw := finding.GetMatch()
	writer, rawRows := safeFixtureWriter(t, preparer, true)
	failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
	require.NoError(t, err)
	require.NoError(t, failed[0])
	require.Len(t, rawRows.rows, 1)

	require.NoError(t, preparer.Prepare(t.Context(), finding))
	require.False(t, finding.HasMatch())
	require.NoError(t, ValidateSafeFinding(finding))
	encoded, err := proto.Marshal(finding)
	require.NoError(t, err)
	require.False(t, bytes.Contains(encoded, []byte(raw)), "the broker payload must not retain the raw match")

	writer, safeRows := safeFixtureWriter(t, preparer, false)
	failed, err = writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
	require.NoError(t, err)
	require.NoError(t, failed[0])
	require.Equal(t, rawRows.rows, safeRows.rows, "safe metadata must preserve every persisted finding field")
	prepared := proto.Clone(finding)
	require.NoError(t, preparer.Prepare(t.Context(), finding))
	require.True(t, proto.Equal(prepared, finding), "preparation is idempotent")
}

func TestSafeFindingPreservesExactAndRegexExclusions(t *testing.T) {
	t.Parallel()
	for _, matchType := range []string{"exact", "regex"} {
		t.Run(matchType, func(t *testing.T) {
			t.Parallel()
			preparer, finding := safeFindingFixture(t)
			value := finding.GetMatch()
			if matchType == "regex" {
				value = "^" + value + "$"
			}
			projectID, policyID := uuid.MustParse(finding.GetProjectId()), uuid.MustParse(finding.GetRiskPolicyId())
			exclusionID := uuid.New()
			preparer.exclusions.cache.Add(findingExclusionKey{projectID: projectID, policyID: policyID}, risk_analysis.NewExclusionSet([]repo.RiskExclusion{{
				ID: exclusionID, MatchType: matchType, MatchValue: value,
				SourceFilter: pgtype.Text{String: "gitleaks", Valid: true},
			}}))
			require.NoError(t, preparer.Prepare(t.Context(), finding))
			resolver := NewFindingExclusionResolver(nil)
			gotID, excluded, err := resolver.ExcludedBy(t.Context(), finding)
			require.NoError(t, err)
			require.True(t, excluded)
			require.Equal(t, exclusionID, gotID)
			writer, rows := safeFixtureWriter(t, preparer, false)
			failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
			require.NoError(t, err)
			require.NoError(t, failed[0])
			require.Len(t, rows.rows, 1)
			require.Equal(t, exclusionID, *rows.rows[0].ExclusionID)
			require.Equal(t, chrepo.ExcludedReasonRule, rows.rows[0].ExcludedReason)
			require.Equal(t, preparer.now(), *rows.rows[0].ExcludedAt)
		})
	}
}

func TestSafeFindingDoesNotMatchRegexAgainstAbsentRawValue(t *testing.T) {
	t.Parallel()
	preparer, finding := safeFindingFixture(t)
	projectID, policyID := uuid.MustParse(finding.GetProjectId()), uuid.MustParse(finding.GetRiskPolicyId())
	preparer.exclusions.cache.Add(findingExclusionKey{projectID: projectID, policyID: policyID}, risk_analysis.NewExclusionSet([]repo.RiskExclusion{{ID: uuid.New(), MatchType: "regex", MatchValue: "^$"}}))
	require.NoError(t, preparer.Prepare(t.Context(), finding))
	_, excluded, err := preparer.exclusions.ExcludedBy(t.Context(), finding)
	require.NoError(t, err)
	require.False(t, excluded)
	writer, rows := safeFixtureWriter(t, preparer, false)
	failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
	require.NoError(t, err)
	require.NoError(t, failed[0])
	require.Len(t, rows.rows, 1)
	require.Nil(t, rows.rows[0].ExcludedAt)
}

func TestSafeFindingPreservesDisabledAndBuiltinFiltering(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"disabled_rule", "builtin_preset"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			preparer, finding := safeFindingFixture(t)
			filters := emptyFindingPolicyFilters()
			if reason == "disabled_rule" {
				filters.disabledRules = risk_analysis.NewDisabledRuleSet([]string{finding.GetRuleId()})
			} else {
				filters.builtinPresetsEnabled = true
				finding.SetRuleId("secret.stripe_secret_key")
				finding.SetMatch("sk_test_4eC39HqLyjWDarjtT1zdp7dc")
			}
			preparer.policyFilters.cache.Add(findingPolicyFilterKey{projectID: uuid.MustParse(finding.GetProjectId()), policyID: uuid.MustParse(finding.GetRiskPolicyId())}, filters)
			writer, rawRows := safeFixtureWriter(t, preparer, true)
			failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
			require.NoError(t, err)
			require.NoError(t, failed[0])
			require.Empty(t, rawRows.rows)
			require.NoError(t, preparer.Prepare(t.Context(), finding))
			require.NoError(t, ValidateSafeFinding(finding))
			writer, safeRows := safeFixtureWriter(t, preparer, false)
			failed, err = writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
			require.NoError(t, err)
			require.NoError(t, failed[0])
			require.Empty(t, safeRows.rows)
		})
	}
}

func TestSafeFindingRejectsConflictingAndMalformedMetadata(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*riskv1.Finding)
	}{
		{name: "raw match alongside safe metadata", mutate: func(f *riskv1.Finding) { f.SetMatch("unexpected raw match") }},
		{name: "empty raw field alongside safe metadata", mutate: func(f *riskv1.Finding) { f.SetMatch("") }},
		{name: "unknown version", mutate: func(f *riskv1.Finding) { f.GetSafeMatch().SetVersion(2) }},
		{name: "missing preparation time", mutate: func(f *riskv1.Finding) { f.GetSafeMatch().ClearPreparedAt() }},
		{name: "invalid global fingerprint", mutate: func(f *riskv1.Finding) { f.GetSafeMatch().SetFingerprintGlobalHs256("invalid") }},
		{name: "missing tenant fingerprint", mutate: func(f *riskv1.Finding) { f.GetSafeMatch().ClearFingerprintTenantHs256() }},
		{name: "invalid exclusion id", mutate: func(f *riskv1.Finding) {
			f.GetSafeMatch().SetExcludedAt(f.GetCreatedAt())
			f.GetSafeMatch().SetExclusionId("invalid")
		}},
		{name: "exclusion without timestamp", mutate: func(f *riskv1.Finding) { f.GetSafeMatch().SetExclusionId(uuid.NewString()) }},
		{name: "capture provenance missing tag", mutate: func(f *riskv1.Finding) { f.GetSafeMatch().SetCaptureRedacted(true) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			preparer, finding := safeFindingFixture(t)
			require.NoError(t, preparer.Prepare(t.Context(), finding))
			tc.mutate(finding)
			require.Error(t, ValidateSafeFinding(finding))
			writer, rows := safeFixtureWriter(t, preparer, false)
			failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
			require.NoError(t, err)
			require.Error(t, failed[0])
			require.Empty(t, rows.rows)
			_, _, err = NewFindingExclusionResolver(nil).ExcludedBy(t.Context(), finding)
			require.Error(t, err)
		})
	}
}

func TestSafeFindingPreparationFailureKeepsRawInputUnchanged(t *testing.T) {
	t.Parallel()
	preparer, finding := safeFindingFixture(t)
	before := proto.Clone(finding)
	preparer.fingerprinter = Fingerprinter{}
	require.Error(t, preparer.Prepare(t.Context(), finding))
	require.True(t, proto.Equal(before, finding))
	preparer, finding = safeFindingFixture(t)
	before = proto.Clone(finding)
	preparer.exclusions = NewFindingExclusionResolver(nil)
	require.Error(t, preparer.Prepare(t.Context(), finding))
	require.True(t, proto.Equal(before, finding))
}

func TestSafeFindingCarriesCaptureProvenance(t *testing.T) {
	t.Parallel()
	preparer, finding := safeFindingFixture(t)
	finding.SetTags(append(finding.GetTags(), CaptureRedactionTag))
	require.NoError(t, preparer.Prepare(t.Context(), finding))
	finding.GetSafeMatch().SetCaptureRedacted(true)
	require.NoError(t, ValidateSafeFinding(finding))
	writer, rows := safeFixtureWriter(t, preparer, false)
	failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
	require.NoError(t, err)
	require.NoError(t, failed[0])
	require.Contains(t, rows.rows[0].Tags, CaptureRedactionTag)
	require.Equal(t, "gitleaks", rows.rows[0].Source)
}

func TestSafeFindingPreservesDeadLettersAndManualState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*riskv1.Finding)
	}{
		{name: "dead letter", mutate: func(f *riskv1.Finding) { f.SetDeadLetterReason("detector_unavailable") }},
		{name: "empty match", mutate: func(f *riskv1.Finding) { f.ClearMatch() }},
		{name: "global fingerprint without organization", mutate: func(f *riskv1.Finding) { f.ClearOrganizationId() }},
		{name: "manual suppression", mutate: func(f *riskv1.Finding) {
			f.SetEventKind(chrepo.EventKindSuppression)
			f.SetExcludedAt(f.GetCreatedAt())
			f.SetExcludedReason(chrepo.ExcludedReasonManual)
			f.SetExcludedDetail("reviewer dismissed this finding")
		}},
		{name: "unsuppression", mutate: func(f *riskv1.Finding) { f.SetEventKind(chrepo.EventKindUnsuppression) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			preparer, finding := safeFindingFixture(t)
			tc.mutate(finding)
			writer, rawRows := safeFixtureWriter(t, preparer, true)
			failed, err := writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
			require.NoError(t, err)
			require.NoError(t, failed[0])
			require.Len(t, rawRows.rows, 1)
			require.NoError(t, preparer.Prepare(t.Context(), finding))
			writer, safeRows := safeFixtureWriter(t, preparer, false)
			failed, err = writer.ProcessBatch(t.Context(), []*riskv1.Finding{finding})
			require.NoError(t, err)
			require.NoError(t, failed[0])
			require.Equal(t, rawRows.rows, safeRows.rows)
		})
	}
}
