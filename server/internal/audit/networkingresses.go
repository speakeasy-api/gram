package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionNetworkIngressCreate            Action = "network_ingress:create"
	ActionNetworkIngressUpdate            Action = "network_ingress:update"
	ActionNetworkIngressRotateCredentials Action = "network_ingress:rotate_credentials"
	ActionNetworkIngressDelete            Action = "network_ingress:delete"
)

type LogNetworkIngressCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	NetworkIngressURN urn.NetworkIngress
	Hostname          string
}

func (l *Logger) LogNetworkIngressCreate(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressCreateEvent) error {
	action := ActionNetworkIngressCreate

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.NetworkIngressURN.ID.String(),
		SubjectType:        string(subjectTypeNetworkIngress),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Hostname),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.NetworkIngressV1})
}

type LogNetworkIngressUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	NetworkIngressURN            urn.NetworkIngress
	Hostname                     string
	NetworkIngressSnapshotBefore *gen.NetworkIngress
	NetworkIngressSnapshotAfter  *gen.NetworkIngress
}

func (l *Logger) LogNetworkIngressUpdate(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressUpdateEvent) error {
	action := ActionNetworkIngressUpdate
	beforeSnapshot, err := marshalAuditPayload(event.NetworkIngressSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.NetworkIngressSnapshotAfter)
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

		SubjectID:          event.NetworkIngressURN.ID.String(),
		SubjectType:        string(subjectTypeNetworkIngress),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Hostname),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.NetworkIngressV1})
}

type LogNetworkIngressRotateCredentialsEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	NetworkIngressURN urn.NetworkIngress
	Hostname          string
	CredentialKind    string
}

func (l *Logger) LogNetworkIngressRotateCredentials(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressRotateCredentialsEvent) error {
	action := ActionNetworkIngressRotateCredentials

	metadata, err := marshalAuditPayload(map[string]string{"credential_kind": event.CredentialKind})
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

		SubjectID:          event.NetworkIngressURN.ID.String(),
		SubjectType:        string(subjectTypeNetworkIngress),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Hostname),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.NetworkIngressV1})
}

type LogNetworkIngressDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	NetworkIngressURN urn.NetworkIngress
	Hostname          string
}

func (l *Logger) LogNetworkIngressDelete(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressDeleteEvent) error {
	action := ActionNetworkIngressDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.NetworkIngressURN.ID.String(),
		SubjectType:        string(subjectTypeNetworkIngress),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Hostname),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.NetworkIngressV1})
}
