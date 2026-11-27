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

type recordingHooksRolloutReader struct {
	recordingOrganizationReader
	organizationInput *gen.GetOrganizationHooksRolloutPayload
	platform          *gen.AdminHooksRollout
	organization      *gen.AdminOrganizationHooksRollout
	progress          *gen.AdminHooksReleaseProgress
	rolloutErr        error
}

func (r *recordingHooksRolloutReader) GetHooksReleaseProgress(context.Context, *gen.GetHooksReleaseProgressPayload) (*gen.AdminHooksReleaseProgress, error) {
	return r.progress, r.rolloutErr
}

func (r *recordingHooksRolloutReader) GetHooksRollout(context.Context, *gen.GetHooksRolloutPayload) (*gen.AdminHooksRollout, error) {
	return r.platform, r.rolloutErr
}

func (r *recordingHooksRolloutReader) GetOrganizationHooksRollout(_ context.Context, input *gen.GetOrganizationHooksRolloutPayload) (*gen.AdminOrganizationHooksRollout, error) {
	r.organizationInput = input
	return r.organization, r.rolloutErr
}

func TestGetHooksRolloutProjectsPinsAndHistory(t *testing.T) {
	t.Parallel()
	orgID, orgSlug, version, defaultVersion := "org-a", "example-one", 41, 42
	reads := &recordingHooksRolloutReader{platform: &gen.AdminHooksRollout{
		CurrentVersion: 42,
		DefaultPin:     &gen.AdminHooksRolloutPin{Version: 42, SetBy: "operator@example.com", SetAt: "2026-01-01T00:00:00Z"},
		Stage: &gen.AdminHooksRolloutStage{
			Revision: 7, Version: 43, Percent: 25, State: "running", Reason: nil, SetBy: "hooks-rollout-schedule",
			SetAt: "2026-01-03T00:00:00Z", NextAdvanceAt: new("2026-01-04T00:00:00Z"),
		},
		CanaryOrganizationSlugs: []string{"canary-org"},
		Overrides: []*gen.AdminHooksRolloutOverride{{
			OrganizationID: orgID, OrganizationName: "Example One", OrganizationSlug: orgSlug,
			Pin: &gen.AdminHooksRolloutPin{Version: 41, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"},
		}},
		RecentChanges: []*gen.AdminHooksRolloutChange{
			{OrganizationID: &orgID, OrganizationSlug: &orgSlug, Version: &version, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"},
			{OrganizationID: nil, OrganizationSlug: nil, Version: &defaultVersion, SetBy: "operator@example.com", SetAt: "2026-01-01T00:00:00Z"},
		},
	}}

	status, body, data := callStaffReadTool(t, reads, "get_hooks_rollout", `{}`)
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, `"isError":true`)
	var out HooksRollout
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, 42, out.CurrentVersion)
	require.Equal(t, 42, out.DefaultPin.Version)
	require.Equal(t, &HooksRolloutStage{Version: 43, Percent: 25, State: "running", Reason: "", SetBy: "hooks-rollout-schedule", SetAt: "2026-01-03T00:00:00Z", NextAdvanceAt: "2026-01-04T00:00:00Z"}, out.Stage)
	require.Equal(t, []string{"canary-org"}, out.CanaryOrganizationSlugs)
	require.Equal(t, []HooksRolloutOverride{{OrganizationID: orgID, OrganizationSlug: orgSlug, Pin: HooksRolloutPin{Version: 41, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"}}}, out.Overrides)
	require.False(t, out.OverridesTruncated)
	require.Len(t, out.RecentChanges, 2)
	require.Equal(t, orgID, out.RecentChanges[0].OrganizationID)
	require.Equal(t, 41, *out.RecentChanges[0].Version)
	require.Empty(t, out.RecentChanges[1].OrganizationID)
	require.Equal(t, 42, *out.RecentChanges[1].Version)
	require.NotContains(t, body, "Example One", "organization names are customer content and are not projected")
}

func TestGetHooksRolloutBoundsOverrides(t *testing.T) {
	t.Parallel()
	overrides := make([]*gen.AdminHooksRolloutOverride, maxHooksRolloutOverrides+1)
	for i := range overrides {
		overrides[i] = &gen.AdminHooksRolloutOverride{OrganizationID: "org", OrganizationName: "", OrganizationSlug: "org", Pin: &gen.AdminHooksRolloutPin{Version: 1, SetBy: "operator@example.com", SetAt: "2026-01-01T00:00:00Z"}}
	}
	reads := &recordingHooksRolloutReader{platform: &gen.AdminHooksRollout{CurrentVersion: 1, Overrides: overrides}}

	_, _, data := callStaffReadTool(t, reads, "get_hooks_rollout", `{}`)
	var out HooksRollout
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.Overrides, maxHooksRolloutOverrides)
	require.True(t, out.OverridesTruncated)
}

func TestGetHooksReleaseProgressProjectsCounts(t *testing.T) {
	t.Parallel()
	binary := "0.3.58"
	reads := &recordingHooksRolloutReader{progress: &gen.AdminHooksReleaseProgress{
		CurrentVersion: 66, CurrentBinaryVersion: &binary,
		Organizations: 3, OrganizationsCleared: 1, OrganizationsHeld: 1, OrganizationsOnLegacyFlag: 1,
		Repos: 4, ReposOnCurrent: 2,
		RepoVersions: []*gen.AdminHooksReleaseRepoVersion{{Version: "66", Repos: 2}, {Version: "", Repos: 2}},
		Fleet: &gen.AdminHooksReleaseFleet{
			WindowDays: 7, Installs: 5, InstallsOnCurrent: 3,
			Versions: []*gen.AdminHooksReleaseInstallVersion{{Version: "0.3.58", Status: "current", Installs: 3}, {Version: "0.3.40", Status: "behind", Installs: 2}},
		},
	}}

	status, body, data := callStaffReadTool(t, reads, "get_hooks_release_progress", `{}`)
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, `"isError":true`)
	var out HooksReleaseProgress
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, 66, out.CurrentVersion)
	require.Equal(t, "0.3.58", out.CurrentBinaryVersion)
	require.Equal(t, int64(1), out.OrganizationsCleared)
	require.Equal(t, int64(2), out.ReposOnCurrent)
	require.Equal(t, []HooksReleaseRepoVersion{{Version: "66", Repos: 2}, {Version: "", Repos: 2}}, out.RepoVersions)
	require.NotNil(t, out.Fleet)
	require.Equal(t, int64(3), out.Fleet.InstallsOnCurrent)
	require.Len(t, out.Fleet.Versions, 2)

	failing := &recordingHooksRolloutReader{rolloutErr: errors.New("database down")}
	_, body, _ = callStaffReadTool(t, failing, "get_hooks_release_progress", `{}`)
	require.Contains(t, body, `"isError":true`)
}

func TestGetOrganizationHooksRolloutReadsExactTarget(t *testing.T) {
	t.Parallel()
	effective, eligible := 41, false
	reads := &recordingHooksRolloutReader{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		organization: &gen.AdminOrganizationHooksRollout{
			OrganizationID: "org-a", CurrentVersion: 42, Source: gen.AdminHooksRolloutSource("organization"), EffectiveVersion: &effective, Eligible: &eligible,
			Override: &gen.AdminHooksRolloutPin{Version: 41, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"},
		},
	}

	status, body, data := callStaffReadTool(t, reads, "get_organization_hooks_rollout", `{"organization_id":"org-a"}`)
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, `"isError":true`)
	require.Equal(t, "org-a", reads.getInput.IDOrSlug)
	require.Equal(t, "org-a", reads.organizationInput.OrganizationID)
	var out OrganizationHooksRollout
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "organization", out.Source)
	require.Equal(t, 41, *out.EffectiveVersion)
	require.False(t, *out.Eligible)
	require.Equal(t, 41, out.Override.Version)
	require.Nil(t, out.DefaultPin)
}

func TestGetOrganizationHooksRolloutFailsClosed(t *testing.T) {
	t.Parallel()

	mismatched := &recordingHooksRolloutReader{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-b"}},
	}
	_, body, _ := callStaffReadTool(t, mismatched, "get_organization_hooks_rollout", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	require.Nil(t, mismatched.organizationInput, "a non-exact organization match is never read")

	failing := &recordingHooksRolloutReader{
		recordingOrganizationReader: recordingOrganizationReader{org: &gen.AdminOrganization{ID: "org-a"}},
		rolloutErr:                  errors.New("database down"),
	}
	_, body, _ = callStaffReadTool(t, failing, "get_organization_hooks_rollout", `{"organization_id":"org-a"}`)
	require.Contains(t, body, `"isError":true`)
	_, body, _ = callStaffReadTool(t, failing, "get_hooks_rollout", `{}`)
	require.Contains(t, body, `"isError":true`)
}

func TestHooksRolloutAppearsInContext(t *testing.T) {
	t.Parallel()
	reads := &recordingHooksRolloutReader{}
	body, data := issuerToolCall(t, reads, "get_admin_context", `{}`, true)
	require.NotContains(t, body, `"isError":true`)
	var out AdminContext
	require.NoError(t, json.Unmarshal(data, &out))
	require.Contains(t, out.Workflows, "inspect the hooks version rollout pins, the staged rollout, how far the current hooks release has reached, and which hooks version an organization is cleared for")
}
