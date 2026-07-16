package risk_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

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

// wantRedacted mirrors fingerprintRedactedMatch: `<redacted len=N sha=XXXXXXXX>`
// over sha256(orgID \x00 match).
func wantRedacted(orgID, match string) string {
	buf := append([]byte(orgID), 0x00)
	buf = append(buf, match...)
	sum := sha256.Sum256(buf)
	return fmt.Sprintf("<redacted len=%d sha=%s>", len(match), hex.EncodeToString(sum[:4]))
}

func TestFindingCHWriter_HandleBatch_MapsAllFields(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))

	require.Equal(t, 1, ins.calls)
	all := chRows(t, ins)
	require.Len(t, all, 1)
	row := all[0]

	require.Equal(t, "finding-1", row.ID)
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

	// Hashes-only: the raw match is never stored, only its length + redaction.
	require.Equal(t, uint32(len("hunter2")), row.MatchLen)
	require.Equal(t, wantRedacted("org-1", "hunter2"), row.MatchRedacted)
	require.NotContains(t, row.MatchRedacted, "hunter2")
	require.Equal(t, wantGlobalFingerprint("hunter2"), row.FingerprintGlobalHS256)
	require.Equal(t, wantTenantFingerprint(t, "org-1", "hunter2"), row.FingerprintTenantHS256)
	require.Equal(t, testPepperVersion, row.FingerprintPepperVersion)
}

// The security-critical case: shadow_mcp and account_identity findings pass
// their match through verbatim on the dashboard, but must be REDACTED in
// ClickHouse so no server URL or account email (PII) is stored at rest.
func TestFindingCHWriter_HandleBatch_RedactsEverySourceNoPassthrough(t *testing.T) {
	t.Parallel()

	for _, source := range []string{shadowmcp.SourceShadowMCP, ra.SourceAccountIdentity} {
		w, ins := newCHWriter(t)

		f := finding()
		f.SetSource(source)
		f.SetMatch("user@example.com")

		require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

		row := chRows(t, ins)[0]
		require.Equal(t, wantRedacted("org-1", "user@example.com"), row.MatchRedacted,
			"source %q must be redacted in ClickHouse, not passed through verbatim", source)
		require.NotContains(t, row.MatchRedacted, "user@example.com",
			"source %q leaked plaintext into ClickHouse", source)
	}
}

func TestFindingCHWriter_HandleBatch_NoMatchYieldsNoFingerprintsOrRedaction(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	f := finding()
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

	f := finding()
	f.SetDeadLetterReason("malformed")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := chRows(t, ins)[0]
	require.Equal(t, "malformed", row.DeadLetterReason)
	require.Empty(t, row.FingerprintGlobalHS256)
	require.Empty(t, row.FingerprintTenantHS256)
	require.Empty(t, row.MatchRedacted)
	require.Equal(t, uint32(0), row.MatchLen)
}

func TestFindingCHWriter_HandleBatch_TenantFingerprintRequiresOrg(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	f := finding()
	f.SetOrganizationId("   ") // trims to empty

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := chRows(t, ins)[0]
	require.Equal(t, wantGlobalFingerprint("hunter2"), row.FingerprintGlobalHS256)
	require.Empty(t, row.FingerprintTenantHS256, "no org id means no tenant-qualified fingerprint")
}

func TestFindingCHWriter_HandleBatch_InvalidTimestampSkipsFinding(t *testing.T) {
	t.Parallel()

	w, ins := newCHWriter(t)

	bad := finding()
	bad.SetCreatedAt("not-a-timestamp")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{bad, finding()}, nil))

	require.Len(t, chRows(t, ins), 1, "only the finding with a valid timestamp is inserted")
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
	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))
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

		require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding(), finding()}, nil))

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
func TestFindingCHWriter_HandleBatch_DropsExcludedFindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// Global exact exclusion suppressing the secret "hunter2" for this project.
	_, err := riskrepo.New(ti.conn).CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
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
	excluded := finding()
	excluded.SetProjectId(authCtx.ProjectID.String())
	excluded.SetRiskPolicyId(policyID)
	excluded.SetMatch("hunter2")

	// Kept: a different value the exclusion does not cover.
	kept := finding()
	kept.SetProjectId(authCtx.ProjectID.String())
	kept.SetRiskPolicyId(policyID)
	kept.SetMatch("different-secret")

	require.NoError(t, w.HandleBatch(ctx, []*riskv1.Finding{excluded, kept}, nil))

	rows := chRows(t, ins)
	require.Len(t, rows, 1, "the excluded finding must be dropped, the other kept")
	require.Equal(t, wantRedacted("org-1", "different-secret"), rows[0].MatchRedacted)
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
