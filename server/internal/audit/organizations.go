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

	ActionOrganizationProductFeatureEnabled      Action = "organization:product_feature_enabled"
	ActionOrganizationProductFeatureDisabled     Action = "organization:product_feature_disabled"
	ActionOrganizationSetupTaskUpdated           Action = "organization:setup_task_updated"
	ActionOrganizationOnboardingUpdated          Action = "organization:onboarding_updated"
	ActionOrganizationOnboardingStackUpdated     Action = "organization:onboarding_stack_updated"
	ActionOrganizationOnboardingPlaybookAssigned Action = "organization:onboarding_playbook_assigned"
	// ActionOrganizationOnboardingPlaybookUnassigned records the assignment
	// being cleared: the organization walks its saved selection again.
	ActionOrganizationOnboardingPlaybookUnassigned Action = "organization:onboarding_playbook_unassigned"

	ActionOrganizationDeviceAgentConfigurationUpdated Action = "organization:device_agent_configuration_updated"

	ActionOrganizationEnterpriseTrialArmed Action = "organization:enterprise_trial_armed"

	ActionOrganizationEnterpriseTrialDemoted    Action = "organization:enterprise_trial_demoted"
	ActionOrganizationEnterpriseTrialRearmed    Action = "organization:enterprise_trial_rearmed"
	ActionOrganizationEnterpriseTrialExtended   Action = "organization:enterprise_trial_extended"
	ActionOrganizationInferenceKeyRepaired      Action = "organization:inference_key_repaired"
	ActionOrganizationAccountTypeChanged        Action = "organization:account_type_changed"
	ActionOrganizationEnterpriseTrialConverted  Action = "organization:enterprise_trial_converted"
	ActionOrganizationEnterpriseTrialStarted    Action = "organization:enterprise_trial_started"
	ActionOrganizationEnterpriseTrialEndChanged Action = "organization:enterprise_trial_end_changed"

	ActionOrganizationDisabled         Action = "organization:disabled"
	ActionOrganizationEnabled          Action = "organization:enabled"
	ActionOrganizationWhitelistUpdated Action = "organization:whitelist_updated"
	ActionOrganizationPaygActivated    Action = "organization:payg_activated"
	ActionOrganizationPaygDeactivated  Action = "organization:payg_deactivated"
)

type LogOrganizationSetupTaskUpdatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string
	TaskKey          string

	SetupTaskSnapshotBefore *OrganizationSetupTaskSnapshot
	SetupTaskSnapshotAfter  *OrganizationSetupTaskSnapshot
}

// OrganizationOnboardingPlaybookSnapshot is the playbook an organization walks.
type OrganizationOnboardingPlaybookSnapshot struct {
	PlaybookID string   `json:"playbook_id"`
	Name       string   `json:"name"`
	UseCase    string   `json:"use_case,omitempty"`
	StepSlugs  []string `json:"step_slugs"`
}

type LogOrganizationOnboardingPlaybookAssignedEvent struct {
	OrganizationID   string
	Actor            urn.Principal
	ActorDisplayName *string
	OrganizationName string
	OrganizationSlug string
	// PlaybookSnapshotBefore is nil when no playbook was assigned.
	PlaybookSnapshotBefore *OrganizationOnboardingPlaybookSnapshot
	// PlaybookSnapshotAfter is nil when the assignment was cleared.
	PlaybookSnapshotAfter *OrganizationOnboardingPlaybookSnapshot
}

func (l *Logger) LogOrganizationOnboardingPlaybookAssigned(ctx context.Context, dbtx repo.DBTX, event LogOrganizationOnboardingPlaybookAssignedEvent) error {
	before, err := marshalAuditPayload(event.PlaybookSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal onboarding playbook before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.PlaybookSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal onboarding playbook after snapshot: %w", err)
	}
	action := ActionOrganizationOnboardingPlaybookAssigned
	if event.PlaybookSnapshotAfter == nil {
		action = ActionOrganizationOnboardingPlaybookUnassigned
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(action), SubjectID: event.OrganizationID, SubjectType: "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName), SubjectSlug: conv.ToPGTextEmpty(event.OrganizationSlug),
		Metadata: nil, BeforeSnapshot: before, AfterSnapshot: after,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationOnboardingV1})
}

type OrganizationOnboardingStackVendorSnapshot struct {
	Vendor string  `json:"vendor"`
	Plan   *string `json:"plan,omitempty"`
}

type OrganizationOnboardingStackSnapshot struct {
	Vendors       []OrganizationOnboardingStackVendorSnapshot `json:"vendors"`
	MdmVendor     *string                                     `json:"mdm_vendor,omitempty"`
	MdmVendorName *string                                     `json:"mdm_vendor_name,omitempty"`
}

type LogOrganizationOnboardingStackUpdatedEvent struct {
	OrganizationID      string
	Actor               urn.Principal
	ActorDisplayName    *string
	OrganizationName    string
	OrganizationSlug    string
	StackSnapshotBefore *OrganizationOnboardingStackSnapshot
	StackSnapshotAfter  *OrganizationOnboardingStackSnapshot
}

func (l *Logger) LogOrganizationOnboardingStackUpdated(ctx context.Context, dbtx repo.DBTX, event LogOrganizationOnboardingStackUpdatedEvent) error {
	before, err := marshalAuditPayload(event.StackSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal onboarding stack before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.StackSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal onboarding stack after snapshot: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionOrganizationOnboardingStackUpdated), SubjectID: event.OrganizationID, SubjectType: "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName), SubjectSlug: conv.ToPGTextEmpty(event.OrganizationSlug),
		Metadata: nil, BeforeSnapshot: before, AfterSnapshot: after,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationOnboardingV1})
}

type OrganizationSetupTaskAssigneeSnapshot struct {
	UserID   *string `json:"user_id,omitempty"`
	Email    string  `json:"email"`
	Name     *string `json:"name,omitempty"`
	PhotoURL *string `json:"photo_url,omitempty"`
}

type OrganizationSetupTaskSnapshot struct {
	Key         string                                 `json:"key"`
	Title       string                                 `json:"title"`
	Description string                                 `json:"description"`
	Status      string                                 `json:"status"`
	Assignee    *OrganizationSetupTaskAssigneeSnapshot `json:"assignee,omitempty"`
	BlockedBy   []string                               `json:"blocked_by"`
	Hidden      bool                                   `json:"hidden"`
}

func (l *Logger) LogOrganizationSetupTaskUpdated(ctx context.Context, dbtx repo.DBTX, event LogOrganizationSetupTaskUpdatedEvent) error {
	metadata, err := marshalAuditPayload(map[string]string{"task_key": event.TaskKey})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", ActionOrganizationSetupTaskUpdated, err)
	}
	beforeSnapshot, err := marshalAuditPayload(event.SetupTaskSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionOrganizationSetupTaskUpdated, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.SetupTaskSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionOrganizationSetupTaskUpdated, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionOrganizationSetupTaskUpdated),

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationSetupTaskV1})
}

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

// hooksFailOpenFeatureName mirrors productfeatures.FeatureHooksFailOpen,
// which this package cannot import without a cycle.
const hooksFailOpenFeatureName = "hooks_fail_open"

// LogOrganizationProductFeatureToggledEvent records a productFeatures.set
// change. The toggled feature's name is carried in metadata under
// "feature_name", except for hooks_fail_open, which keeps its dedicated
// action (and no metadata) so security-posture changes stay distinguishable
// from ordinary feature toggles.
type LogOrganizationProductFeatureToggledEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	FeatureName    string
	FeatureEnabled bool
}

func (l *Logger) LogOrganizationProductFeatureToggled(ctx context.Context, dbtx repo.DBTX, event LogOrganizationProductFeatureToggledEvent) error {
	action := conv.Ternary(event.FeatureEnabled, ActionOrganizationProductFeatureEnabled, ActionOrganizationProductFeatureDisabled)
	outboxEvent := events.OrganizationProductFeatureV1
	var metadata []byte
	if event.FeatureName == hooksFailOpenFeatureName {
		action = conv.Ternary(event.FeatureEnabled, ActionOrganizationHooksFailOpenEnabled, ActionOrganizationHooksFailOpenDisabled)
		outboxEvent = events.OrganizationHooksFailOpenV1
	} else {
		var err error
		metadata, err = marshalAuditPayload(map[string]any{
			"feature_name": event.FeatureName,
		})
		if err != nil {
			return fmt.Errorf("marshal %s metadata: %w", action, err)
		}
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

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: outboxEvent})
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
type OrganizationEnterpriseTrialConversionOrganizationSnapshot struct {
	AccountType string `json:"account_type"`
	Whitelisted bool   `json:"whitelisted"`
	Disabled    bool   `json:"disabled"`
}

type OrganizationEnterpriseTrialConversionLifecycleSnapshot struct {
	Status      string     `json:"status"`
	Tier        string     `json:"tier"`
	EndsAt      *time.Time `json:"ends_at"`
	ConvertedAt *time.Time `json:"converted_at"`
	DemotedAt   *time.Time `json:"demoted_at"`
}

type OrganizationEnterpriseTrialConversionKeySnapshot struct {
	KeyType           string `json:"key_type"`
	StoredDisabled    bool   `json:"stored_disabled"`
	EffectiveDisabled bool   `json:"effective_disabled"`
	KeyAccessChanged  bool   `json:"key_access_changed"`
	MonthlyCredits    int64  `json:"monthly_credits"`
}

type OrganizationEnterpriseTrialConversionSnapshot struct {
	Organization OrganizationEnterpriseTrialConversionOrganizationSnapshot `json:"organization"`
	Trial        OrganizationEnterpriseTrialConversionLifecycleSnapshot    `json:"trial"`
	Keys         []OrganizationEnterpriseTrialConversionKeySnapshot        `json:"keys"`
}

type LogOrganizationEnterpriseTrialConvertedEvent struct {
	OrganizationID   string
	ConversionSource string
	KeyAccessChanged *bool
	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string
	Before           OrganizationEnterpriseTrialConversionSnapshot
	After            OrganizationEnterpriseTrialConversionSnapshot
}

func (l *Logger) LogOrganizationEnterpriseTrialConverted(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialConvertedEvent) error {
	action := ActionOrganizationEnterpriseTrialConverted
	metadataFields := map[string]any{"conversion_source": event.ConversionSource}
	if event.KeyAccessChanged != nil {
		metadataFields["key_access_changed"] = *event.KeyAccessChanged
	}
	metadata, err := marshalAuditPayload(metadataFields)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}
	beforeSnapshot, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.PtrToPGTextEmpty(event.ActorSlug),
		Action: string(action), SubjectID: event.OrganizationID, SubjectType: "organization",
		SubjectDisplayName: conv.ToPGText("Organization"), SubjectSlug: conv.ToPGTextEmpty(""),
		Metadata: metadata, BeforeSnapshot: beforeSnapshot, AfterSnapshot: afterSnapshot,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationEnterpriseTrialV1})
}

type LogOrganizationEnterpriseTrialRearmedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	AccountType       string
	TrialEndsAt       time.Time
	ArmAuditOperation string
	KeyAccessChanged  bool
}

func (l *Logger) LogOrganizationEnterpriseTrialRearmed(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialRearmedEvent) error {
	action := ActionOrganizationEnterpriseTrialRearmed

	metadata, err := marshalAuditPayload(map[string]any{
		"account_type":       event.AccountType,
		"trial_ends_at":      event.TrialEndsAt,
		"arm_operation_id":   event.ArmAuditOperation,
		"key_access_changed": event.KeyAccessChanged,
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

// LogOrganizationEnterpriseTrialStartedEvent records an operator granting a
// new enterprise trial, either to an organization that never trialled or to
// one whose previous trial expired without converting or being demoted.
type LogOrganizationEnterpriseTrialStartedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	AccountType string
	TrialEndsAt time.Time
}

func (l *Logger) LogOrganizationEnterpriseTrialStarted(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialStartedEvent) error {
	action := ActionOrganizationEnterpriseTrialStarted

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

// LogOrganizationEnterpriseTrialExtendedEvent records an operator moving a
// running trial's end date forward. Both dates are carried so the entry never
// depends on inverting the calendar-day arithmetic, which is exact only while
// every session runs in UTC.
type LogOrganizationEnterpriseTrialExtendedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	ExtendedByDays      int
	PreviousTrialEndsAt time.Time
	TrialEndsAt         time.Time
}

func (l *Logger) LogOrganizationEnterpriseTrialExtended(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialExtendedEvent) error {
	action := ActionOrganizationEnterpriseTrialExtended

	metadata, err := marshalAuditPayload(map[string]any{
		"extended_by_days":       event.ExtendedByDays,
		"previous_trial_ends_at": event.PreviousTrialEndsAt,
		"trial_ends_at":          event.TrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}
	beforeSnapshot, err := marshalAuditPayload(map[string]any{
		"trial_ends_at": event.PreviousTrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(map[string]any{
		"trial_ends_at": event.TrialEndsAt,
	})
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

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationEnterpriseTrialV1})
}

// LogOrganizationEnterpriseTrialEndChangedEvent records the previous and new end dates.
type LogOrganizationEnterpriseTrialEndChangedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OrganizationName string
	OrganizationSlug string

	PreviousTrialEndsAt time.Time
	TrialEndsAt         time.Time
}

func (l *Logger) LogOrganizationEnterpriseTrialEndChanged(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialEndChangedEvent) error {
	action := ActionOrganizationEnterpriseTrialEndChanged

	metadata, err := marshalAuditPayload(map[string]any{
		"previous_trial_ends_at": event.PreviousTrialEndsAt,
		"trial_ends_at":          event.TrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}
	beforeSnapshot, err := marshalAuditPayload(map[string]any{
		"trial_ends_at": event.PreviousTrialEndsAt,
	})
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(map[string]any{
		"trial_ends_at": event.TrialEndsAt,
	})
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

		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),

		Metadata:       metadata,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
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
	KeyAccessChanged    bool
}

func (l *Logger) LogOrganizationEnterpriseTrialDemoted(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEnterpriseTrialDemotedEvent) error {
	action := ActionOrganizationEnterpriseTrialDemoted

	metadata, err := marshalAuditPayload(map[string]any{
		"previous_account_type": event.PreviousAccountType,
		"trial_ends_at":         event.TrialEndsAt,
		"key_access_changed":    event.KeyAccessChanged,
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

// OrganizationWhitelistSnapshot records the dashboard demo-access gate.
type OrganizationWhitelistSnapshot struct {
	// Whitelisted bypasses the demo gate without changing account type or disabled state.
	Whitelisted bool `json:"whitelisted"`
}

type LogOrganizationWhitelistUpdatedEvent struct {
	// OrganizationID is the canonical target.
	OrganizationID string

	// Actor is the verified staff principal.
	Actor urn.Principal

	// ActorDisplayName is the staff display name.
	ActorDisplayName *string

	// OrganizationName identifies the subject.
	OrganizationName string

	// OrganizationSlug identifies the subject.
	OrganizationSlug string

	// OrganizationSnapshotBefore records the previous gate setting.
	OrganizationSnapshotBefore *OrganizationWhitelistSnapshot

	// OrganizationSnapshotAfter records the committed gate setting.
	OrganizationSnapshotAfter *OrganizationWhitelistSnapshot
}

func (l *Logger) LogOrganizationWhitelistUpdated(ctx context.Context, dbtx repo.DBTX, event LogOrganizationWhitelistUpdatedEvent) error {
	before, err := marshalAuditPayload(event.OrganizationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal whitelist before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.OrganizationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal whitelist after snapshot: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionOrganizationWhitelistUpdated), SubjectID: event.OrganizationID, SubjectType: "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName), SubjectSlug: conv.ToPGTextEmpty(event.OrganizationSlug),
		Metadata: nil, BeforeSnapshot: before, AfterSnapshot: after,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationAccessV1})
}

// OrganizationAccessSnapshot records whether the organization was disabled.
type OrganizationAccessSnapshot struct {
	DisabledAt *time.Time `json:"disabled_at"`
}

// LogOrganizationAccessEvent describes an organization access transition.
type LogOrganizationAccessEvent struct {
	OrganizationID             string
	Actor                      urn.Principal
	ActorDisplayName           *string
	ActorSlug                  *string
	OrganizationName           string
	OrganizationSlug           string
	OrganizationSnapshotBefore *OrganizationAccessSnapshot
	OrganizationSnapshotAfter  *OrganizationAccessSnapshot
}

func (l *Logger) LogOrganizationEnabled(ctx context.Context, dbtx repo.DBTX, event LogOrganizationAccessEvent) error {
	return l.logOrganizationAccess(ctx, dbtx, event, ActionOrganizationEnabled)
}

func (l *Logger) LogOrganizationDisabled(ctx context.Context, dbtx repo.DBTX, event LogOrganizationAccessEvent) error {
	return l.logOrganizationAccess(ctx, dbtx, event, ActionOrganizationDisabled)
}

func (l *Logger) logOrganizationAccess(ctx context.Context, dbtx repo.DBTX, event LogOrganizationAccessEvent, action Action) error {
	before, err := marshalAuditPayload(event.OrganizationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	after, err := marshalAuditPayload(event.OrganizationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID:     event.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:          conv.PtrToPGTextEmpty(event.ActorSlug),
		Action:             string(action),
		SubjectID:          event.OrganizationID,
		SubjectType:        "organization",
		SubjectDisplayName: conv.ToPGTextEmpty(event.OrganizationName),
		SubjectSlug:        conv.ToPGTextEmpty(event.OrganizationSlug),
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
		Metadata:           nil,
		ActingSurface:      conv.ToPGTextEmpty(""),
		ActingClientID:     conv.ToPGTextEmpty(""),
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationAccessV1})
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

// LogOrganizationAccountTypeChangedEvent describes a committed tier policy intent.
// Equal before/after tiers intentionally emit an event to repair existing keys.
type LogOrganizationAccountTypeChangedEvent struct {
	OrganizationID    string
	Actor             urn.Principal
	ActorDisplayName  *string
	BeforeAccountType string
	AccountType       string
}

func (l *Logger) LogOrganizationAccountTypeChanged(ctx context.Context, dbtx repo.DBTX, event LogOrganizationAccountTypeChangedEvent) error {
	action := ActionOrganizationAccountTypeChanged
	metadata, err := marshalAuditPayload(map[string]string{"operation": "account_type_change"})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}
	before, err := marshalAuditPayload(map[string]string{"account_type": event.BeforeAccountType})
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	after, err := marshalAuditPayload(map[string]string{"account_type": event.AccountType})
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ActorSlug: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), ActingSurface: conv.ToPGTextEmpty(""), ActingClientID: conv.ToPGTextEmpty(""),
		OrganizationID: event.OrganizationID,
		ActorID:        event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		Action: string(action), SubjectID: event.OrganizationID, SubjectType: "organization", SubjectDisplayName: conv.ToPGText("Organization"),
		Metadata: metadata, BeforeSnapshot: before, AfterSnapshot: after,
	}, OutboxEvent: events.OrganizationAccountTypeV1})
}

// LogOrganizationInferenceKeyRepairedEvent contains policy state only, never credentials.
type InferenceKeyPolicySnapshot struct {
	KeyType        string   `json:"key_type"`
	MonthlyCredits int64    `json:"monthly_credits"`
	Disabled       bool     `json:"disabled"`
	DisableCauses  []string `json:"disable_causes"`
}

type LogOrganizationInferenceKeyRepairedEvent struct {
	OrganizationID   string
	Actor            urn.Principal
	ActorDisplayName *string
	Reason           string
	RemoveCauses     []string
	Before           InferenceKeyPolicySnapshot
	After            InferenceKeyPolicySnapshot
}

func (l *Logger) LogOrganizationInferenceKeyRepaired(ctx context.Context, dbtx repo.DBTX, event LogOrganizationInferenceKeyRepairedEvent) error {
	metadata, err := marshalAuditPayload(map[string]any{"operation": "inference_key_repair", "reason": event.Reason, "remove_causes": event.RemoveCauses, "key_type": event.After.KeyType})
	if err != nil {
		return err
	}
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return err
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return err
	}
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ActorSlug: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), ActingSurface: conv.ToPGTextEmpty(""), ActingClientID: conv.ToPGTextEmpty(""), OrganizationID: event.OrganizationID, ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), Action: string(ActionOrganizationInferenceKeyRepaired), SubjectID: event.OrganizationID, SubjectType: "organization", SubjectDisplayName: conv.ToPGText("Organization"), Metadata: metadata, BeforeSnapshot: before, AfterSnapshot: after}, OutboxEvent: events.OrganizationAccountTypeV1})
}
