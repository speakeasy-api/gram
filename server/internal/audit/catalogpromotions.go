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
