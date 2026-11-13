// Code generated from registry.Definitions by authzgen. DO NOT EDIT.
// Regenerate with `mise run gen:authz`.

package authz

import "github.com/google/uuid"

// Id types, one per resource kind. A typed ref takes these, never a raw
// string, so ids of different kinds cannot be swapped. An id built from an
// empty or reserved value is absent, and every check built from it is refused.

// OrgID is the id of one org resource in a check.
type OrgID struct{ id Option[string] }

// OrgIDOf is the OrgID for a uuid. The nil uuid is absent.
func OrgIDOf(id uuid.UUID) OrgID { return OrgID{id: idFromUUID(id)} }

// OrgIDFrom is the OrgID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func OrgIDFrom(id string) OrgID { return OrgID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i OrgID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same org. It fails with
// ErrEmptyValue when either is absent.
func (i OrgID) Equals(other OrgID) (bool, error) { return i.id.Equals(other.id) }

// ProjectID is the id of one project resource in a check.
type ProjectID struct{ id Option[string] }

// ProjectIDOf is the ProjectID for a uuid. The nil uuid is absent.
func ProjectIDOf(id uuid.UUID) ProjectID { return ProjectID{id: idFromUUID(id)} }

// ProjectIDFrom is the ProjectID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func ProjectIDFrom(id string) ProjectID { return ProjectID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i ProjectID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same project. It fails with
// ErrEmptyValue when either is absent.
func (i ProjectID) Equals(other ProjectID) (bool, error) { return i.id.Equals(other.id) }

// MCPServerID is the id of one mcp resource in a check.
type MCPServerID struct{ id Option[string] }

// MCPServerIDOf is the MCPServerID for a uuid. The nil uuid is absent.
func MCPServerIDOf(id uuid.UUID) MCPServerID { return MCPServerID{id: idFromUUID(id)} }

// MCPServerIDFrom is the MCPServerID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func MCPServerIDFrom(id string) MCPServerID { return MCPServerID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i MCPServerID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same mcp. It fails with
// ErrEmptyValue when either is absent.
func (i MCPServerID) Equals(other MCPServerID) (bool, error) { return i.id.Equals(other.id) }

// EnvironmentID is the id of one environment resource in a check.
type EnvironmentID struct{ id Option[string] }

// EnvironmentIDOf is the EnvironmentID for a uuid. The nil uuid is absent.
func EnvironmentIDOf(id uuid.UUID) EnvironmentID { return EnvironmentID{id: idFromUUID(id)} }

// EnvironmentIDFrom is the EnvironmentID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func EnvironmentIDFrom(id string) EnvironmentID { return EnvironmentID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i EnvironmentID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same environment. It fails with
// ErrEmptyValue when either is absent.
func (i EnvironmentID) Equals(other EnvironmentID) (bool, error) { return i.id.Equals(other.id) }

// SkillID is the id of one skill resource in a check.
type SkillID struct{ id Option[string] }

// SkillIDOf is the SkillID for a uuid. The nil uuid is absent.
func SkillIDOf(id uuid.UUID) SkillID { return SkillID{id: idFromUUID(id)} }

// SkillIDFrom is the SkillID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func SkillIDFrom(id string) SkillID { return SkillID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i SkillID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same skill. It fails with
// ErrEmptyValue when either is absent.
func (i SkillID) Equals(other SkillID) (bool, error) { return i.id.Equals(other.id) }

// AssistantID is the id of one assistant resource in a check.
type AssistantID struct{ id Option[string] }

// AssistantIDOf is the AssistantID for a uuid. The nil uuid is absent.
func AssistantIDOf(id uuid.UUID) AssistantID { return AssistantID{id: idFromUUID(id)} }

// AssistantIDFrom is the AssistantID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func AssistantIDFrom(id string) AssistantID { return AssistantID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i AssistantID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same assistant. It fails with
// ErrEmptyValue when either is absent.
func (i AssistantID) Equals(other AssistantID) (bool, error) { return i.id.Equals(other.id) }

// ChatID is the id of one chat resource in a check.
type ChatID struct{ id Option[string] }

// ChatIDOf is the ChatID for a uuid. The nil uuid is absent.
func ChatIDOf(id uuid.UUID) ChatID { return ChatID{id: idFromUUID(id)} }

// ChatIDFrom is the ChatID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func ChatIDFrom(id string) ChatID { return ChatID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i ChatID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same chat. It fails with
// ErrEmptyValue when either is absent.
func (i ChatID) Equals(other ChatID) (bool, error) { return i.id.Equals(other.id) }

// AgentID is the id of one agent resource in a check.
type AgentID struct{ id Option[string] }

// AgentIDOf is the AgentID for a uuid. The nil uuid is absent.
func AgentIDOf(id uuid.UUID) AgentID { return AgentID{id: idFromUUID(id)} }

// AgentIDFrom is the AgentID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func AgentIDFrom(id string) AgentID { return AgentID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i AgentID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same agent. It fails with
// ErrEmptyValue when either is absent.
func (i AgentID) Equals(other AgentID) (bool, error) { return i.id.Equals(other.id) }

// WorkloadID is the id of one workload resource in a check.
type WorkloadID struct{ id Option[string] }

// WorkloadIDOf is the WorkloadID for a uuid. The nil uuid is absent.
func WorkloadIDOf(id uuid.UUID) WorkloadID { return WorkloadID{id: idFromUUID(id)} }

// WorkloadIDFrom is the WorkloadID for an id held as a string. An empty, wildcard or
// reserved value is absent.
func WorkloadIDFrom(id string) WorkloadID { return WorkloadID{id: idFromString(id)} }

// String is the id as checks and grants carry it, empty when absent.
func (i WorkloadID) String() string { return idValue(i.id) }

// Equals reports whether both ids name the same workload. It fails with
// ErrEmptyValue when either is absent.
func (i WorkloadID) Equals(other WorkloadID) (bool, error) { return i.id.Equals(other.id) }
