package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingCoverageReader struct {
	*recordingOrganizationReader
	input  *gen.GetSupportCoveragePayload
	result *gen.SupportCoverageResult
	err    error
}

func (r *recordingCoverageReader) GetSupportCoverage(_ context.Context, input *gen.GetSupportCoveragePayload) (*gen.SupportCoverageResult, error) {
	r.input = input
	return r.result, r.err
}

func testCoverageReads() *recordingCoverageReader {
	seen := "2026-09-24T10:00:00Z"
	return &recordingCoverageReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		result: &gen.SupportCoverageResult{
			WindowDays: 30,
			From:       "2026-08-25T10:00:00Z",
			To:         seen,
			Cells: []*gen.SupportCoverageCell{
				{Capability: "session", Surface: "cursor", Status: "observed", Value: 12, Detail: "", LastSeen: &seen},
				{Capability: "session", Surface: "cowork", Status: "none", Value: 0, Detail: "", LastSeen: nil},
			},
			Unmapped: []*gen.SupportCoverageUnmapped{{HookSource: "some-new-agent", Sessions: 4}},
		},
	}
}

func TestOrganizationSupportCoverageExactTargetAndProjection(t *testing.T) {
	t.Parallel()
	reads := testCoverageReads()

	status, body, data := callStaffReadTool(t, reads, "get_organization_support_coverage", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status, body)

	// Staff name the organization, and it is resolved exactly before any read.
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.input.OrganizationID)
	require.Nil(t, reads.input.AdminSessionToken)

	var output OrganizationCoverage
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, "org-a", output.OrganizationID)
	require.Equal(t, 30, output.WindowDays)
	require.Len(t, output.Cells, 2)
	require.Equal(t, "observed", output.Cells[0].Status)
	// Absent rather than an empty stamp when there is no evidence.
	require.Nil(t, output.Cells[1].LastSeen)
	// An unrecognized source is reported, not hidden.
	require.Equal(t, []string{"some-new-agent"}, output.UnmappedSources)
}
