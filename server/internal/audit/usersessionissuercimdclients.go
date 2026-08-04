package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Adding or removing a CIMD document URL changes which OAuth clients an
// issuer will admit, so both are audited. The URL entry is the subject in
// its own right, matching how other issuer-owned child resources are
// recorded; the parent issuer travels in the metadata so a reader can
// attribute an entry without a second lookup.
const (
	ActionUserSessionIssuerCimdClientAdd    Action = "user-session-issuer-cimd-client:add"
	ActionUserSessionIssuerCimdClientRemove Action = "user-session-issuer-cimd-client:remove"
)

// userSessionIssuerCimdClientMetadata carries the parent issuer on every
// event so a reader can attribute a URL to its issuer without a second
// lookup.
func userSessionIssuerCimdClientMetadata(issuerURN urn.UserSessionIssuer, issuerSlug string) ([]byte, error) {
	metadata, err := marshalAuditPayload(map[string]any{
		"user_session_issuer":      issuerURN.String(),
		"user_session_issuer_slug": issuerSlug,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal user session issuer cimd client metadata: %w", err)
	}

	return metadata, nil
}

type LogUserSessionIssuerCimdClientAddEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CimdClientURN       urn.UserSessionIssuerCimdClient
	ClientIDMetadataURI string
	CimdClientSnapshot  *types.UserSessionIssuerCimdClient

	UserSessionIssuerURN  urn.UserSessionIssuer
	UserSessionIssuerSlug string
}

func (l *Logger) LogUserSessionIssuerCimdClientAdd(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerCimdClientAddEvent) error {
	action := ActionUserSessionIssuerCimdClientAdd

	metadata, err := userSessionIssuerCimdClientMetadata(event.UserSessionIssuerURN, event.UserSessionIssuerSlug)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.CimdClientSnapshot)
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

		SubjectID:   event.CimdClientURN.ID.String(),
		SubjectType: string(subjectTypeUserSessionIssuerCimdClient),
		// The URL is the entry's whole identity; there is no name or slug.
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientIDMetadataURI),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionIssuerCimdClientV1})
}

type LogUserSessionIssuerCimdClientRemoveEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CimdClientURN       urn.UserSessionIssuerCimdClient
	ClientIDMetadataURI string
	CimdClientSnapshot  *types.UserSessionIssuerCimdClient

	UserSessionIssuerURN  urn.UserSessionIssuer
	UserSessionIssuerSlug string
}

func (l *Logger) LogUserSessionIssuerCimdClientRemove(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerCimdClientRemoveEvent) error {
	action := ActionUserSessionIssuerCimdClientRemove

	metadata, err := userSessionIssuerCimdClientMetadata(event.UserSessionIssuerURN, event.UserSessionIssuerSlug)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	beforeSnapshot, err := marshalAuditPayload(event.CimdClientSnapshot)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.CimdClientURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuerCimdClient),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ClientIDMetadataURI),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionIssuerCimdClientV1})
}
