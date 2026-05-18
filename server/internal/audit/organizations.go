package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	organizationsgen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionOrganizationInviteCreate     Action = "organization_invitation:create"
	ActionOrganizationInviteRoleUpdate Action = "organization_invitation:update_role"
	ActionOrganizationWebhooksEnabled  Action = "organization:webhooks_enabled"
	ActionOrganizationWebhooksDisabled Action = "organization:webhooks_disabled"
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

	return l.log(ctx, dbtx, entry)
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

	return l.log(ctx, dbtx, entry)
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

	return l.log(ctx, dbtx, entry)
}
