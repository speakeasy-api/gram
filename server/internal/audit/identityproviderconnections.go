package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionIdentityProviderConnectionCreated  Action = "identity_provider_connection:created"
	ActionIdentityProviderConnectionUpdated  Action = "identity_provider_connection:updated"
	ActionIdentityProviderConnectionVerified Action = "identity_provider_connection:verified"
	ActionIdentityProviderConnectionDeleted  Action = "identity_provider_connection:deleted"
)

type IdentityProviderConnectionSnapshot struct {
	Kind             string   `json:"kind"`
	TenantIdentifier string   `json:"tenant_identifier"`
	Status           string   `json:"status"`
	Outcome          string   `json:"outcome,omitempty"`
	Capabilities     []string `json:"capabilities"`
	GrantedScopes    []string `json:"granted_scopes"`
}

type LogIdentityProviderConnectionCreatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IdentityProviderConnectionURN urn.IdentityProviderConnectionID
	TenantIdentifier              string
}

type LogIdentityProviderConnectionUpdatedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IdentityProviderConnectionURN            urn.IdentityProviderConnectionID
	TenantIdentifier                         string
	IdentityProviderConnectionSnapshotBefore *IdentityProviderConnectionSnapshot
	IdentityProviderConnectionSnapshotAfter  *IdentityProviderConnectionSnapshot
}

type LogIdentityProviderConnectionVerifiedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IdentityProviderConnectionURN            urn.IdentityProviderConnectionID
	TenantIdentifier                         string
	IdentityProviderConnectionSnapshotBefore *IdentityProviderConnectionSnapshot
	IdentityProviderConnectionSnapshotAfter  *IdentityProviderConnectionSnapshot
}

type LogIdentityProviderConnectionDeletedEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IdentityProviderConnectionURN urn.IdentityProviderConnectionID
	TenantIdentifier              string
}

func (l *Logger) LogIdentityProviderConnectionCreated(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionCreatedEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, identityProviderConnectionAuditEvent{
		organizationID:   event.OrganizationID,
		actor:            event.Actor,
		actorDisplayName: event.ActorDisplayName,
		actorSlug:        event.ActorSlug,
		connectionURN:    event.IdentityProviderConnectionURN,
		tenantIdentifier: event.TenantIdentifier,
		action:           ActionIdentityProviderConnectionCreated,
		beforeSnapshot:   nil,
		afterSnapshot:    nil,
	})
}

func (l *Logger) LogIdentityProviderConnectionUpdated(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionUpdatedEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, identityProviderConnectionAuditEvent{
		organizationID:   event.OrganizationID,
		actor:            event.Actor,
		actorDisplayName: event.ActorDisplayName,
		actorSlug:        event.ActorSlug,
		connectionURN:    event.IdentityProviderConnectionURN,
		tenantIdentifier: event.TenantIdentifier,
		action:           ActionIdentityProviderConnectionUpdated,
		beforeSnapshot:   event.IdentityProviderConnectionSnapshotBefore,
		afterSnapshot:    event.IdentityProviderConnectionSnapshotAfter,
	})
}

func (l *Logger) LogIdentityProviderConnectionVerified(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionVerifiedEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, identityProviderConnectionAuditEvent{
		organizationID:   event.OrganizationID,
		actor:            event.Actor,
		actorDisplayName: event.ActorDisplayName,
		actorSlug:        event.ActorSlug,
		connectionURN:    event.IdentityProviderConnectionURN,
		tenantIdentifier: event.TenantIdentifier,
		action:           ActionIdentityProviderConnectionVerified,
		beforeSnapshot:   event.IdentityProviderConnectionSnapshotBefore,
		afterSnapshot:    event.IdentityProviderConnectionSnapshotAfter,
	})
}

func (l *Logger) LogIdentityProviderConnectionDeleted(ctx context.Context, dbtx repo.DBTX, event LogIdentityProviderConnectionDeletedEvent) error {
	return l.logIdentityProviderConnection(ctx, dbtx, identityProviderConnectionAuditEvent{
		organizationID:   event.OrganizationID,
		actor:            event.Actor,
		actorDisplayName: event.ActorDisplayName,
		actorSlug:        event.ActorSlug,
		connectionURN:    event.IdentityProviderConnectionURN,
		tenantIdentifier: event.TenantIdentifier,
		action:           ActionIdentityProviderConnectionDeleted,
		beforeSnapshot:   nil,
		afterSnapshot:    nil,
	})
}

type identityProviderConnectionAuditEvent struct {
	organizationID   string
	actor            urn.Principal
	actorDisplayName *string
	actorSlug        *string
	connectionURN    urn.IdentityProviderConnectionID
	tenantIdentifier string
	action           Action
	beforeSnapshot   *IdentityProviderConnectionSnapshot
	afterSnapshot    *IdentityProviderConnectionSnapshot
}

func (l *Logger) logIdentityProviderConnection(ctx context.Context, dbtx repo.DBTX, event identityProviderConnectionAuditEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.beforeSnapshot)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", event.action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.afterSnapshot)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", event.action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.actor.ID,
		ActorType:        string(event.actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.actorSlug),

		Action: string(event.action),

		SubjectID:          event.connectionURN.ID.String(),
		SubjectType:        string(subjectTypeIdentityProviderConnection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.tenantIdentifier),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.IdentityProviderConnectionV1})
}
