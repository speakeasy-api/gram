package metering

import (
	"errors"

	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

// UsageFamily identifies one compatible group of reporting meters.
type UsageFamily string

const (
	// UsageFamilyAgentSessionStorage selects durable agent-session storage.
	UsageFamilyAgentSessionStorage UsageFamily = "agent_session_storage"

	// UsageFamilyMCPBandwidth selects MCP ingress and egress body bytes.
	UsageFamilyMCPBandwidth UsageFamily = "mcp_bandwidth"

	// UsageFamilyRiskContentScans selects all six risk content scanners.
	UsageFamilyRiskContentScans UsageFamily = "risk_content_scans"
)

var (
	// ErrInvalidUsageFamily reports an unknown reporting family.
	ErrInvalidUsageFamily = errors.New("invalid meter usage family")

	// ErrInvalidBreakdown reports a facet unsupported by the selected family.
	ErrInvalidBreakdown = errors.New("invalid meter usage breakdown")
)

type usageFamilySpec struct {
	unit              string
	measurementMethod string
	defaultBreakdown  string
	breakdowns        map[string]struct{}
}

var usageFamilySpecs = map[UsageFamily]usageFamilySpec{
	UsageFamilyAgentSessionStorage: {
		unit:              string(UnitSTokens),
		measurementMethod: string(MeasurementTiktokenO200kBase),
		defaultBreakdown:  "total",
		breakdowns: map[string]struct{}{
			"total": {}, "project": {}, "model": {}, "provider": {},
			"billing_mode": {}, "assistant": {}, "billing_user": {},
			"division": {}, "department": {}, "job_title": {},
			"employee_type": {}, "cost_center": {}, "directory_group_set": {},
		},
	},
	UsageFamilyMCPBandwidth: {
		unit:              string(UnitBytes),
		measurementMethod: string(MeasurementHTTPBodyBytes),
		defaultBreakdown:  "direction",
		breakdowns: map[string]struct{}{
			"total": {}, "project": {}, "direction": {}, "mcp_server": {}, "server_type": {},
		},
	},
	UsageFamilyRiskContentScans: {
		unit:              string(UnitSTokens),
		measurementMethod: string(MeasurementTiktokenO200kBase),
		defaultBreakdown:  "scanner",
		breakdowns: map[string]struct{}{
			"total": {}, "project": {}, "scanner": {}, "policy": {},
			"judge_model": {}, "judge_provider": {}, "tool_name": {},
		},
	},
}

// ResolveUsageSelection validates the API family/facet pair and returns the
// exact internal repository selection plus the resolved default facet name.
func ResolveUsageSelection(family UsageFamily, breakdown string) (string, chrepo.UsageSelection, error) {
	spec, ok := usageFamilySpecs[family]
	if !ok {
		return "", chrepo.UsageSelection{}, ErrInvalidUsageFamily
	}
	if breakdown == "" {
		breakdown = spec.defaultBreakdown
	}
	if _, ok := spec.breakdowns[breakdown]; !ok {
		return "", chrepo.UsageSelection{}, ErrInvalidBreakdown
	}
	return breakdown, chrepo.UsageSelection{
		Family:            string(family),
		Breakdown:         breakdown,
		Unit:              spec.unit,
		MeasurementMethod: spec.measurementMethod,
	}, nil
}
