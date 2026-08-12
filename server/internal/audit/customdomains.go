package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionCustomDomainsCreate Action = "custom_domains:create"
	ActionCustomDomainsUpdate Action = "custom_domains:update"
	ActionCustomDomainsDelete Action = "custom_domains:delete"
)

type LogCustomDomainCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CustomDomainURN urn.CustomDomain
	DomainName      string
}

type LogCustomDomainUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CustomDomainURN            urn.CustomDomain
	DomainName                 string
	CustomDomainSnapshotBefore *domains.CustomDomain
	CustomDomainSnapshotAfter  *domains.CustomDomain
}

func (l *Logger) LogCustomDomainUpdate(ctx context.Context, dbtx repo.DBTX, event LogCustomDomainUpdateEvent) error {
	action := ActionCustomDomainsUpdate
	beforeSnapshot, err := marshalAuditPayload(event.CustomDomainSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.CustomDomainSnapshotAfter)
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

		SubjectID:          event.CustomDomainURN.ID.String(),
		SubjectType:        string(subjectTypeCustomDomain),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DomainName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.CustomDomainV1})
}

func (l *Logger) LogCustomDomainCreate(ctx context.Context, dbtx repo.DBTX, event LogCustomDomainCreateEvent) error {
	action := ActionCustomDomainsCreate

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CustomDomainURN.ID.String(),
		SubjectType:        string(subjectTypeCustomDomain),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DomainName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.CustomDomainV1})
}

type LogCustomDomainDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CustomDomainURN urn.CustomDomain
	DomainName      string
}

func (l *Logger) LogCustomDomainDelete(ctx context.Context, dbtx repo.DBTX, event LogCustomDomainDeleteEvent) error {
	action := ActionCustomDomainsDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CustomDomainURN.ID.String(),
		SubjectType:        string(subjectTypeCustomDomain),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DomainName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.CustomDomainV1})
}
