package metering

import "strings"

// MeterID identifies a workload meter independently of its version.
type MeterID string

// Unit identifies the canonical integral base unit of a meter.
type Unit string

// MeasurementMethod identifies how a quantity was measured.
type MeasurementMethod string

// Definition pins the semantics used to interpret a reading.
type Definition struct {
	// ID identifies the meter.
	ID MeterID

	// Version changes whenever the meter semantics change.
	Version uint32

	// Unit is the canonical integral base unit.
	Unit Unit

	// MeasurementMethod identifies the measurement implementation when applicable.
	MeasurementMethod MeasurementMethod

	scopeKind scopeKind
}

const (
	// MeterAgentSessionStorage measures durable agent-session message storage.
	MeterAgentSessionStorage MeterID = "gram.agent_session.storage"

	// UnitSTokens is the Gram-owned Speakeasy token workload unit.
	UnitSTokens Unit = "stokens"

	// MeasurementTiktokenO200kBase is the canonical s-token measurement method.
	MeasurementTiktokenO200kBase MeasurementMethod = "tiktoken_o200k_base" //nolint:gosec // tokenizer identifier, not a credential
)

// AgentSessionStorage returns the current durable message-storage definition.
func AgentSessionStorage() Definition {
	return Definition{
		ID:                MeterAgentSessionStorage,
		Version:           1,
		Unit:              UnitSTokens,
		MeasurementMethod: MeasurementTiktokenO200kBase,
		scopeKind:         scopeKindProject,
	}
}

// LookupDefinition returns a registered meter definition by identity.
func LookupDefinition(id MeterID, version uint32) (Definition, bool) {
	definition := AgentSessionStorage()
	if id == definition.ID && version == definition.Version {
		return definition, true
	}
	return Definition{
		ID:                "",
		Version:           0,
		Unit:              "",
		MeasurementMethod: "",
		scopeKind:         0,
	}, false
}

func validateDefinition(definition Definition) bool {
	if strings.TrimSpace(string(definition.ID)) == "" || definition.Version == 0 || strings.TrimSpace(string(definition.Unit)) == "" {
		return false
	}
	registered, ok := LookupDefinition(definition.ID, definition.Version)
	return ok && registered == definition
}
