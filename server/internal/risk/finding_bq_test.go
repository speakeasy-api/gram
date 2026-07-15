package risk_test

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/bq"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// fakeInserter captures the rows handed to Put so tests can inspect the
// BigQuery payload the writer produced. Put returns err to let tests exercise
// the shadow-mode error path.
type fakeInserter struct {
	mu    sync.Mutex
	opts  bq.InserterOptions
	calls int
	items any
	err   error
}

func (i *fakeInserter) Put(ctx context.Context, items any) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
	i.items = items
	return i.err
}

// fakeTableHandle is a bq.TableHandle that always returns the same inserter and
// records the options it was constructed with.
type fakeTableHandle struct {
	inserter *fakeInserter
}

func (h *fakeTableHandle) Inserter(opts bq.InserterOptions) bq.Inserter {
	h.inserter.opts = opts
	return h.inserter
}

func newWriter(t *testing.T, features feature.Provider) (*risk.FindingBQWriter, *fakeInserter) {
	t.Helper()
	w, ins, _ := newWriterWithMeter(t, features, testenv.NewMeterProvider(t))
	return w, ins
}

// testPepperVersion / testPepperKey are the raw keyring material backing the
// Fingerprinter the writer uses in these tests. Expected fingerprints are
// recomputed from this same material via the wantHMAC / wantTenantedHMAC
// helpers in fingerprint_test.go (same package).
const testPepperVersion = "v1"

var testPepperKey = []byte("finding-bq-test-pepper-key-material")

func newWriterWithMeter(t *testing.T, features feature.Provider, mp metric.MeterProvider) (*risk.FindingBQWriter, *fakeInserter, *fakeTableHandle) {
	t.Helper()
	ins := &fakeInserter{}
	table := &fakeTableHandle{inserter: ins}
	// HandleBatch now fingerprints matches, so the writer needs a usable
	// keyring rather than a zero-value Fingerprinter.
	fp, err := risk.ParsePepperKeyRing(keyRingJSON(t, testPepperVersion, map[string][]byte{testPepperVersion: testPepperKey}))
	require.NoError(t, err)
	w := risk.NewFindingBQWriter(testenv.NewLogger(t), mp, table, features, fp)
	return w, ins, table
}

// rows asserts that Put was called and returns the captured rows. The cast
// works because FindingBQRow is exported, so []risk.FindingBQRow is identical
// to the slice HandleBatch built.
func rows(t *testing.T, ins *fakeInserter) []risk.FindingBQRow {
	t.Helper()
	require.NotNil(t, ins.items, "Put was never called with items")
	out, ok := ins.items.([]risk.FindingBQRow)
	require.Truef(t, ok, "items is %T, not []risk.FindingBQRow", ins.items)
	return out
}

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
	}.Build()
}

func TestFindingBQWriter_HandleBatch_MapsAllFields(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))

	require.Equal(t, 1, ins.calls)
	all := rows(t, ins)
	require.Len(t, all, 1)
	row := all[0]

	require.Equal(t, bigquery.NullString{StringVal: "finding-1", Valid: true}, row.ID)
	require.Equal(t, bigquery.NullString{StringVal: "req-1", Valid: true}, row.RequestID)
	require.Equal(t, bigquery.NullString{StringVal: "chat-1", Valid: true}, row.ChatMessageID)
	require.Equal(t, bigquery.NullString{StringVal: "proj-1", Valid: true}, row.ProjectID)
	require.Equal(t, bigquery.NullString{StringVal: "org-1", Valid: true}, row.OrganizationID)
	require.Equal(t, bigquery.NullString{StringVal: "policy-1", Valid: true}, row.RiskPolicyID)
	require.Equal(t, bigquery.NullString{StringVal: "rule-1", Valid: true}, row.RuleID)
	require.Equal(t, bigquery.NullString{StringVal: "a secret leaked", Valid: true}, row.Description)
	require.Equal(t, bigquery.NullString{StringVal: "input", Valid: true}, row.Source)
	require.Equal(t, bigquery.NullString{Valid: false}, row.DeadLetterReason)

	require.Equal(t, bigquery.NullInt64{Int64: 7, Valid: true}, row.RiskPolicyVersion)
	require.Equal(t, bigquery.NullInt64{Int64: 3, Valid: true}, row.StartPos)
	require.Equal(t, bigquery.NullInt64{Int64: 10, Valid: true}, row.EndPos)
	require.Equal(t, bigquery.NullFloat64{Float64: 0.95, Valid: true}, row.Confidence)
	require.Equal(t, []string{"pii", "secret"}, row.Tags)

	wantTS := time.Date(2026, 6, 27, 12, 30, 0, 0, time.UTC)
	require.Equal(t, bigquery.NullTimestamp{Timestamp: wantTS, Valid: true}, row.CreatedAt)
}

func TestFindingBQWriter_HandleBatch_ConfiguresInserter(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))

	// Shadow mode: tolerate schema drift and bad rows rather than fail the batch.
	require.True(t, ins.opts.IgnoreUnknownValues, "unknown values should be ignored")
	require.True(t, ins.opts.SkipInvalidRows, "invalid rows should be skipped in shadow mode")
}

func TestFindingBQWriter_HandleBatch_ComputesFingerprints(t *testing.T) {
	t.Parallel()

	// Flag stays disabled: fingerprints are derived independently of whether the
	// raw match is captured.
	w, ins := newWriter(t, &feature.InMemory{})

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))

	row := rows(t, ins)[0]

	wantGlobal := wantGlobalFingerprint("hunter2")
	wantTenant := wantTenantFingerprint(t, "org-1", "hunter2")

	require.Equal(t, bigquery.NullString{StringVal: wantGlobal, Valid: true}, row.FingerprintGlobalHS256)
	require.Equal(t, bigquery.NullString{StringVal: wantTenant, Valid: true}, row.FingerprintTenantHS256)

	// The global fingerprint omits the tenant, so the two must differ.
	require.NotEqual(t, wantGlobal, wantTenant)
	// Raw match is still suppressed while the flag is off.
	require.Equal(t, bigquery.NullString{Valid: false}, row.Match)
}

func TestFindingBQWriter_HandleBatch_TenantFingerprintRequiresOrg(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	f := finding()
	f.SetOrganizationId("   ") // trims to empty

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := rows(t, ins)[0]

	// Global fingerprint only needs the match.
	require.Equal(t, bigquery.NullString{StringVal: wantGlobalFingerprint("hunter2"), Valid: true}, row.FingerprintGlobalHS256)
	// Without an org id there is no tenant-qualified fingerprint.
	require.Equal(t, bigquery.NullString{Valid: false}, row.FingerprintTenantHS256)
}

func TestFindingBQWriter_HandleBatch_NoMatchYieldsNoFingerprints(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	f := finding()
	f.SetMatch("")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := rows(t, ins)[0]
	require.Equal(t, bigquery.NullString{Valid: false}, row.FingerprintGlobalHS256)
	require.Equal(t, bigquery.NullString{Valid: false}, row.FingerprintTenantHS256)
}

func TestFindingBQWriter_HandleBatch_DeadLetterSuppressesFingerprints(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	f := finding()
	f.SetDeadLetterReason("malformed")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := rows(t, ins)[0]
	require.Equal(t, bigquery.NullString{StringVal: "malformed", Valid: true}, row.DeadLetterReason)
	// A dead-lettered finding carries no usable match, so no fingerprints.
	require.Equal(t, bigquery.NullString{Valid: false}, row.FingerprintGlobalHS256)
	require.Equal(t, bigquery.NullString{Valid: false}, row.FingerprintTenantHS256)
}

func TestFindingBQWriter_HandleBatch_OmitsMatchWhenFlagDisabled(t *testing.T) {
	t.Parallel()

	// Flag defaults to disabled in an empty InMemory provider.
	w, ins := newWriter(t, &feature.InMemory{})

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))

	require.Equal(t, bigquery.NullString{Valid: false}, rows(t, ins)[0].Match, "raw match must not be stored")
}

func TestFindingBQWriter_HandleBatch_CapturesMatchWhenFlagEnabled(t *testing.T) {
	t.Parallel()

	features := &feature.InMemory{}
	features.SetFlag(feature.FlagRiskFindingAnalytics, "org-1", true)

	w, ins := newWriter(t, features)

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))

	require.Equal(t, bigquery.NullString{StringVal: "hunter2", Valid: true}, rows(t, ins)[0].Match)
}

func TestFindingBQWriter_HandleBatch_RecordsMessagesInsertedMetric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inserterErr error
		wantOutcome string
	}{
		{name: "success", inserterErr: nil, wantOutcome: "success"},
		{name: "failure", inserterErr: errors.New("bigquery down"), wantOutcome: "failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := sdkmetric.NewManualReader()
			mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

			w, ins, _ := newWriterWithMeter(t, &feature.InMemory{}, mp)
			ins.err = tt.inserterErr

			// Two findings produce two messages in a single insert.
			require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding(), finding()}, nil))

			point := messagesInsertedPoint(t, reader)
			require.Equal(t, int64(2), point.Value, "counter should track the number of messages submitted")

			outcome, ok := point.Attributes.Value(attr.OutcomeKey)
			require.True(t, ok, "outcome attribute should be present")
			require.Equal(t, tt.wantOutcome, outcome.AsString())
		})
	}
}

func TestFindingBQWriter_HandleBatch_NoInsertRecordsNoMetric(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	w, _, _ := newWriterWithMeter(t, &feature.InMemory{}, mp)

	// An empty batch skips the insert entirely, so no metric is emitted.
	require.NoError(t, w.HandleBatch(context.Background(), nil, nil))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Empty(t, rm.ScopeMetrics, "no insert means no messages_inserted metric")
}

func TestFindingBQWriter_HandleBatch_RecordsSkippedMessagesMetric(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	w, ins, _ := newWriterWithMeter(t, &feature.InMemory{}, mp)

	// One finding has an unparseable timestamp and is skipped; the other is
	// valid and inserted.
	bad := finding()
	bad.SetCreatedAt("not-a-timestamp")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{bad, finding()}, nil))

	require.Len(t, rows(t, ins), 1, "only the valid finding should be inserted")

	point := messagesSkippedPoint(t, reader)
	require.Equal(t, int64(1), point.Value, "one message should be counted as skipped")

	reason, ok := point.Attributes.Value(attr.ReasonKey)
	require.True(t, ok, "reason attribute should be present")
	require.Equal(t, "invalid_timestamp", reason.AsString())
}

func TestFindingBQWriter_HandleBatch_FlagScopedPerOrganization(t *testing.T) {
	t.Parallel()

	// Flag enabled for org-1 only; the batch carries findings from two orgs.
	features := &feature.InMemory{}
	features.SetFlag(feature.FlagRiskFindingAnalytics, "org-1", true)

	w, ins := newWriter(t, features)

	other := finding()
	other.SetOrganizationId("org-2")
	other.SetMatch("other-secret")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding(), other}, nil))

	all := rows(t, ins)
	require.Len(t, all, 2)

	require.Equal(t, bigquery.NullString{StringVal: "hunter2", Valid: true}, all[0].Match,
		"opted-in org should capture match")
	require.Equal(t, bigquery.NullString{Valid: false}, all[1].Match,
		"org without the flag should not capture match")
}

func TestFindingBQWriter_HandleBatch_EmptyOrgSkipsFlagCheck(t *testing.T) {
	t.Parallel()

	// A panicking provider proves the flag is never consulted for a blank org id.
	w, ins := newWriter(t, panicProvider{})

	f := finding()
	f.SetOrganizationId("   ") // whitespace trims to empty

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	require.Equal(t, bigquery.NullString{Valid: false}, rows(t, ins)[0].Match)
}

func TestFindingBQWriter_HandleBatch_FlagErrorOmitsMatch(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, errProvider{err: errors.New("posthog down")})

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil),
		"a feature-flag error must not fail the batch")

	require.Equal(t, bigquery.NullString{Valid: false}, rows(t, ins)[0].Match,
		"on flag error, match must be omitted (fail closed)")
}

func TestFindingBQWriter_HandleBatch_CachesFlagPerOrg(t *testing.T) {
	t.Parallel()

	features := &feature.InMemory{}
	features.SetFlag(feature.FlagRiskFindingAnalytics, "org-1", true)
	counter := &countingProvider{Provider: features}

	w, ins := newWriter(t, counter)

	// Three findings, all from org-1, should trigger exactly one flag lookup.
	require.NoError(t, w.HandleBatch(context.Background(),
		[]*riskv1.Finding{finding(), finding(), finding()}, nil))

	require.Equal(t, 1, counter.calls, "the flag should be resolved once per org and cached")
	for _, row := range rows(t, ins) {
		require.Equal(t, bigquery.NullString{StringVal: "hunter2", Valid: true}, row.Match)
	}
}

func TestFindingBQWriter_HandleBatch_UnsetFieldsAreInvalid(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	// A minimal finding: only the org id and a valid timestamp are set (the
	// timestamp is required for the row to be emitted at all).
	f := riskv1.Finding_builder{
		OrganizationId: new("org-1"),
		CreatedAt:      new("2026-06-27T12:30:00Z"),
	}.Build()

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	row := rows(t, ins)[0]
	require.Equal(t, bigquery.NullString{Valid: false}, row.ID)
	require.Equal(t, bigquery.NullString{Valid: false}, row.RequestID)
	require.Equal(t, bigquery.NullString{Valid: false}, row.RuleID)
	require.Equal(t, bigquery.NullInt64{Valid: false}, row.RiskPolicyVersion)
	require.Equal(t, bigquery.NullInt64{Valid: false}, row.StartPos)
	require.Equal(t, bigquery.NullFloat64{Valid: false}, row.Confidence)
}

func TestFindingBQWriter_HandleBatch_Timestamps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		createdAt *string
		wantRow   bool
		wantValid bool
		wantTime  time.Time
	}{
		{
			name:      "valid rfc3339",
			createdAt: new("2026-01-02T15:04:05Z"),
			wantRow:   true,
			wantValid: true,
			wantTime:  time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
		},
		{
			// An unparseable timestamp causes the finding to be skipped entirely.
			name:      "invalid string skips the finding",
			createdAt: new("not-a-timestamp"),
			wantRow:   false,
		},
		{
			name:      "empty string skips the finding",
			createdAt: new(""),
			wantRow:   false,
		},
		{
			name:      "absent field skips the finding",
			createdAt: nil,
			wantRow:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w, ins := newWriter(t, &feature.InMemory{})

			b := riskv1.Finding_builder{OrganizationId: new("org-1")}
			b.CreatedAt = tt.createdAt
			f := b.Build()

			require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

			if !tt.wantRow {
				// A finding with an unusable timestamp is dropped entirely, and
				// with nothing left to insert no Put is issued.
				require.Zero(t, ins.calls, "a skipped finding should not issue an insert")
				return
			}

			ts := rows(t, ins)[0].CreatedAt
			require.True(t, ts.Valid)
			require.True(t, tt.wantTime.Equal(ts.Timestamp), "want %s got %s", tt.wantTime, ts.Timestamp)
		})
	}
}

func TestFindingBQWriter_HandleBatch_EmptyBatch(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	require.NoError(t, w.HandleBatch(context.Background(), nil, nil))

	require.Zero(t, ins.calls, "an empty batch should not issue an insert")
	require.Nil(t, ins.items, "Put should not be called for an empty batch")
}

func TestFindingBQWriter_HandleBatch_AllSkippedSkipsInsert(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})

	// Every finding has an unusable timestamp, so all are dropped and there is
	// nothing left to insert.
	f := finding()
	f.SetCreatedAt("not-a-timestamp")

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{f}, nil))

	require.Zero(t, ins.calls, "a batch with no surviving rows should not issue an insert")
	require.Nil(t, ins.items, "Put should not be called when all findings are skipped")
}

func TestFindingBQWriter_HandleBatch_InserterErrorIsSwallowed(t *testing.T) {
	t.Parallel()

	w, ins := newWriter(t, &feature.InMemory{})
	ins.err = errors.New("bigquery unavailable")

	// Shadow mode: the writer logs but does not surface insert failures.
	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, nil))
}

func TestFindingBQWriter_HandleBatch_MetadataLengthMismatch(t *testing.T) {
	t.Parallel()

	// metadata is accepted but unused; a length mismatch must not panic.
	w, ins := newWriter(t, &feature.InMemory{})

	require.NoError(t, w.HandleBatch(context.Background(), []*riskv1.Finding{finding()}, []gcp.MessageMetadata{}))
	require.Len(t, rows(t, ins), 1)
}

// messagesInsertedPoint collects metrics and returns the single data point for
// the messages-inserted counter, failing the test if it is missing.
func messagesInsertedPoint(t *testing.T, reader *sdkmetric.ManualReader) metricdata.DataPoint[int64] {
	t.Helper()

	const metricName = "gram.risk_findings.bq_messages_inserted"

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

// messagesSkippedPoint collects metrics and returns the single data point for
// the messages-skipped counter, failing the test if it is missing.
func messagesSkippedPoint(t *testing.T, reader *sdkmetric.ManualReader) metricdata.DataPoint[int64] {
	t.Helper()

	const metricName = "gram.risk_findings.bq_messages_skipped"

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

// panicProvider fails the test if IsFlagEnabled is ever called.
type panicProvider struct{}

func (panicProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	panic("IsFlagEnabled should not be called")
}

func (panicProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	panic("IsFlagEnabledLocal should not be called")
}

func (panicProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	panic("FlagPayload should not be called")
}

// errProvider always returns an error from IsFlagEnabled.
type errProvider struct{ err error }

func (p errProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, p.err
}

func (p errProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, p.err
}

func (p errProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, p.err
}

// countingProvider records how many times the flag was resolved so tests can
// assert the per-org cache works.
type countingProvider struct {
	feature.Provider
	mu    sync.Mutex
	calls int
}

func (c *countingProvider) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	//nolint:wrapcheck // test passthrough to the wrapped provider
	return c.Provider.IsFlagEnabled(ctx, flag, distinctID, groups)
}
