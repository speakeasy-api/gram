package metering

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type readingKind uint8

const (
	readingKindUsage readingKind = iota + 1
	readingKindAdjustment
)

type scopeKind uint8

const (
	scopeKindOrganization scopeKind = iota + 1
	scopeKindProject
)

// AttributeChatID identifies the chat containing the metered message.
const AttributeChatID = "chat_id"

// AttributeModel identifies the model that produced or received the message.
const AttributeModel = "model"

// AttributeHookSource identifies the canonical agent source.
const AttributeHookSource = "hook_source"

// AttributeMessageUserID identifies the Gram user attached to the message.
const AttributeMessageUserID = "message_user_id"

// AttributeMessageExternalUserID preserves the message actor's opaque external ID.
const AttributeMessageExternalUserID = "message_external_user_id"

// AttributeMessageUserEmail preserves the email explicitly observed by the producer.
const AttributeMessageUserEmail = "message_user_email"

// AttributeMessageUserAccountEmail is the current Gram account email for the message user.
const AttributeMessageUserAccountEmail = "message_user_account_email"

// AttributeChatOwnerUserID identifies the Gram user that owns the chat.
const AttributeChatOwnerUserID = "chat_owner_user_id"

// AttributeChatOwnerExternalUserID preserves the chat owner's opaque external ID.
const AttributeChatOwnerExternalUserID = "chat_owner_external_user_id"

// AttributeChatOwnerUserEmail is the current Gram account email for the chat owner.
const AttributeChatOwnerUserEmail = "chat_owner_user_email"

// AttributeMessageUserDivisionName identifies the message user's active directory division.
const AttributeMessageUserDivisionName = "message_user_division_name"

// AttributeMessageUserDepartmentName identifies the message user's active directory department.
const AttributeMessageUserDepartmentName = "message_user_department_name"

// AttributeMessageUserDirectoryMatch records how the message user's directory profile matched.
const AttributeMessageUserDirectoryMatch = "message_user_directory_match"

// AttributeMessageUserRBACRoles contains the message user's sorted role slugs as JSON.
const AttributeMessageUserRBACRoles = "message_user_rbac_roles"

// AttributeChatOwnerDivisionName identifies the chat owner's active directory division.
const AttributeChatOwnerDivisionName = "chat_owner_division_name"

// AttributeChatOwnerDepartmentName identifies the chat owner's active directory department.
const AttributeChatOwnerDepartmentName = "chat_owner_department_name"

// AttributeChatOwnerDirectoryMatch records how the chat owner's directory profile matched.
const AttributeChatOwnerDirectoryMatch = "chat_owner_directory_match"

// AttributeChatOwnerRBACRoles contains the chat owner's sorted role slugs as JSON.
const AttributeChatOwnerRBACRoles = "chat_owner_rbac_roles"

// Scope identifies the owner responsible for workload.
type Scope struct {
	kind           scopeKind
	organizationID string
	projectID      uuid.UUID
}

// OrganizationScope creates an organization-owned workload scope.
func OrganizationScope(id string) Scope {
	return Scope{
		kind:           scopeKindOrganization,
		organizationID: id,
		projectID:      uuid.Nil,
	}
}

// ProjectScope creates a project-owned workload scope.
func ProjectScope(organizationID string, projectID uuid.UUID) Scope {
	return Scope{
		kind:           scopeKindProject,
		organizationID: organizationID,
		projectID:      projectID,
	}
}

// OrganizationID returns the organization responsible for the scope.
func (s Scope) OrganizationID() string {
	return s.organizationID
}

// ProjectID returns the project when the scope is project-owned.
func (s Scope) ProjectID() (uuid.UUID, bool) {
	return s.projectID, s.kind == scopeKindProject
}

// UsageInput describes one positive quantity measured by a meter.
type UsageInput struct {
	// Meter pins the semantics of the quantity.
	Meter Definition

	// Scope identifies the workload owner.
	Scope Scope

	// OperationID identifies the domain operation within the meter.
	OperationID string

	// Value is a positive quantity in the meter's canonical base unit.
	Value int64

	// OccurredAt assigns the quantity to a billing-effective UTC time.
	OccurredAt time.Time

	// ProducedAt is the UTC time the source produced the reading.
	ProducedAt time.Time

	// Source identifies the producer subsystem for diagnostics.
	Source string

	// Attributes contains non-authoritative diagnostic dimensions.
	Attributes map[string]string
}

// AdjustmentInput describes a signed correction to an earlier reading.
type AdjustmentInput struct {
	// Meter pins the semantics of the quantity.
	Meter Definition

	// Scope identifies the workload owner.
	Scope Scope

	// OperationID identifies the domain operation within the meter.
	OperationID string

	// Value is a non-zero signed quantity in the meter's canonical base unit.
	Value int64

	// OccurredAt assigns the adjustment to a billing-effective UTC time.
	OccurredAt time.Time

	// ProducedAt is the UTC time the source produced the adjustment.
	ProducedAt time.Time

	// CorrectsReadingID identifies the reading affected by this adjustment.
	CorrectsReadingID uuid.UUID

	// Reason is a machine-readable explanation for the adjustment.
	Reason string

	// Source identifies the producer subsystem for diagnostics.
	Source string

	// Attributes contains non-authoritative diagnostic dimensions.
	Attributes map[string]string
}

// Reading is an immutable, validated workload quantity ready for publication.
type Reading struct {
	meter             Definition
	scope             Scope
	operationID       string
	value             int64
	occurredAt        time.Time
	producedAt        time.Time
	kind              readingKind
	correctsReadingID uuid.UUID
	adjustmentReason  string
	source            string
	attributes        map[string]string
}

// NewUsage validates and constructs an ordinary usage reading.
func NewUsage(input UsageInput) (Reading, error) {
	reading := Reading{
		meter:             input.Meter,
		scope:             input.Scope,
		operationID:       input.OperationID,
		value:             input.Value,
		occurredAt:        input.OccurredAt,
		producedAt:        input.ProducedAt,
		kind:              readingKindUsage,
		correctsReadingID: uuid.Nil,
		adjustmentReason:  "",
		source:            input.Source,
		attributes:        maps.Clone(input.Attributes),
	}
	if err := reading.validate(); err != nil {
		return Reading{}, err
	}
	return reading, nil
}

// NewAdjustment validates and constructs a signed adjustment reading.
func NewAdjustment(input AdjustmentInput) (Reading, error) {
	reading := Reading{
		meter:             input.Meter,
		scope:             input.Scope,
		operationID:       input.OperationID,
		value:             input.Value,
		occurredAt:        input.OccurredAt,
		producedAt:        input.ProducedAt,
		kind:              readingKindAdjustment,
		correctsReadingID: input.CorrectsReadingID,
		adjustmentReason:  input.Reason,
		source:            input.Source,
		attributes:        maps.Clone(input.Attributes),
	}
	if err := reading.validate(); err != nil {
		return Reading{}, err
	}
	return reading, nil
}

func validateReadingInput(meter Definition, scope Scope, operationID string, occurredAt time.Time, producedAt time.Time) error {
	if !validateDefinition(meter) {
		return fmt.Errorf("unknown or inconsistent meter definition")
	}
	if strings.TrimSpace(scope.organizationID) == "" {
		return fmt.Errorf("reading organization id must not be empty")
	}
	switch scope.kind {
	case scopeKindOrganization:
		if scope.projectID != uuid.Nil {
			return fmt.Errorf("organization scope project id must be zero")
		}
	case scopeKindProject:
		if scope.projectID == uuid.Nil {
			return fmt.Errorf("project scope project id must not be zero")
		}
	default:
		return fmt.Errorf("reading scope must be constructed by metering")
	}
	if scope.kind != meter.scopeKind {
		return fmt.Errorf("reading scope does not match meter definition")
	}
	if strings.TrimSpace(operationID) == "" {
		return fmt.Errorf("reading operation id must not be empty")
	}
	if occurredAt.IsZero() || occurredAt.Location() != time.UTC {
		return fmt.Errorf("reading occurred at must be a nonzero UTC timestamp")
	}
	if producedAt.IsZero() || producedAt.Location() != time.UTC {
		return fmt.Errorf("reading produced at must be a nonzero UTC timestamp")
	}
	return nil
}

func (r Reading) validate() error {
	if err := validateReadingInput(r.meter, r.scope, r.operationID, r.occurredAt, r.producedAt); err != nil {
		return err
	}
	switch r.kind {
	case readingKindUsage:
		if r.value <= 0 {
			return fmt.Errorf("usage value must be positive")
		}
		if r.correctsReadingID != uuid.Nil || r.adjustmentReason != "" {
			return fmt.Errorf("usage must not contain adjustment fields")
		}
	case readingKindAdjustment:
		if r.value == 0 {
			return fmt.Errorf("adjustment value must not be zero")
		}
		if r.correctsReadingID == uuid.Nil {
			return fmt.Errorf("adjustment correction id must not be zero")
		}
		if strings.TrimSpace(r.adjustmentReason) == "" {
			return fmt.Errorf("adjustment reason must not be empty")
		}
		if r.ID() == r.correctsReadingID {
			return fmt.Errorf("adjustment must not correct itself")
		}
	default:
		return fmt.Errorf("reading kind is invalid")
	}
	return nil
}

// ID returns the deterministic domain identity of the reading.
func (r Reading) ID() uuid.UUID {
	preimage := "gram:meter-reading:v1\x00" +
		r.scope.organizationID + "\x00" +
		r.scope.projectID.String() + "\x00" +
		string(r.meter.id) + "\x00" +
		strconv.FormatUint(uint64(r.meter.version), 10) + "\x00" +
		r.operationID
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(preimage))
}
