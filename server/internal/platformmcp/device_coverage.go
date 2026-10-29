package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/device_integrations"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
)

var (
	// ErrDeviceCoverageInvalid marks an argument the caller can correct, kept
	// apart from ErrDeviceCoverageUnavailable so a typo is not read as the
	// feature being off.
	ErrDeviceCoverageInvalid     = errors.New("invalid platform mcp device coverage request")
	ErrDeviceCoverageUnavailable = errors.New("platform mcp device coverage unavailable")
)

const (
	deviceCoverageFeature = "device_coverage"
	// Smaller than the API's 200: every row carries a tool list, and an agent
	// pages through rather than reading a whole fleet at once.
	maxManagedDevicesPage     = 100
	defaultManagedDevicesPage = 50
)

type deviceCoverageReader interface {
	Coverage(ctx context.Context, organizationID string, provider string) (*gen.DeviceIntegrationCoverage, error)
	ManagedDevices(ctx context.Context, query deviceintegrations.ManagedDevicesQuery) (*gen.ListManagedDevicesResult, error)
}

type deviceCoverageAuthorizer interface {
	Require(ctx context.Context, checks ...authz.Check) error
}

// DeviceCoverageService serves the MDM coverage reads: how much of the fleet
// runs the agent, and whether each device enforces the policy it was served.
// Organization-scoped, like the dashboard's Device Agent pages.
type DeviceCoverageService struct {
	reader     deviceCoverageReader
	authorizer deviceCoverageAuthorizer
	budget     OperationBudget
}

func NewDeviceCoverageService(reader deviceCoverageReader, authorizer deviceCoverageAuthorizer, budget OperationBudget) *DeviceCoverageService {
	if reader == nil || authorizer == nil || !budget.valid() {
		return nil
	}
	return &DeviceCoverageService{reader: reader, authorizer: authorizer, budget: budget}
}

func (s *DeviceCoverageService) valid() bool {
	return s != nil && s.reader != nil && s.authorizer != nil && s.budget.valid()
}

// admit requires org:read live, the grant the dashboard's device pages and
// the management API both require.
func (s *DeviceCoverageService) admit(ctx context.Context, principal Principal) error {
	if !s.valid() || principal.OrganizationID == "" {
		return ErrDeviceCoverageUnavailable
	}
	if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}); err != nil {
		return fmt.Errorf("require org read for device coverage: %w", err)
	}
	return s.budget.Allow(ctx, principal)
}

type GetDeviceCoverageInput struct {
	Provider string `json:"provider,omitempty" jsonschema:"one MDM provider, e.g. jamf or kandji; omit for every connected inventory"`
}

type DeviceCoverageOutput struct {
	TotalDevices int64 `json:"total_devices"`
	// ActiveWindowMinutes is how recent an agent's sign of life must be for
	// its device to count as running the agent.
	ActiveWindowMinutes int `json:"active_window_minutes"`
	// Attestation is device when every active device was matched by its own
	// hardware serial, or user when some only prove their assigned user runs
	// the agent somewhere.
	Attestation      string `json:"attestation"`
	AgentActive      int64  `json:"agent_active"`
	AgentStale       int64  `json:"agent_stale"`
	AgentOtherDevice int64  `json:"agent_other_device"`
	NoAgent          int64  `json:"no_agent"`
	NoEmail          int64  `json:"no_email"`
	UnresolvedEmail  int64  `json:"unresolved_email"`
	Missing          int64  `json:"missing"`
	// The policy counts split agent_active by whether the device enforces the
	// policy last served to it.
	PolicyAttested    int64 `json:"policy_attested"`
	PolicyOutdated    int64 `json:"policy_outdated"`
	PolicyUnreported  int64 `json:"policy_unreported"`
	EnforcementHalted int64 `json:"enforcement_halted"`
	// UnmanagedAgentUsers run the agent but have no device in the inventory.
	UnmanagedAgentUsers int64 `json:"unmanaged_agent_users"`
}

func (s *DeviceCoverageService) GetCoverage(ctx context.Context, principal Principal, input GetDeviceCoverageInput) (DeviceCoverageOutput, error) {
	if err := s.admit(ctx, principal); err != nil {
		return DeviceCoverageOutput{}, err
	}
	coverage, err := s.reader.Coverage(ctx, principal.OrganizationID, strings.TrimSpace(input.Provider))
	if err != nil {
		return DeviceCoverageOutput{}, fmt.Errorf("read device coverage: %w", err)
	}
	return DeviceCoverageOutput{
		TotalDevices:        coverage.TotalDevices,
		ActiveWindowMinutes: coverage.ActiveWindowMinutes,
		Attestation:         coverage.Attestation,
		AgentActive:         coverage.AgentActive,
		AgentStale:          coverage.AgentStale,
		AgentOtherDevice:    coverage.AgentOtherDevice,
		NoAgent:             coverage.NoAgent,
		NoEmail:             coverage.NoEmail,
		UnresolvedEmail:     coverage.UnresolvedEmail,
		Missing:             coverage.Missing,
		PolicyAttested:      coverage.PolicyAttested,
		PolicyOutdated:      coverage.PolicyOutdated,
		PolicyUnreported:    coverage.PolicyUnreported,
		EnforcementHalted:   coverage.EnforcementHalted,
		UnmanagedAgentUsers: coverage.UnmanagedAgentUsers,
	}, nil
}

type ListManagedDevicesInput struct {
	Provider       string `json:"provider,omitempty" jsonschema:"one MDM provider, e.g. jamf or kandji; omit for every connected inventory"`
	CoverageBucket string `json:"coverage_bucket,omitempty" jsonschema:"only devices in this coverage bucket: agent_active, agent_stale, agent_other_device, no_agent, no_email, unresolved_email or missing"`
	PolicyState    string `json:"policy_state,omitempty" jsonschema:"only agent_active devices in this policy state: attested, outdated, unreported or halted"`
	UserEmail      string `json:"user_email,omitempty" jsonschema:"only devices assigned to this email, matched case-insensitively"`
	Cursor         string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
	Limit          int    `json:"limit,omitempty" jsonschema:"page size, 1 to 100; defaults to 50"`
}

type ManagedDeviceToolOutcome struct {
	Tool    string `json:"tool"`
	Outcome string `json:"outcome"`
}

type ManagedDeviceSummary struct {
	Hostname       string `json:"hostname,omitempty"`
	SerialNumber   string `json:"serial_number,omitempty"`
	Provider       string `json:"provider"`
	OSName         string `json:"os_name,omitempty"`
	OSVersion      string `json:"os_version,omitempty"`
	UserEmail      string `json:"user_email,omitempty"`
	CoverageBucket string `json:"coverage_bucket"`
	// PolicyState is set only for agent_active devices.
	PolicyState       string                     `json:"policy_state,omitempty"`
	AgentLastSeenAt   string                     `json:"agent_last_seen_at,omitempty"`
	MDMLastCheckInAt  string                     `json:"mdm_last_check_in_at,omitempty"`
	MissingSince      string                     `json:"missing_since,omitempty"`
	ReportedAt        string                     `json:"reported_at,omitempty"`
	AppliedAt         string                     `json:"applied_at,omitempty"`
	AgentVersion      string                     `json:"agent_version,omitempty"`
	EnforcementHalted bool                       `json:"enforcement_halted"`
	BlockedBuild      bool                       `json:"blocked_build"`
	AutoUpdate        string                     `json:"auto_update,omitempty"`
	Tools             []ManagedDeviceToolOutcome `json:"tools,omitempty"`
}

type ListManagedDevicesOutput struct {
	Devices    []ManagedDeviceSummary `json:"devices"`
	NextCursor string                 `json:"next_cursor,omitempty"`
}

func (s *DeviceCoverageService) ListManagedDevices(ctx context.Context, principal Principal, input ListManagedDevicesInput) (ListManagedDevicesOutput, error) {
	if err := s.admit(ctx, principal); err != nil {
		return ListManagedDevicesOutput{}, err
	}
	bucket := strings.TrimSpace(input.CoverageBucket)
	if bucket != "" && !slices.Contains(deviceintegrations.CoverageBuckets, bucket) {
		return ListManagedDevicesOutput{}, ErrDeviceCoverageInvalid
	}
	policyState := strings.TrimSpace(input.PolicyState)
	if policyState != "" && !slices.Contains(deviceintegrations.PolicyStates, policyState) {
		return ListManagedDevicesOutput{}, ErrDeviceCoverageInvalid
	}
	limit := input.Limit
	switch {
	case limit == 0:
		limit = defaultManagedDevicesPage
	case limit < 0 || limit > maxManagedDevicesPage:
		return ListManagedDevicesOutput{}, ErrDeviceCoverageInvalid
	}
	var emails []string
	if email := strings.TrimSpace(input.UserEmail); email != "" {
		emails = []string{email}
	}

	result, err := s.reader.ManagedDevices(ctx, deviceintegrations.ManagedDevicesQuery{
		OrganizationID: principal.OrganizationID,
		Provider:       strings.TrimSpace(input.Provider),
		CoverageBucket: bucket,
		PolicyState:    policyState,
		UserIDs:        nil,
		UserEmails:     emails,
		Cursor:         strings.TrimSpace(input.Cursor),
		Limit:          limit,
	})
	if errors.Is(err, deviceintegrations.ErrInvalidCursor) {
		return ListManagedDevicesOutput{}, ErrDeviceCoverageInvalid
	}
	if err != nil {
		return ListManagedDevicesOutput{}, fmt.Errorf("read managed devices: %w", err)
	}

	devices := make([]ManagedDeviceSummary, 0, len(result.Devices))
	for _, device := range result.Devices {
		if device == nil {
			continue
		}
		devices = append(devices, managedDeviceSummary(device))
	}
	return ListManagedDevicesOutput{Devices: devices, NextCursor: dereferenceString(result.NextCursor)}, nil
}

// managedDeviceSummary keeps what an agent needs to explain a device's state
// and leaves out row and MDM identifiers, which mean nothing outside the
// dashboard.
func managedDeviceSummary(device *gen.ManagedDevice) ManagedDeviceSummary {
	summary := ManagedDeviceSummary{
		Hostname:         dereferenceString(device.Hostname),
		SerialNumber:     dereferenceString(device.SerialNumber),
		Provider:         device.Provider,
		OSName:           dereferenceString(device.OsName),
		OSVersion:        dereferenceString(device.OsVersion),
		UserEmail:        dereferenceString(device.UserEmail),
		CoverageBucket:   device.CoverageBucket,
		PolicyState:      dereferenceString(device.PolicyState),
		AgentLastSeenAt:  dereferenceString(device.AgentLastSeenAt),
		MDMLastCheckInAt: dereferenceString(device.MdmLastCheckInAt),
		MissingSince:     dereferenceString(device.MissingSince),
		// Set below when the agent has reported.
		ReportedAt:        "",
		AppliedAt:         "",
		AgentVersion:      "",
		EnforcementHalted: false,
		BlockedBuild:      false,
		AutoUpdate:        "",
		Tools:             nil,
	}
	state := device.AgentState
	if state == nil {
		return summary
	}
	summary.ReportedAt = state.ReportedAt
	summary.AppliedAt = dereferenceString(state.AppliedAt)
	summary.AgentVersion = dereferenceString(state.SpeakeasydVersion)
	summary.EnforcementHalted = state.EnforcementHalted
	summary.BlockedBuild = state.CurrentBlocked
	summary.AutoUpdate = dereferenceString(state.AutoUpdate)
	for _, tool := range state.Tools {
		if tool == nil {
			continue
		}
		summary.Tools = append(summary.Tools, ManagedDeviceToolOutcome{Tool: tool.Tool, Outcome: tool.Outcome})
	}
	return summary
}
