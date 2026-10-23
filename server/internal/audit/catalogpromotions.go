package audit

import (
	"context"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ActionCatalogPromotionRetryDocs records an explicit staff documentation retry.
const ActionCatalogPromotionRetryDocs Action = "catalog-promotion:retry-docs"

// LogCatalogPromotionRetryDocsEvent scopes a retry without source connection data.
type LogCatalogPromotionRetryDocsEvent struct {
	// OrganizationID identifies the original source tenant.
	OrganizationID string
	// ProjectID is absent when the source project was deleted.
	ProjectID uuid.NullUUID
	// Actor is the verified staff principal.
	Actor urn.Principal
	// CatalogPromotionURN identifies the durable promotion receipt.
	CatalogPromotionURN urn.CatalogPromotion
}

// LogCatalogPromotionRetryDocs writes an audit entry in the retry transaction.
func (l *Logger) LogCatalogPromotionRetryDocs(ctx context.Context, dbtx repo.DBTX, event LogCatalogPromotionRetryDocsEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: event.ProjectID,
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.ToPGTextEmpty(""), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionCatalogPromotionRetryDocs), SubjectID: event.CatalogPromotionURN.ID.String(), SubjectType: string(subjectTypeCatalogPromotion),
		SubjectDisplayName: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), BeforeSnapshot: nil, AfterSnapshot: nil, Metadata: nil,
	}, OutboxEvent: events.CatalogPromotionV1})
}

// ActionCatalogPromotionReject records an explicit staff candidate rejection.
const ActionCatalogPromotionReject Action = "catalog-promotion:reject"

// LogCatalogPromotionRejectEvent scopes rejection without source connection data.
type LogCatalogPromotionRejectEvent struct {
	// OrganizationID identifies the original source tenant.
	OrganizationID string
	// ProjectID is absent when the source project was deleted.
	ProjectID uuid.NullUUID
	// Actor is the verified staff principal.
	Actor urn.Principal
	// CatalogPromotionURN identifies the durable promotion receipt.
	CatalogPromotionURN urn.CatalogPromotion
}

// LogCatalogPromotionReject writes an audit entry in the curation transaction.
func (l *Logger) LogCatalogPromotionReject(ctx context.Context, dbtx repo.DBTX, event LogCatalogPromotionRejectEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: event.ProjectID,
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.ToPGTextEmpty(""), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionCatalogPromotionReject), SubjectID: event.CatalogPromotionURN.ID.String(), SubjectType: string(subjectTypeCatalogPromotion),
		SubjectDisplayName: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), BeforeSnapshot: nil, AfterSnapshot: nil, Metadata: nil,
	}, OutboxEvent: events.CatalogPromotionV1})
}

// ActionCatalogPromotionArchive records an explicit staff candidate archive.
const ActionCatalogPromotionArchive Action = "catalog-promotion:archive"

// LogCatalogPromotionArchiveEvent scopes archive without source connection data.
type LogCatalogPromotionArchiveEvent struct {
	// OrganizationID identifies the original source tenant.
	OrganizationID string
	// ProjectID is absent when the source project was deleted.
	ProjectID uuid.NullUUID
	// Actor is the verified staff principal.
	Actor urn.Principal
	// CatalogPromotionURN identifies the durable promotion receipt.
	CatalogPromotionURN urn.CatalogPromotion
}

// LogCatalogPromotionArchive writes an audit entry in the curation transaction.
func (l *Logger) LogCatalogPromotionArchive(ctx context.Context, dbtx repo.DBTX, event LogCatalogPromotionArchiveEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: event.ProjectID,
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.ToPGTextEmpty(""), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionCatalogPromotionArchive), SubjectID: event.CatalogPromotionURN.ID.String(), SubjectType: string(subjectTypeCatalogPromotion),
		SubjectDisplayName: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), BeforeSnapshot: nil, AfterSnapshot: nil, Metadata: nil,
	}, OutboxEvent: events.CatalogPromotionV1})
}

// ActionCatalogPromotionRestore records an explicit staff candidate restoration.
const ActionCatalogPromotionRestore Action = "catalog-promotion:restore"

// LogCatalogPromotionRestoreEvent scopes restoration without source connection data.
type LogCatalogPromotionRestoreEvent struct {
	// OrganizationID identifies the original source tenant.
	OrganizationID string
	// ProjectID is absent when the source project was deleted.
	ProjectID uuid.NullUUID
	// Actor is the verified staff principal.
	Actor urn.Principal
	// CatalogPromotionURN identifies the durable promotion receipt.
	CatalogPromotionURN urn.CatalogPromotion
}

// LogCatalogPromotionRestore writes an audit entry in the curation transaction.
func (l *Logger) LogCatalogPromotionRestore(ctx context.Context, dbtx repo.DBTX, event LogCatalogPromotionRestoreEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: event.ProjectID,
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.ToPGTextEmpty(""), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(ActionCatalogPromotionRestore), SubjectID: event.CatalogPromotionURN.ID.String(), SubjectType: string(subjectTypeCatalogPromotion),
		SubjectDisplayName: conv.ToPGTextEmpty(""), SubjectSlug: conv.ToPGTextEmpty(""), BeforeSnapshot: nil, AfterSnapshot: nil, Metadata: nil,
	}, OutboxEvent: events.CatalogPromotionV1})
}
