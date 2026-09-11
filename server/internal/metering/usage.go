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
	meterIDs          []string
	unit              string
	measurementMethod string
	defaultBreakdown  string
	breakdowns        map[string]chrepo.UsageFacet
}

var usageFamilySpecs = map[UsageFamily]usageFamilySpec{
	UsageFamilyAgentSessionStorage: {
		meterIDs:          []string{string(MeterAgentSessionStorage)},
		unit:              string(UnitSTokens),
		measurementMethod: string(MeasurementTiktokenO200kBase),
		defaultBreakdown:  "total",
		breakdowns: map[string]chrepo.UsageFacet{
			"total":               {Kind: chrepo.UsageFacetTotal, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: nil},
			"project":             {Kind: chrepo.UsageFacetProject, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: nil},
			"model":               attributeUsageFacet(AttributeModel),
			"provider":            attributeUsageFacet(AttributeProvider),
			"billing_mode":        attributeUsageFacet(AttributeBillingMode),
			"assistant":           attributeUsageFacet(AttributeAssistantID),
			"billing_user":        attributeUsageFacet(AttributeBillingUserID),
			"division":            attributeUsageFacet(AttributeBillingUserDivisionName),
			"department":          attributeUsageFacet(AttributeBillingUserDepartmentName),
			"job_title":           attributeUsageFacet(AttributeBillingUserJobTitle),
			"employee_type":       attributeUsageFacet(AttributeBillingUserEmployeeType),
			"cost_center":         attributeUsageFacet(AttributeBillingUserCostCenterName),
			"directory_group_set": sortedSetUsageFacet(AttributeBillingUserDirectoryGroups),
		},
	},
	UsageFamilyMCPBandwidth: {
		meterIDs: []string{
			string(MeterMCPBandwidthIngress),
			string(MeterMCPBandwidthEgress),
		},
		unit:              string(UnitBytes),
		measurementMethod: string(MeasurementHTTPBodyBytes),
		defaultBreakdown:  "direction",
		breakdowns: map[string]chrepo.UsageFacet{
			"total":   {Kind: chrepo.UsageFacetTotal, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: nil},
			"project": {Kind: chrepo.UsageFacetProject, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: nil},
			"direction": meterUsageFacet(
				chrepo.UsageFacetValue{Source: string(MeterMCPBandwidthIngress), Key: "ingress", Label: "Ingress"},
				chrepo.UsageFacetValue{Source: string(MeterMCPBandwidthEgress), Key: "egress", Label: "Egress"},
			),
			"mcp_server": {
				Kind:               chrepo.UsageFacetMCPServer,
				Attribute:          AttributeMCPServerType,
				SecondaryAttribute: AttributeMCPServerID,
				LabelAttribute:     AttributeMCPServerSlug,
				Values:             nil,
			},
			"server_type": attributeUsageFacet(AttributeMCPServerType),
		},
	},
	UsageFamilyRiskContentScans: {
		meterIDs: []string{
			string(MeterRiskGitleaks),
			string(MeterRiskPresidio),
			string(MeterRiskPromptInjection),
			string(MeterRiskPromptPolicy),
			string(MeterRiskCustomRules),
			string(MeterRiskCLIDestructive),
		},
		unit:              string(UnitSTokens),
		measurementMethod: string(MeasurementTiktokenO200kBase),
		defaultBreakdown:  "scanner",
		breakdowns: map[string]chrepo.UsageFacet{
			"total":   {Kind: chrepo.UsageFacetTotal, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: nil},
			"project": {Kind: chrepo.UsageFacetProject, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: nil},
			"scanner": meterUsageFacet(
				chrepo.UsageFacetValue{Source: string(MeterRiskGitleaks), Key: string(MeterRiskGitleaks), Label: "Gitleaks"},
				chrepo.UsageFacetValue{Source: string(MeterRiskPresidio), Key: string(MeterRiskPresidio), Label: "Presidio"},
				chrepo.UsageFacetValue{Source: string(MeterRiskPromptInjection), Key: string(MeterRiskPromptInjection), Label: "Prompt injection"},
				chrepo.UsageFacetValue{Source: string(MeterRiskPromptPolicy), Key: string(MeterRiskPromptPolicy), Label: "Prompt policy"},
				chrepo.UsageFacetValue{Source: string(MeterRiskCustomRules), Key: string(MeterRiskCustomRules), Label: "Custom rules"},
				chrepo.UsageFacetValue{Source: string(MeterRiskCLIDestructive), Key: string(MeterRiskCLIDestructive), Label: "CLI destructive"},
			),
			"policy":         attributeUsageFacet(AttributeRiskPolicyID),
			"judge_model":    attributeUsageFacet(AttributeModel),
			"judge_provider": attributeUsageFacet(AttributeProvider),
			"tool_name":      attributeUsageFacet(AttributeToolName),
		},
	},
}

func attributeUsageFacet(attribute string) chrepo.UsageFacet {
	return chrepo.UsageFacet{Kind: chrepo.UsageFacetAttribute, Attribute: attribute, SecondaryAttribute: "", LabelAttribute: "", Values: nil}
}

func sortedSetUsageFacet(attribute string) chrepo.UsageFacet {
	return chrepo.UsageFacet{Kind: chrepo.UsageFacetSortedSet, Attribute: attribute, SecondaryAttribute: "", LabelAttribute: "", Values: nil}
}

func meterUsageFacet(values ...chrepo.UsageFacetValue) chrepo.UsageFacet {
	return chrepo.UsageFacet{Kind: chrepo.UsageFacetMeter, Attribute: "", SecondaryAttribute: "", LabelAttribute: "", Values: values}
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
	facet, ok := spec.breakdowns[breakdown]
	if !ok {
		return "", chrepo.UsageSelection{}, ErrInvalidBreakdown
	}
	return breakdown, chrepo.UsageSelection{
		MeterIDs:          spec.meterIDs,
		Unit:              spec.unit,
		MeasurementMethod: spec.measurementMethod,
		Facet:             facet,
	}, nil
}
