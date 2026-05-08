package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionUserSessionIssuerCreate Action = "user-session-issuer:create"
	ActionUserSessionIssuerUpdate Action = "user-session-issuer:update"
	ActionUserSessionIssuerDelete Action = "user-session-issuer:delete"
)

type LogUserSessionIssuerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionIssuerURN urn.UserSessionIssuer
	Slug                 string
}

func LogUserSessionIssuerCreate(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerCreateEvent) error {
	action := ActionUserSessionIssuerCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogUserSessionIssuerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionIssuerURN            urn.UserSessionIssuer
	Slug                            string
	UserSessionIssuerSnapshotBefore *types.UserSessionIssuer
	UserSessionIssuerSnapshotAfter  *types.UserSessionIssuer
}

func LogUserSessionIssuerUpdate(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerUpdateEvent) error {
	action := ActionUserSessionIssuerUpdate

	beforeSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogUserSessionIssuerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionIssuerURN urn.UserSessionIssuer
	Slug                 string
}

func LogUserSessionIssuerDelete(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerDeleteEvent) error {
	action := ActionUserSessionIssuerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
