package metering

import "strings"

// MeterID identifies a workload meter independently of its version.
type MeterID string

// Unit identifies the canonical integral base unit of a meter.
type Unit string

// MeasurementMethod identifies how a quantity was measured.
type MeasurementMethod string

// Definition is an opaque, registered meter contract.
type Definition struct {
	id                MeterID
	version           uint32
	unit              Unit
	measurementMethod MeasurementMethod
	scopeKind         scopeKind
	stripeExportable  bool
}

const (
	// MeterAgentSessionStorage measures durable agent-session message storage.
	MeterAgentSessionStorage MeterID = "gram.agent_session.storage"

	// MeterMCPBandwidthIngress measures application-visible MCP request body bytes.
	MeterMCPBandwidthIngress MeterID = "gram.mcp.bandwidth.ingress"

	// MeterMCPBandwidthEgress measures application-visible MCP response body bytes.
	MeterMCPBandwidthEgress MeterID = "gram.mcp.bandwidth.egress"

	// Risk evaluation meters measure detector-qualified scanned volume.
	MeterRiskGitleaksRealtime        MeterID = "gram.risk.evaluation.gitleaks.realtime"
	MeterRiskGitleaksBatch           MeterID = "gram.risk.evaluation.gitleaks.batch"
	MeterRiskGitleaksShadow          MeterID = "gram.risk.evaluation.gitleaks.shadow"
	MeterRiskPresidioRealtime        MeterID = "gram.risk.evaluation.presidio.realtime"
	MeterRiskPresidioBatch           MeterID = "gram.risk.evaluation.presidio.batch"
	MeterRiskPresidioShadow          MeterID = "gram.risk.evaluation.presidio.shadow"
	MeterRiskPromptInjectionRealtime MeterID = "gram.risk.evaluation.prompt_injection.realtime"
	MeterRiskPromptInjectionBatch    MeterID = "gram.risk.evaluation.prompt_injection.batch"
	MeterRiskPromptInjectionShadow   MeterID = "gram.risk.evaluation.prompt_injection.shadow"
	MeterRiskPromptPolicyRealtime    MeterID = "gram.risk.evaluation.prompt_policy.realtime"
	MeterRiskPromptPolicyBatch       MeterID = "gram.risk.evaluation.prompt_policy.batch"
	MeterRiskPromptPolicyShadow      MeterID = "gram.risk.evaluation.prompt_policy.shadow"
	MeterRiskCustomRulesRealtime     MeterID = "gram.risk.evaluation.custom_rules.realtime"
	MeterRiskCustomRulesBatch        MeterID = "gram.risk.evaluation.custom_rules.batch"
	MeterRiskCustomRulesShadow       MeterID = "gram.risk.evaluation.custom_rules.shadow"

	// UnitSTokens is the Gram-owned Speakeasy token workload unit.
	UnitSTokens Unit = "stokens"

	// UnitBytes is the byte workload unit.
	UnitBytes Unit = "bytes"

	// MeasurementTiktokenO200kBase is the canonical s-token measurement method.
	MeasurementTiktokenO200kBase MeasurementMethod = "tiktoken_o200k_base" //nolint:gosec // tokenizer identifier, not a credential

	// MeasurementHTTPBodyBytes counts bytes returned by HTTP body reads and writes.
	MeasurementHTTPBodyBytes MeasurementMethod = "http_body_bytes"
)

// AgentSessionStorage returns the current durable message-storage definition.
func AgentSessionStorage() Definition {
	return Definition{
		id:                MeterAgentSessionStorage,
		version:           1,
		unit:              UnitSTokens,
		measurementMethod: MeasurementTiktokenO200kBase,
		scopeKind:         scopeKindProject,
		stripeExportable:  true,
	}
}

// MCPBandwidthIngress returns the current MCP request-bandwidth definition.
func MCPBandwidthIngress() Definition {
	return Definition{
		id:                MeterMCPBandwidthIngress,
		version:           1,
		unit:              UnitBytes,
		measurementMethod: MeasurementHTTPBodyBytes,
		scopeKind:         scopeKindProject,
		stripeExportable:  true,
	}
}

// MCPBandwidthEgress returns the current MCP response-bandwidth definition.
func MCPBandwidthEgress() Definition {
	return Definition{
		id:                MeterMCPBandwidthEgress,
		version:           1,
		unit:              UnitBytes,
		measurementMethod: MeasurementHTTPBodyBytes,
		scopeKind:         scopeKindProject,
		stripeExportable:  true,
	}
}

// RiskEvaluationDefinition returns the registered detector-and-mode workload definition.
func RiskEvaluationDefinition(detector, executionMode string) (Definition, bool) {
	id, ok := RiskEvaluationMeterID(detector, executionMode)
	if !ok {
		var zero Definition
		return zero, false
	}
	return Definition{
		id:                id,
		version:           1,
		unit:              UnitSTokens,
		measurementMethod: MeasurementTiktokenO200kBase,
		scopeKind:         scopeKindProject,
		stripeExportable:  false,
	}, true
}

// RiskEvaluationMeterID returns the registered meter identity for a detector and mode.
func RiskEvaluationMeterID(detector, executionMode string) (MeterID, bool) {
	var ids [3]MeterID
	switch detector {
	case "gitleaks":
		ids = [3]MeterID{MeterRiskGitleaksRealtime, MeterRiskGitleaksBatch, MeterRiskGitleaksShadow}
	case "presidio":
		ids = [3]MeterID{MeterRiskPresidioRealtime, MeterRiskPresidioBatch, MeterRiskPresidioShadow}
	case "prompt_injection":
		ids = [3]MeterID{MeterRiskPromptInjectionRealtime, MeterRiskPromptInjectionBatch, MeterRiskPromptInjectionShadow}
	case "prompt_policy":
		ids = [3]MeterID{MeterRiskPromptPolicyRealtime, MeterRiskPromptPolicyBatch, MeterRiskPromptPolicyShadow}
	case "custom_rules":
		ids = [3]MeterID{MeterRiskCustomRulesRealtime, MeterRiskCustomRulesBatch, MeterRiskCustomRulesShadow}
	default:
		return "", false
	}

	switch executionMode {
	case "realtime":
		return ids[0], true
	case "batch":
		return ids[1], true
	case "shadow":
		return ids[2], true
	default:
		return "", false
	}
}

// LookupDefinition returns a registered meter definition by identity.
func LookupDefinition(id MeterID, version uint32) (Definition, bool) {
	for _, definition := range [...]Definition{AgentSessionStorage(), MCPBandwidthIngress(), MCPBandwidthEgress()} {
		if id == definition.id && version == definition.version {
			return definition, true
		}
	}
	for _, detector := range [...]string{"gitleaks", "presidio", "prompt_injection", "prompt_policy", "custom_rules"} {
		for _, mode := range [...]string{"realtime", "batch", "shadow"} {
			definition, _ := RiskEvaluationDefinition(detector, mode)
			if id == definition.id && version == definition.version {
				return definition, true
			}
		}
	}
	var zero Definition
	return zero, false
}

func validateDefinition(definition Definition) bool {
	if strings.TrimSpace(string(definition.id)) == "" || definition.version == 0 || strings.TrimSpace(string(definition.unit)) == "" {
		return false
	}
	registered, ok := LookupDefinition(definition.id, definition.version)
	return ok && registered == definition
}
