package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

type recordingHookFleetReader struct {
	*recordingOrganizationReader
	input  *gen.GetOrganizationHookFleetPayload
	result *gen.AdminHookFleetResult
	err    error
}

func (r *recordingHookFleetReader) GetOrganizationHookFleet(_ context.Context, input *gen.GetOrganizationHookFleetPayload) (*gen.AdminHookFleetResult, error) {
	r.input = input
	return r.result, r.err
}

func testHookFleetReads(installs int) *recordingHookFleetReader {
	rows := make([]*gen.AdminHookFleetInstall, 0, installs)
	for i := range installs {
		rows = append(rows, &gen.AdminHookFleetInstall{
			ProjectID: "project-a", UserEmail: "dev@example.com", Hostname: fmt.Sprintf("host-%d", i), Harness: "claude",
			Version: "0.3.40", VersionStatus: "behind", EndpointGeneration: "ingest_binary", InstallRoute: "marketplace",
			FirstSeen: "2026-11-01T00:00:00Z", LastSeen: "2026-11-09T00:00:00Z", Events: 3, Errors: 0, Quiet: false,
		})
	}
	return &recordingHookFleetReader{
		recordingOrganizationReader: &recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		result: &gen.AdminHookFleetResult{
			OrganizationID: "org-a", WindowDays: 30, Since: "2026-10-11T00:00:00Z",
			LatestVersion: "0.3.46", MinimumVersion: nil, QuietAfterDays: 3, Truncated: false,
			TotalInstalls: int64(installs), QuietInstalls: 0, LegacyEvents: 5,
			Versions:    []*gen.AdminHookFleetVersion{{Version: "0.3.40", Status: "behind", Installs: int64(installs)}},
			Routes:      []*gen.AdminHookFleetBucket{{Value: "marketplace", Installs: int64(installs), Events: 0}},
			Generations: []*gen.AdminHookFleetBucket{{Value: "ingest_binary", Installs: int64(installs), Events: 9}},
			Installs:    rows,
		},
	}
}

func TestOrganizationHookFleetExactTargetAndProjection(t *testing.T) {
	t.Parallel()
	reads := testHookFleetReads(2)

	status, body, data := callStaffReadTool(t, reads, "get_organization_hook_fleet", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.input.OrganizationID)
	require.Equal(t, hookFleetDefaultWindowDays, reads.input.WindowDays)
	require.Nil(t, reads.input.AdminSessionToken)

	var output OrganizationHookFleet
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, "0.3.46", output.LatestVersion)
	require.Nil(t, output.MinimumVersion)
	require.Equal(t, int64(5), output.LegacyEvents)
	require.Len(t, output.Installs, 2)
	require.Equal(t, "behind", output.Installs[0].VersionStatus)
	require.False(t, output.InstallsTruncated)
}

func TestOrganizationHookFleetBoundsInstalls(t *testing.T) {
	t.Parallel()
	reads := testHookFleetReads(hookFleetMaxInstalls + 1)

	_, body, data := callStaffReadTool(t, reads, "get_organization_hook_fleet", `{"organization_id":"org-a","window_days":7}`)
	require.Equal(t, 7, reads.input.WindowDays, body)

	var output OrganizationHookFleet
	require.NoError(t, json.Unmarshal(data, &output))
	require.Len(t, output.Installs, hookFleetMaxInstalls)
	require.True(t, output.InstallsTruncated)
}

func TestOrganizationHookFleetErrors(t *testing.T) {
	t.Parallel()

	for _, args := range []string{`{"organization_id":"org-a","window_days":90}`, `{"organization_id":"org-a","window_days":-1}`} {
		reads := testHookFleetReads(0)
		_, body, _ := callStaffReadTool(t, reads, "get_organization_hook_fleet", args)
		require.Contains(t, body, `"isError":true`)
		require.Nil(t, reads.input)
	}

	reads := testHookFleetReads(0)
	reads.err = errors.New("clickhouse down")
	_, body, _ := callStaffReadTool(t, reads, "get_organization_hook_fleet", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.NotContains(t, body, "clickhouse down")
}
