package agent_test

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentserver "github.com/speakeasy-api/gram/server/gen/http/agent/server"
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

func TestReportAIScan_HTTPAcceptsBodyWithoutMatchCount(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/rpc/agent.reportAIScan", bytes.NewBufferString(`{
		"scan_started_at": "2026-08-31T12:00:00Z",
		"scan_completed_at": "2026-08-31T12:00:02Z",
		"target_list_version": 3,
		"matches": []
	}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Gram-Key", "test-key")

	var decoded *gen.ReportAIScanPayload
	handler := agentserver.NewReportAIScanHandler(
		func(_ context.Context, payload any) (any, error) {
			var ok bool
			decoded, ok = payload.(*gen.ReportAIScanPayload)
			require.True(t, ok)
			return nil, nil
		},
		nil,
		goahttp.RequestDecoder,
		goahttp.ResponseEncoder,
		nil,
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NotNil(t, decoded)
	require.Empty(t, decoded.Matches)
}

func TestReportAIScan_StoresDetectionsAndReceipt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	started := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	completed := started.Add(30 * time.Second)

	requestStartedAt := time.Now().UTC()
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
	requestCompletedAt := time.Now().UTC()

	summaries := aiDetectionSummaries(t, ti, orgID)
	require.Len(t, summaries, 2)

	cursor := summaries["cursor"]
	require.Equal(t, "harness", cursor.Category)
	require.EqualValues(t, 1, cursor.UserCount)
	require.EqualValues(t, 1, cursor.DeviceCount)
	require.ElementsMatch(t, []string{"installed"}, cursor.Signals)
	require.WithinRange(t, cursor.FirstSeen, requestStartedAt, requestCompletedAt, "first_seen must be derived from server receipt time")
	require.Equal(t, cursor.FirstSeen, cursor.LastSeen)

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
	require.WithinRange(t, receipt.ReceivedAt, requestStartedAt, requestCompletedAt)
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

	firstReportedCompletion := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	firstRequestStartedAt := time.Now().UTC()
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     firstReportedCompletion.Add(-10 * time.Second).Format(time.RFC3339),
		ScanCompletedAt:   firstReportedCompletion.Format(time.RFC3339),
		TargetListVersion: 3,
		Matches: []*gen.AIScanMatch{
			{TargetID: "claude-code", Category: "harness", Signal: "installed", Version: nil},
		},
		Email:        new("developer@example.com"),
		SerialNumber: new("SERIAL-1"),
		Hostname:     nil,
	})
	require.NoError(t, err)
	firstRequestCompletedAt := time.Now().UTC()

	firstRow := aiDetectionSummaries(t, ti, orgID)["claude-code"]
	require.WithinRange(t, firstRow.FirstSeen, firstRequestStartedAt, firstRequestCompletedAt)
	require.Equal(t, firstRow.FirstSeen, firstRow.LastSeen)

	secondReportedCompletion := firstReportedCompletion.Add(time.Hour)
	secondRequestStartedAt := time.Now().UTC()
	err = ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     secondReportedCompletion.Add(-10 * time.Second).Format(time.RFC3339),
		ScanCompletedAt:   secondReportedCompletion.Format(time.RFC3339),
		TargetListVersion: 3,
		Matches: []*gen.AIScanMatch{
			{TargetID: "claude-code", Category: "harness", Signal: "installed", Version: nil},
		},
		Email:        new("developer@example.com"),
		SerialNumber: new("SERIAL-1"),
		Hostname:     nil,
	})
	require.NoError(t, err)
	secondRequestCompletedAt := time.Now().UTC()

	row := aiDetectionSummaries(t, ti, orgID)["claude-code"]
	require.Equal(t, firstRow.FirstSeen, row.FirstSeen, "first_seen must survive the second report's read-merge-write")
	require.WithinRange(t, row.LastSeen, secondRequestStartedAt, secondRequestCompletedAt)
	require.EqualValues(t, 1, row.UserCount)
	require.EqualValues(t, 1, row.DeviceCount)

	receipts := aiScanReceipts(t, ti, orgID)
	require.Len(t, receipts, 2, "every scan posts its own receipt")
	require.WithinDuration(t, secondReportedCompletion, receipts[0].ScanCompletedAt, time.Second)
	require.WithinDuration(t, firstReportedCompletion, receipts[1].ScanCompletedAt, time.Second)
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

func TestReportAIScan_RejectsMalformedVouchedEmailWithoutWriting(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	now := time.Now().UTC()
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
		ScanCompletedAt:   now.Format(time.RFC3339),
		TargetListVersion: 3,
		Matches: []*gen.AIScanMatch{
			{TargetID: "cursor", Category: "harness", Signal: "installed", Version: nil},
		},
		Email:        new("not an email"),
		SerialNumber: new("SERIAL-INVALID-EMAIL"),
		Hostname:     nil,
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeBadRequest, shareableErr.Code)
	require.ErrorContains(t, err, "invalid email")
	require.Empty(t, aiDetectionSummaries(t, ti, orgID))
	require.Empty(t, aiScanReceipts(t, ti, orgID))
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

func TestReportAIScan_DerivesReceiptCountFromMatches(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)

	now := time.Now().UTC()
	err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
		ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
		ScanCompletedAt:   now.Format(time.RFC3339),
		TargetListVersion: math.MaxInt32,
		Matches: []*gen.AIScanMatch{
			{TargetID: "count-test-1", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-2", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-3", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-4", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-5", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-6", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-7", Category: "harness", Signal: "installed", Version: nil},
			{TargetID: "count-test-8", Category: "harness", Signal: "installed", Version: nil},
		},
		Email:        new("developer@example.com"),
		SerialNumber: new("SERIAL-COUNT"),
		Hostname:     nil,
	})
	require.NoError(t, err)

	receipts := aiScanReceipts(t, ti, orgID)
	require.Len(t, receipts, 1)
	require.EqualValues(t, math.MaxInt32, receipts[0].TargetListVersion, "receipt must preserve the agent-reported target list version")
	require.EqualValues(t, 8, receipts[0].MatchCount, "receipt count must be derived from the matches array")
}

func TestReportAIScan_RejectsMalformedMatches(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx, orgID := withUniqueScanOrg(t, ctx)
	now := time.Now().UTC()

	cases := []struct {
		name  string
		match *gen.AIScanMatch
	}{
		{name: "null match", match: nil},
		{name: "blank target id", match: &gen.AIScanMatch{TargetID: " ", Category: "harness", Signal: "installed", Version: nil}},
		{name: "invalid category", match: &gen.AIScanMatch{TargetID: "new-tool", Category: "other", Signal: "installed", Version: nil}},
		{name: "invalid signal", match: &gen.AIScanMatch{TargetID: "cursor", Category: "harness", Signal: "stopped", Version: nil}},
	}

	for _, testCase := range cases {
		err := ti.service.ReportAIScan(ctx, &gen.ReportAIScanPayload{
			ScanStartedAt:     now.Add(-time.Minute).Format(time.RFC3339),
			ScanCompletedAt:   now.Format(time.RFC3339),
			TargetListVersion: 3,
			Matches:           []*gen.AIScanMatch{testCase.match},
			Email:             new("developer@example.com"),
			SerialNumber:      new("SERIAL-INVALID"),
			Hostname:          nil,
		})
		var shareableErr *oops.ShareableError
		require.ErrorAs(t, err, &shareableErr, testCase.name)
		require.Equal(t, oops.CodeBadRequest, shareableErr.Code, testCase.name)
	}

	require.Empty(t, aiDetectionSummaries(t, ti, orgID))
	require.Empty(t, aiScanReceipts(t, ti, orgID), "malformed reports must not leave receipts")
}
