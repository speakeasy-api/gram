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
	rolloutErr        error
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
	orgID, orgSlug, version := "org-a", "example-one", 41
	reads := &recordingHooksRolloutReader{platform: &gen.AdminHooksRollout{
		CurrentVersion:          42,
		DefaultPin:              &gen.AdminHooksRolloutPin{Version: 42, SetBy: "operator@example.com", SetAt: "2026-01-01T00:00:00Z"},
		CanaryOrganizationSlugs: []string{"canary-org"},
		Overrides: []*gen.AdminHooksRolloutOverride{{
			OrganizationID: orgID, OrganizationName: "Example One", OrganizationSlug: orgSlug,
			Pin: &gen.AdminHooksRolloutPin{Version: 41, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"},
		}},
		RecentChanges: []*gen.AdminHooksRolloutChange{
			{OrganizationID: &orgID, OrganizationSlug: &orgSlug, Version: &version, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"},
			{OrganizationID: nil, OrganizationSlug: nil, Version: nil, SetBy: "operator@example.com", SetAt: "2026-01-01T00:00:00Z"},
		},
	}}

	status, body, data := callStaffReadTool(t, reads, "get_hooks_rollout", `{}`)
	require.Equal(t, http.StatusOK, status)
	require.NotContains(t, body, `"isError":true`)
	var out HooksRollout
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, 42, out.CurrentVersion)
	require.Equal(t, 42, out.DefaultPin.Version)
	require.Equal(t, []string{"canary-org"}, out.CanaryOrganizationSlugs)
	require.Equal(t, []HooksRolloutOverride{{OrganizationID: orgID, OrganizationSlug: orgSlug, Pin: HooksRolloutPin{Version: 41, SetBy: "operator@example.com", SetAt: "2026-01-02T00:00:00Z"}}}, out.Overrides)
	require.False(t, out.OverridesTruncated)
	require.Len(t, out.RecentChanges, 2)
	require.Equal(t, orgID, out.RecentChanges[0].OrganizationID)
	require.Equal(t, 41, *out.RecentChanges[0].Version)
	require.Empty(t, out.RecentChanges[1].OrganizationID)
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
	require.Contains(t, out.Workflows, "inspect the hooks version rollout pins and which hooks version an organization is cleared for")
}
