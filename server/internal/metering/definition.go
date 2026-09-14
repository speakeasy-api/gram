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
}

const (
	// MeterAgentSessionStorage measures durable agent-session message storage.
	MeterAgentSessionStorage MeterID = "gram.agent_session.storage"

	// MeterMCPBandwidthIngress measures application-visible MCP request body bytes.
	MeterMCPBandwidthIngress MeterID = "gram.mcp.bandwidth.ingress"

	// MeterMCPBandwidthEgress measures application-visible MCP response body bytes.
	MeterMCPBandwidthEgress MeterID = "gram.mcp.bandwidth.egress"

	// MeterRiskGitleaks measures secret-scanner input.
	MeterRiskGitleaks MeterID = "gram.risk.scan.gitleaks"

	// MeterRiskPresidio measures PII-scanner input.
	MeterRiskPresidio MeterID = "gram.risk.scan.presidio"

	// MeterRiskPromptInjection measures prompt-injection scanner input.
	MeterRiskPromptInjection MeterID = "gram.risk.scan.prompt_injection"

	// MeterRiskPromptPolicy measures prompt-policy judge input.
	MeterRiskPromptPolicy MeterID = "gram.risk.scan.prompt_policy"

	// MeterRiskCustomRules measures custom detection rule input.
	MeterRiskCustomRules MeterID = "gram.risk.scan.custom_rules"

	// MeterRiskCLIDestructive measures destructive-command scanner input.
	MeterRiskCLIDestructive MeterID = "gram.risk.scan.cli_destructive"

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
	}
}

// RiskGitleaks returns the secret-scanning meter.
func RiskGitleaks() Definition {
	return riskDefinition(MeterRiskGitleaks)
}

// RiskPresidio returns the PII-scanning meter.
func RiskPresidio() Definition {
	return riskDefinition(MeterRiskPresidio)
}

// RiskPromptInjection returns the prompt-injection scanning meter.
func RiskPromptInjection() Definition {
	return riskDefinition(MeterRiskPromptInjection)
}

// RiskPromptPolicy returns the prompt-policy scanning meter.
func RiskPromptPolicy() Definition {
	return riskDefinition(MeterRiskPromptPolicy)
}

// RiskCustomRules returns the custom-rule scanning meter.
func RiskCustomRules() Definition {
	return riskDefinition(MeterRiskCustomRules)
}

// RiskCLIDestructive returns the destructive-command scanning meter.
func RiskCLIDestructive() Definition {
	return riskDefinition(MeterRiskCLIDestructive)
}

func riskDefinition(id MeterID) Definition {
	return Definition{
		id:                id,
		version:           1,
		unit:              UnitSTokens,
		measurementMethod: MeasurementTiktokenO200kBase,
		scopeKind:         scopeKindProject,
	}
}

// LookupDefinition returns a registered meter definition by identity.
func LookupDefinition(id MeterID, version uint32) (Definition, bool) {
	for _, definition := range [...]Definition{
		AgentSessionStorage(),
		MCPBandwidthIngress(),
		MCPBandwidthEgress(),
		RiskGitleaks(),
		RiskPresidio(),
		RiskPromptInjection(),
		RiskPromptPolicy(),
		RiskCustomRules(),
		RiskCLIDestructive(),
	} {
		if id == definition.id && version == definition.version {
			return definition, true
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
