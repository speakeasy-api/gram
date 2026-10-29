package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/device_integrations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
)

type stubDeviceCoverageReader struct {
	coverage      *gen.DeviceIntegrationCoverage
	devices       *gen.ListManagedDevicesResult
	devicesErr    error
	coverageOrg   string
	coverageProv  string
	lastQuery     deviceintegrations.ManagedDevicesQuery
	devicesCalled bool
}

func (s *stubDeviceCoverageReader) Coverage(_ context.Context, organizationID string, provider string) (*gen.DeviceIntegrationCoverage, error) {
	s.coverageOrg, s.coverageProv = organizationID, provider
	return s.coverage, nil
}

func (s *stubDeviceCoverageReader) ManagedDevices(_ context.Context, query deviceintegrations.ManagedDevicesQuery) (*gen.ListManagedDevicesResult, error) {
	s.lastQuery, s.devicesCalled = query, true
	return s.devices, s.devicesErr
}

type stubOrgReadAuthorizer struct {
	err    error
	checks []authz.Check
}

func (s *stubOrgReadAuthorizer) Require(_ context.Context, checks ...authz.Check) error {
	s.checks = append(s.checks, checks...)
	return s.err
}

//go:fix inline
func TestDeviceCoverageGetCoverageReadsThePrincipalsOrganization(t *testing.T) {
	t.Parallel()

	reader := &stubDeviceCoverageReader{coverage: &gen.DeviceIntegrationCoverage{
		TotalDevices: 10, AgentActive: 6, NoAgent: 4, Attestation: "device", ActiveWindowMinutes: 60,
		PolicyAttested: 3, PolicyOutdated: 1, PolicyUnreported: 1, EnforcementHalted: 1,
	}}
	authorizer := &stubOrgReadAuthorizer{}
	service := NewDeviceCoverageService(reader, authorizer, allowingBudget())
	require.NotNil(t, service)

	out, err := service.GetCoverage(t.Context(), shadowAIPrincipal(), GetDeviceCoverageInput{Provider: " jamf "})
	require.NoError(t, err)
	require.Equal(t, "org_1", reader.coverageOrg)
	require.Equal(t, "jamf", reader.coverageProv)
	require.EqualValues(t, 10, out.TotalDevices)
	require.Equal(t, "device", out.Attestation)
	require.EqualValues(t, 3, out.PolicyAttested)
	require.EqualValues(t, 1, out.PolicyOutdated)
	require.EqualValues(t, 1, out.PolicyUnreported)
	require.EqualValues(t, 1, out.EnforcementHalted)

	require.Equal(t, []authz.Check{{Scope: authz.ScopeOrgRead, ResourceID: "org_1"}}, authorizer.checks)
}

// The dashboard's device pages require org:read, so this surface must refuse
// the same callers rather than become the way around it.
func TestDeviceCoverageRefusesWithoutLiveOrgRead(t *testing.T) {
	t.Parallel()

	denied := errors.New("forbidden")
	reader := &stubDeviceCoverageReader{}
	service := NewDeviceCoverageService(reader, &stubOrgReadAuthorizer{err: denied}, allowingBudget())
	require.NotNil(t, service)

	_, err := service.GetCoverage(t.Context(), shadowAIPrincipal(), GetDeviceCoverageInput{})
	require.ErrorIs(t, err, denied)
	_, err = service.ListManagedDevices(t.Context(), shadowAIPrincipal(), ListManagedDevicesInput{})
	require.ErrorIs(t, err, denied)
	require.Empty(t, reader.coverageOrg)
	require.False(t, reader.devicesCalled)
}

func TestDeviceCoverageListManagedDevicesProjectsAttestation(t *testing.T) {
	t.Parallel()

	reader := &stubDeviceCoverageReader{devices: &gen.ListManagedDevicesResult{
		Devices: []*gen.ManagedDevice{{
			ID:             "row-id",
			ExternalID:     "mdm-id",
			Provider:       "jamf",
			Hostname:       new("alpha-mbp"),
			SerialNumber:   new("C02ALPHA"),
			UserEmail:      new("alpha@example.com"),
			CoverageBucket: "agent_active",
			PolicyState:    new("outdated"),
			AgentState: &gen.ManagedDeviceAgentState{
				ReportedAt:        "2026-10-29T12:00:00Z",
				AppliedEtag:       new("etag-old"),
				AppliedAt:         new("2026-10-28T12:00:00Z"),
				SpeakeasydVersion: new("1.4.0"),
				CurrentBlocked:    true,
				AutoUpdate:        new("automatic"),
				Tools:             []*gen.ManagedDeviceTool{{Tool: "claude_code", Layer: "managed", Outcome: "armed"}},
			},
		}},
		NextCursor: new("cursor-2"),
	}}
	service := NewDeviceCoverageService(reader, &stubOrgReadAuthorizer{}, allowingBudget())
	require.NotNil(t, service)

	out, err := service.ListManagedDevices(t.Context(), shadowAIPrincipal(), ListManagedDevicesInput{
		PolicyState: "outdated", UserEmail: "alpha@example.com", Cursor: "cursor-1",
	})
	require.NoError(t, err)
	require.Equal(t, deviceintegrations.ManagedDevicesQuery{
		OrganizationID: "org_1",
		PolicyState:    "outdated",
		UserEmails:     []string{"alpha@example.com"},
		Cursor:         "cursor-1",
		Limit:          defaultManagedDevicesPage,
	}, reader.lastQuery)

	require.Equal(t, "cursor-2", out.NextCursor)
	require.Len(t, out.Devices, 1)
	device := out.Devices[0]
	require.Equal(t, "alpha-mbp", device.Hostname)
	require.Equal(t, "C02ALPHA", device.SerialNumber)
	require.Equal(t, "outdated", device.PolicyState)
	require.Equal(t, "1.4.0", device.AgentVersion)
	require.True(t, device.BlockedBuild)
	require.False(t, device.EnforcementHalted)
	require.Equal(t, []ManagedDeviceToolOutcome{{Tool: "claude_code", Outcome: "armed"}}, device.Tools)
}

func TestDeviceCoverageListManagedDevicesRejectsCorrectableArguments(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]ListManagedDevicesInput{
		"unknown bucket":       {CoverageBucket: "covered"},
		"unknown policy state": {PolicyState: "enforced"},
		"limit too large":      {Limit: maxManagedDevicesPage + 1},
		"negative limit":       {Limit: -1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &stubDeviceCoverageReader{}
			service := NewDeviceCoverageService(reader, &stubOrgReadAuthorizer{}, allowingBudget())
			_, err := service.ListManagedDevices(t.Context(), shadowAIPrincipal(), input)
			require.ErrorIs(t, err, ErrDeviceCoverageInvalid)
			require.False(t, reader.devicesCalled)
		})
	}

	reader := &stubDeviceCoverageReader{devicesErr: deviceintegrations.ErrInvalidCursor}
	service := NewDeviceCoverageService(reader, &stubOrgReadAuthorizer{}, allowingBudget())
	_, err := service.ListManagedDevices(t.Context(), shadowAIPrincipal(), ListManagedDevicesInput{Cursor: "not-a-cursor"})
	require.ErrorIs(t, err, ErrDeviceCoverageInvalid)

	result, ok := deviceCoverageToolResult(err)
	require.True(t, ok)
	require.True(t, result.IsError)
}

func TestDeviceCoverageServiceRequiresEveryDependency(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewDeviceCoverageService(nil, &stubOrgReadAuthorizer{}, allowingBudget()))
	require.Nil(t, NewDeviceCoverageService(&stubDeviceCoverageReader{}, nil, allowingBudget()))
	require.Nil(t, NewDeviceCoverageService(&stubDeviceCoverageReader{}, &stubOrgReadAuthorizer{}, OperationBudget{}))
}
