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

// LookupDefinition returns a registered meter definition by identity.
func LookupDefinition(id MeterID, version uint32) (Definition, bool) {
	for _, definition := range [...]Definition{
		AgentSessionStorage(),
		MCPBandwidthIngress(),
		MCPBandwidthEgress(),
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
