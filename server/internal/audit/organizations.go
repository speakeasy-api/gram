package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	organizationsgen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionOrganizationInviteCreate     Action = "organization_invitation:create"
	ActionOrganizationInviteRevoke     Action = "organization_invitation:revoke"
	ActionOrganizationInviteRoleUpdate Action = "organization_invitation:update_role"
	ActionOrganizationWebhooksEnabled  Action = "organization:webhooks_enabled"
	ActionOrganizationWebhooksDisabled Action = "organization:webhooks_disabled"

	ActionOrganizationHooksFailOpenEnabled  Action = "organization:hooks_fail_open_enabled"
	ActionOrganizationHooksFailOpenDisabled Action = "organization:hooks_fail_open_disabled"

	ActionOrganizationDeviceAgentConfigurationUpdated Action = "organization:device_agent_configuration_updated"

	ActionOrganizationEnterpriseTrialArmed Action = "organization:enterprise_trial_armed"

	ActionOrganizationEnterpriseTrialDemoted Action = "organization:enterprise_trial_demoted"
	ActionOrganizationEnterpriseTrialRearmed Action = "organization:enterprise_trial_rearmed"
	ActionOrganizationPaygActivated          Action = "organization:payg_activated"
	ActionOrganizationPaygDeactivated        Action = "organization:payg_deactivated"
)

type LogOrganizationInviteCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InvitationURN urn.OrganizationInvitation
	InviteeEmail  string
	RoleSlug      *string
}

func (l *Logger) LogOrganizationInviteCreate(ctx context.Context, dbtx repo.DBTX, event LogOrganizationInviteCreateEvent) error {
	action := ActionOrganizationInviteCreate

	metadata, err := marshalAuditPayload(map[string]any{
		"role_slug": event.RoleSlug,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.InvitationURN.ID.String(),
		SubjectType:        string(subjectTypeOrganizationInvite),
		SubjectDisplayName: conv.ToPGTextEmpty(event.InviteeEmail),
		SubjectSlug:        conv.ToPGTextEmpty(event.InviteeEmail),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationInviteV1})
}

type LogOrganizationInviteRevokeEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InvitationURN urn.OrganizationInvitation
	InviteeEmail  string

	InvitationSnapshotBefore *organizationsgen.OrganizationInvitation
	InvitationSnapshotAfter  *organizationsgen.OrganizationInvitation
}

func (l *Logger) LogOrganizationInviteRevoke(ctx context.Context, dbtx repo.DBTX, event LogOrganizationInviteRevokeEvent) error {
	action := ActionOrganizationInviteRevoke

	beforeSnapshot, err := marshalAuditPayload(event.InvitationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.InvitationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.InvitationURN.ID.String(),
		SubjectType:        string(subjectTypeOrganizationInvite),
		SubjectDisplayName: conv.ToPGTextEmpty(event.InviteeEmail),
		SubjectSlug:        conv.ToPGTextEmpty(event.InviteeEmail),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationInviteV1})
}

type LogOrganizationInviteRoleUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	InvitationURN urn.OrganizationInvitation
	InviteeEmail  string

	InvitationSnapshotBefore *organizationsgen.OrganizationInvitation
	InvitationSnapshotAfter  *organizationsgen.OrganizationInvitation
}

func (l *Logger) LogOrganizationInviteRoleUpdate(ctx context.Context, dbtx repo.DBTX, event LogOrganizationInviteRoleUpdateEvent) error {
	action := ActionOrganizationInviteRoleUpdate

	beforeSnapshot, err := marshalAuditPayload(event.InvitationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.InvitationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.InvitationURN.ID.String(),
		SubjectType:        string(subjectTypeOrganizationInvite),
		SubjectDisplayName: conv.ToPGTextEmpty(event.InviteeEmail),
		SubjectSlug:        conv.ToPGTextEmpty(event.InviteeEmail),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationInviteV1})
}

type LogOrganizationWebhooksToggledEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	WebhooksEnabled bool
}

func (l *Logger) LogOrganizationWebhooksToggled(ctx context.Context, dbtx repo.DBTX, event LogOrganizationWebhooksToggledEvent) error {
	var action Action
	if event.WebhooksEnabled {
		action = ActionOrganizationWebhooksEnabled
	} else {
		action = ActionOrganizationWebhooksDisabled
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationWebhooksV1})
}

type LogOrganizationHooksFailOpenToggledEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	FailOpenEnabled bool
}

func (l *Logger) LogOrganizationHooksFailOpenToggled(ctx context.Context, dbtx repo.DBTX, event LogOrganizationHooksFailOpenToggledEvent) error {
	var action Action
	if event.FailOpenEnabled {
		action = ActionOrganizationHooksFailOpenEnabled
	} else {
		action = ActionOrganizationHooksFailOpenDisabled
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationHooksFailOpenV1})
}

type DeviceAgentConfigurationSnapshot struct {
	SchemaVersion int32          `json:"schema_version"`
	Config        map[string]any `json:"config"`
}

type LogOrganizationDeviceAgentConfigurationUpdatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationSlug string

	DeviceAgentConfigurationSnapshotBefore *DeviceAgentConfigurationSnapshot
	DeviceAgentConfigurationSnapshotAfter  *DeviceAgentConfigurationSnapshot
}

func (l *Logger) LogOrganizationDeviceAgentConfigurationUpdated(
	ctx context.Context,
	dbtx repo.DBTX,
	event LogOrganizationDeviceAgentConfigurationUpdatedEvent,
) error {
	beforeSnapshot, err := marshalAuditPayload(event.DeviceAgentConfigurationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionOrganizationDeviceAgentConfigurationUpdated, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.DeviceAgentConfigurationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionOrganizationDeviceAgentConfigurationUpdated, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionOrganizationDeviceAgentConfigurationUpdated),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationSlug),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{
		Params:      entry,
		OutboxEvent: events.OrganizationDeviceAgentConfigurationV1,
	})
}

type LogOrganizationEnterpriseTrialArmedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	TrialEndsAt time.Time
}

func (l *Logger) LogOrganizationEnterpriseTrialArmed(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialArmedEvent) error {
	action := ActionOrganizationEnterpriseTrialArmed

	metadata, err := marshalAuditPayload(map[string]any{
		"trial_ends_at": event.TrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationEnterpriseTrialV1})
}

// LogOrganizationEnterpriseTrialRearmedEvent records an operator putting a
// demoted trial back on. AccountType carries the restored tier so a reader can
// compare it with the demotion entry, which is the only record of the old one.
type LogOrganizationEnterpriseTrialRearmedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	AccountType string
	TrialEndsAt time.Time
}

func (l *Logger) LogOrganizationEnterpriseTrialRearmed(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialRearmedEvent) error {
	action := ActionOrganizationEnterpriseTrialRearmed

	metadata, err := marshalAuditPayload(map[string]any{
		"account_type":  event.AccountType,
		"trial_ends_at": event.TrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationEnterpriseTrialV1})
}

type LogOrganizationEnterpriseTrialDemotedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	PreviousAccountType string
	TrialEndsAt         time.Time
}

func (l *Logger) LogOrganizationEnterpriseTrialDemoted(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialDemotedEvent) error {
	action := ActionOrganizationEnterpriseTrialDemoted

	metadata, err := marshalAuditPayload(map[string]any{
		"previous_account_type": event.PreviousAccountType,
		"trial_ends_at":         event.TrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationEnterpriseTrialV1})
}

type OrganizationPaygActivationSnapshot struct {
	AccountType string `json:"account_type"`
	Whitelisted bool   `json:"whitelisted"`
}

type LogOrganizationPaygActivatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	OrganizationSnapshotBefore *OrganizationPaygActivationSnapshot
	OrganizationSnapshotAfter  *OrganizationPaygActivationSnapshot
}

func (l *Logger) LogOrganizationPaygActivated(ctx context.Context, dbtx repo.DBTX, event LogOrganizationPaygActivatedEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.OrganizationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionOrganizationPaygActivated, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.OrganizationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionOrganizationPaygActivated, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionOrganizationPaygActivated),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationBillingV1})
}

type LogOrganizationPaygDeactivatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	OrganizationSnapshotBefore *OrganizationPaygActivationSnapshot
	OrganizationSnapshotAfter  *OrganizationPaygActivationSnapshot
}

func (l *Logger) LogOrganizationPaygDeactivated(ctx context.Context, dbtx repo.DBTX, event LogOrganizationPaygDeactivatedEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.OrganizationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionOrganizationPaygDeactivated, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.OrganizationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionOrganizationPaygDeactivated, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionOrganizationPaygDeactivated),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationBillingV1})
}
