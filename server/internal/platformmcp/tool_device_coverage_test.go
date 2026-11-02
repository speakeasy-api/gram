package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/device_integrations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
)

type stubDeviceCoverageReader struct {
	coverage      *gen.DeviceIntegrationCoverage
	coverageErr   error
	devices       *gen.ListManagedDevicesResult
	devicesErr    error
	coverageOrg   string
	coverageProv  string
	lastQuery     deviceintegrations.ManagedDevicesQuery
	devicesCalled bool
}

func (s *stubDeviceCoverageReader) Coverage(_ context.Context, organizationID string, provider string) (*gen.DeviceIntegrationCoverage, error) {
	s.coverageOrg, s.coverageProv = organizationID, provider
	return s.coverage, s.coverageErr
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
		TotalDevices: 10, AgentActive: 6, AgentActiveDeviceAttested: 5, NoAgent: 4, Attestation: "user", ActiveWindowMinutes: 60,
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
	require.Equal(t, "user", out.Attestation)
	require.EqualValues(t, 5, out.AgentActiveDeviceAttested)
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
				Attestation:       "user",
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
	require.Equal(t, "user", device.ReportAttestation)
	require.Equal(t, "1.4.0", device.AgentVersion)
	require.True(t, device.BlockedBuild)
	require.False(t, device.EnforcementHalted)
	require.Equal(t, []ManagedDeviceToolOutcome{{Tool: "claude_code", Outcome: "armed"}}, device.Tools)
}

func TestDeviceCoverageListManagedDevicesRejectsCorrectableArguments(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]ListManagedDevicesInput{
		"unknown provider":     {Provider: "jamff"},
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

func TestDeviceCoverageGetCoverageRejectsUnknownProviderAndWrapsReadErrors(t *testing.T) {
	t.Parallel()

	reader := &stubDeviceCoverageReader{}
	service := NewDeviceCoverageService(reader, &stubOrgReadAuthorizer{}, allowingBudget())
	_, err := service.GetCoverage(t.Context(), shadowAIPrincipal(), GetDeviceCoverageInput{Provider: "jamff"})
	require.ErrorIs(t, err, ErrDeviceCoverageInvalid)
	require.Empty(t, reader.coverageOrg, "a typo must not read as an empty fleet")

	// A failed read is the server's problem, not an argument to correct.
	readErr := errors.New("db down")
	reader.coverageErr = readErr
	_, err = service.GetCoverage(t.Context(), shadowAIPrincipal(), GetDeviceCoverageInput{})
	require.ErrorIs(t, err, readErr)
	require.NotErrorIs(t, err, ErrDeviceCoverageInvalid)
	_, ok := deviceCoverageToolResult(err)
	require.False(t, ok, "left to the generic tool error")
}

func TestDeviceCoverageToolsMatchWhenUnavailable(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "device-coverage-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "device-coverage-unavailable", Version: "0.0.1"}, nil))
	registerDeviceCoverageTools(live, NewDeviceCoverageService(&stubDeviceCoverageReader{}, &stubOrgReadAuthorizer{}, allowingBudget()))
	// An invalid service falls back to the unavailable tools.
	registerDeviceCoverageTools(unavailable, nil)

	liveTools, unavailableTools := live.Descriptors(), unavailable.Descriptors()
	require.Len(t, unavailableTools, len(liveTools))
	for i := range liveTools {
		require.Equal(t, liveTools[i].Name, unavailableTools[i].Name)
		require.Equal(t, liveTools[i].Meta, unavailableTools[i].Meta)
		require.Equal(t, liveTools[i].Annotations, unavailableTools[i].Annotations)
		require.JSONEq(t, string(liveTools[i].InputSchema), string(unavailableTools[i].InputSchema))
	}
	require.ElementsMatch(t, []string{operationGetDeviceCoverage, operationListManagedDevices}, []string{liveTools[0].Name, liveTools[1].Name})
}

func TestDeviceCoverageServiceRequiresEveryDependency(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewDeviceCoverageService(nil, &stubOrgReadAuthorizer{}, allowingBudget()))
	require.Nil(t, NewDeviceCoverageService(&stubDeviceCoverageReader{}, nil, allowingBudget()))
	require.Nil(t, NewDeviceCoverageService(&stubDeviceCoverageReader{}, &stubOrgReadAuthorizer{}, OperationBudget{}))
}
