package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionCustomDomainsCreate Action = "custom_domains:create"
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

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
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

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
