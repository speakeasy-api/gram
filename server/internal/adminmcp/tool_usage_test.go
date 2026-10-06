package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingUsageReader struct {
	*recordingOrganizationReader
	input   *gen.GetPaygBillingSummaryPayload
	summary *gen.AdminPaygBillingSummary
	err     error
}

func (r *recordingUsageReader) GetPaygBillingSummary(_ context.Context, input *gen.GetPaygBillingSummaryPayload) (*gen.AdminPaygBillingSummary, error) {
	r.input = input
	return r.summary, r.err
}

func testUsageReads() *recordingUsageReader {
	return &recordingUsageReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		summary:                     &gen.AdminPaygBillingSummary{},
	}
}

func TestOrganizationUsageSummaryExactTargetAndProjection(t *testing.T) {
	t.Parallel()
	reads := testUsageReads()
	recordedThrough := "2026-01-12T00:00:00Z"
	reads.summary = &gen.AdminPaygBillingSummary{
		PeriodStart: "2026-01-01T00:00:00Z", PeriodEnd: "2026-02-01T00:00:00Z",
		TumTokens: 42, TumUnitPriceUsd: "0.0001", TumCostUsd: "0.0042",
		OtherInferenceSpendUsd: "1.25", RecordedThrough: &recordedThrough,
		EstimatedTotalUsd: "1.2542",
	}
	status, body, data := callStaffReadTool(t, reads, "get_organization_usage_summary", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.input.OrganizationID)
	require.Nil(t, reads.input.AdminSessionToken)
	var output OrganizationUsageSummary
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, OrganizationUsageSummary{
		OrganizationID: "org-a", PeriodStart: "2026-01-01T00:00:00Z", PeriodEnd: "2026-02-01T00:00:00Z",
		TokensUnderManagement: 42, TumUnitPriceUSD: "0.0001", TumCostUSD: "0.0042",
		OtherInferenceSpendUSD: "1.25", RecordedThrough: &recordedThrough, EstimatedTotalUSD: "1.2542",
	}, output)
	for _, excluded := range []string{"stripe_customer_id", "inference_key", "admin_session_token"} {
		require.NotContains(t, body, excluded)
	}

	reads.summary.RecordedThrough = nil
	_, _, data = callStaffReadTool(t, reads, "get_organization_usage_summary", `{"organization_id":"org-a"}`)
	output = OrganizationUsageSummary{}
	require.NoError(t, json.Unmarshal(data, &output))
	require.Nil(t, output.RecordedThrough)
}

func TestOrganizationUsageSummaryFailsClosed(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{`{"organization_id":" org-a"}`, `{"organization_id":""}`, `{"organization_id":"org-a "}`} {
		reads := testUsageReads()
		_, body, _ := callStaffReadTool(t, reads, "get_organization_usage_summary", invalid)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.input)
	}
	reads := testUsageReads()
	reads.org.ID = "org-b"
	_, body, _ := callStaffReadTool(t, reads, "get_organization_usage_summary", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, reads.input)

	reads = testUsageReads()
	reads.err = errors.New("private billing failure")
	_, body, _ = callStaffReadTool(t, reads, "get_organization_usage_summary", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "private billing failure")

	reads = testUsageReads()
	reads.summary = nil
	_, body, _ = callStaffReadTool(t, reads, "get_organization_usage_summary", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)

	_, body, _ = callStaffReadTool(t, &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}}, "get_organization_usage_summary", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
}

func TestOrganizationUsageSummaryContextAvailability(t *testing.T) {
	t.Parallel()
	_, body, _ := callStaffReadTool(t, testUsageReads(), "get_admin_context", `{}`)
	require.Contains(t, body, "inspect current-cycle organization usage estimate")
	_, body, _ = callStaffReadTool(t, &recordingOrganizationReader{}, "get_admin_context", `{}`)
	require.NotContains(t, body, "inspect current-cycle organization usage estimate")
}
