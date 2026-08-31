package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// withUniqueScanOrg rewrites the auth context onto a per-test organization id.
// The test ClickHouse database is shared across the package's parallel tests
// while the mock auth context always carries the same org id, so org-scoped
// scan reads would otherwise observe sibling tests' writes.
func withUniqueScanOrg(t *testing.T, ctx context.Context) (context.Context, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	clone := *authCtx
	clone.ActiveOrganizationID = "scan-test-org-" + uuid.NewString()
	return contextvalues.SetAuthContext(ctx, &clone), clone.ActiveOrganizationID
}

func aiDetectionSummaries(t *testing.T, ti *testInstance, orgID string) map[string]telemetryrepo.AIDetectionSummaryRow {
	t.Helper()
	rows, err := telemetryrepo.New(ti.chConn).ListAIDetectionSummaries(t.Context(), telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       orgID,
		Categories:           nil,
		UserEmails:           nil,
		CanonicalIdentityOrg: "",
	})
	require.NoError(t, err)
	byTarget := make(map[string]telemetryrepo.AIDetectionSummaryRow, len(rows))
	for _, row := range rows {
		byTarget[row.TargetID] = row
	}
	return byTarget
}

func aiScanReceipts(t *testing.T, ti *testInstance, orgID string) []telemetryrepo.AIScanReceiptRow {
	t.Helper()
	rows, err := telemetryrepo.New(ti.chConn).ListAIScanReceipts(t.Context(), telemetryrepo.ListAIScanReceiptsParams{
		OrganizationID: orgID,
		DeviceSerial:   "",
		Limit:          0,
	})
	require.NoError(t, err)
	return rows
}

func TestReportAIScan_StoresDetectionsAndReceipt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	started := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	completed := started.Add(30 * time.Second)

	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     started.Format(time.RFC3339),
		ScanCompletedAt:   completed.Format(time.RFC3339),
		TargetListVersion: 3,
		Matches: []*gen.AIScanMatch{
			{TargetID: "cursor", Category: "harness", Signal: "installed", Version: new("1.7.2")},
			{TargetID: "ollama", Category: "local_model", Signal: "running", Version: nil},
		},
		Email:        new("developer@example.com"),
		SerialNumber: new("C02XL0GZJGH5"),
		Hostname:     new("devbox"),
	})
	require.NoError(t, err)

	summaries := aiDetectionSummaries(t, ti, orgID)
	require.Len(t, summaries, 2)

	cursor := summaries["cursor"]
	require.Equal(t, "harness", cursor.Category)
	require.EqualValues(t, 1, cursor.UserCount)
	require.EqualValues(t, 1, cursor.DeviceCount)
	require.ElementsMatch(t, []string{"installed"}, cursor.Signals)
	require.WithinDuration(t, completed, cursor.FirstSeen, time.Second)
	require.WithinDuration(t, completed, cursor.LastSeen, time.Second)

	ollama := summaries["ollama"]
	require.Equal(t, "local_model", ollama.Category)
	require.ElementsMatch(t, []string{"running"}, ollama.Signals)

	receipts := aiScanReceipts(t, ti, orgID)
	require.Len(t, receipts, 1)
	receipt := receipts[0]
	require.Equal(t, "developer@example.com", receipt.UserEmail)
	require.Equal(t, "c02xl0gzjgh5", receipt.DeviceSerial, "serials are normalized like every other device write")
	require.EqualValues(t, 3, receipt.TargetListVersion, "the agent's compiled-in list version is echoed as reported")
	require.EqualValues(t, 2, receipt.MatchCount)
	require.WithinDuration(t, started, receipt.ScanStartedAt, time.Second)
	require.WithinDuration(t, completed, receipt.ScanCompletedAt, time.Second)
}

// A clean device still posts: no detections, one receipt with a zero match
// count — that receipt is what proves the device was scanned.
func TestReportAIScan_ZeroMatchesLandsReceiptOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	started := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     started.Format(time.RFC3339),
		ScanCompletedAt:   started.Add(5 * time.Second).Format(time.RFC3339),
		TargetListVersion: 3,
		Matches:           []*gen.AIScanMatch{},
		Email:             new("developer@example.com"),
		SerialNumber:      new("SERIAL-CLEAN"),
		Hostname:          nil,
	})
	require.NoError(t, err)

	require.Empty(t, aiDetectionSummaries(t, ti, orgID))

	receipts := aiScanReceipts(t, ti, orgID)
	require.Len(t, receipts, 1)
	require.EqualValues(t, 0, receipts[0].MatchCount)
	require.Equal(t, "developer@example.com", receipts[0].UserEmail)
}

// Target ids the server's catalog does not know are stored as reported — an
// agent binary can ship a newer compiled-in target list than this server —
// while ids the catalog knows get the catalog's category, whatever the agent
// reported.
func TestReportAIScan_StoresUnknownTargetIDsAsReported(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	now := time.Now().UTC().Truncate(time.Second)
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
		ScanCompletedAt:   now.Format(time.RFC3339),
		TargetListVersion: 9,
		Matches: []*gen.AIScanMatch{
			{TargetID: "brand-new-tool", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "cursor", Category: "local_model", Signal: "installed", Version: nil},
		},
		Email:        new("developer@example.com"),
		SerialNumber: new("SERIAL-1"),
		Hostname:     nil,
	})
	require.NoError(t, err)

	summaries := aiDetectionSummaries(t, ti, orgID)
	require.Len(t, summaries, 2)

	unknown, ok := summaries["brand-new-tool"]
	require.True(t, ok, "unknown target ids must be stored, not dropped")
	require.Equal(t, "harness", unknown.Category, "unknown ids keep the reported category")

	cursor := summaries["cursor"]
	require.Equal(t, "harness", cursor.Category, "the catalog's category wins for ids it knows")

	receipts := aiScanReceipts(t, ti, orgID)
	require.Len(t, receipts, 1)
	require.EqualValues(t, 2, receipts[0].MatchCount, "unknown-target matches still count")
}

func TestReportAIScan_PreservesFirstSeenAcrossReports(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	first := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	second := first.Add(time.Hour)

	for _, completed := range []time.Time{first, second} {
		err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
			ScanStartedAt:     completed.Add(-10 * time.Second).Format(time.RFC3339),
			ScanCompletedAt:   completed.Format(time.RFC3339),
			TargetListVersion: 3,
			Matches: []*gen.AIScanMatch{
				{TargetID: "claude-code", Category: "harness", Signal: "installed", Version: nil},
			},
			Email:        new("developer@example.com"),
			SerialNumber: new("SERIAL-1"),
			Hostname:     nil,
		})
		require.NoError(t, err)
	}

	summaries := aiDetectionSummaries(t, ti, orgID)
	row := summaries["claude-code"]
	require.WithinDuration(t, first, row.FirstSeen, time.Second, "first_seen must survive the second report's read-merge-write")
	require.WithinDuration(t, second, row.LastSeen, time.Second)
	require.EqualValues(t, 1, row.UserCount)
	require.EqualValues(t, 1, row.DeviceCount)

	require.Len(t, aiScanReceipts(t, ti, orgID), 2, "every scan posts its own receipt")
}

// The org install key vouches for an email like getPlugins; without one the
// report is unattributable and must be rejected.
func TestReportAIScan_OrgKeyRequiresVouchedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, _ = withUniqueScanOrg(t, ctx)

	now := time.Now().UTC()
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
		ScanCompletedAt:   now.Format(time.RFC3339),
		TargetListVersion: 3,
		Matches:           []*gen.AIScanMatch{},
		Email:             nil,
		SerialNumber:      nil,
		Hostname:          nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeBadRequest, shareableErr.Code)
}

// A per-user key attributes the scan to the key owner: a vouched email in the
// header must be ignored on that path, mirroring getPlugins.
func TestReportAIScan_PerUserKeyAttributesToKeyOwner(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)
	ctx = withPerUserKeyAuth(t, ctx, "owner@example.com")

	now := time.Now().UTC().Truncate(time.Second)
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
		ScanCompletedAt:   now.Format(time.RFC3339),
		TargetListVersion: 3,
		Matches:           []*gen.AIScanMatch{},
		Email:             new("someone-else@example.com"),
		SerialNumber:      nil,
		Hostname:          nil,
	})
	require.NoError(t, err)

	receipts := aiScanReceipts(t, ti, orgID)
	require.Len(t, receipts, 1)
	require.Equal(t, "owner@example.com", receipts[0].UserEmail)
}
