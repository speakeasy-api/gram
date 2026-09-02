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

type NetworkIngressEventBase struct {
	OrganizationID    string
	Actor             urn.Principal
	ActorDisplayName  *string
	NetworkIngressURN urn.NetworkIngress
	Hostname          string
}

type LogNetworkIngressCreateEvent struct {
	NetworkIngressEventBase
	Snapshot *gen.NetworkIngress
}

type LogNetworkIngressUpdateEvent struct {
	NetworkIngressEventBase
	Before *gen.NetworkIngress
	After  *gen.NetworkIngress
}

type LogNetworkIngressRotateCredentialsEvent struct {
	NetworkIngressEventBase
	Provider string
}

type LogNetworkIngressDeleteEvent struct{ NetworkIngressEventBase }

func networkIngressEntry(base NetworkIngressEventBase, action Action, metadata, before, after []byte) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID:     base.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID:            base.Actor.ID,
		ActorType:          string(base.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(base.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(action),
		SubjectID:          base.NetworkIngressURN.ID.String(),
		SubjectType:        string(subjectTypeNetworkIngress),
		SubjectDisplayName: conv.ToPGTextEmpty(base.Hostname),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		Metadata:           metadata,
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
	}
}

func (l *Logger) LogNetworkIngressCreate(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressCreateEvent) error {
	after, err := marshalAuditPayload(event.Snapshot)
	if err != nil {
		return fmt.Errorf("marshal network ingress create snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: networkIngressEntry(event.NetworkIngressEventBase, ActionNetworkIngressCreate, nil, nil, after), OutboxEvent: events.NetworkIngressV1})
}

func (l *Logger) LogNetworkIngressUpdate(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressUpdateEvent) error {
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal network ingress update before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal network ingress update after snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: networkIngressEntry(event.NetworkIngressEventBase, ActionNetworkIngressUpdate, nil, before, after), OutboxEvent: events.NetworkIngressV1})
}

func (l *Logger) LogNetworkIngressRotateCredentials(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressRotateCredentialsEvent) error {
	metadata, err := marshalAuditPayload(map[string]any{"provider": event.Provider, "rotated": true})
	if err != nil {
		return fmt.Errorf("marshal network ingress rotation metadata: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: networkIngressEntry(event.NetworkIngressEventBase, ActionNetworkIngressRotateCredentials, metadata, nil, nil), OutboxEvent: events.NetworkIngressV1})
}

func (l *Logger) LogNetworkIngressDelete(ctx context.Context, dbtx repo.DBTX, event LogNetworkIngressDeleteEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: networkIngressEntry(event.NetworkIngressEventBase, ActionNetworkIngressDelete, nil, nil, nil), OutboxEvent: events.NetworkIngressV1})
}
