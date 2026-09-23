package audit

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionSlackDirectoryConnectionAuthorize  Action = "slack-directory-connection:authorize"
	ActionSlackDirectoryConnectionDisconnect Action = "slack-directory-connection:disconnect"
)

type LogSlackDirectoryConnectionEvent struct {
	OrganizationID           string
	Actor                    urn.Principal
	ActorDisplayName         *string
	ConnectionURN            urn.SlackDirectoryConnection
	ConnectionSnapshotBefore *gen.SlackDirectoryConnection
	ConnectionSnapshotAfter  *gen.SlackDirectoryConnection
}

func (l *Logger) LogSlackDirectoryConnectionAuthorize(ctx context.Context, dbtx repo.DBTX, event LogSlackDirectoryConnectionEvent) error {
	return l.logSlackDirectoryConnection(ctx, dbtx, ActionSlackDirectoryConnectionAuthorize, event)
}
func (l *Logger) LogSlackDirectoryConnectionDisconnect(ctx context.Context, dbtx repo.DBTX, event LogSlackDirectoryConnectionEvent) error {
	return l.logSlackDirectoryConnection(ctx, dbtx, ActionSlackDirectoryConnectionDisconnect, event)
}
func (l *Logger) logSlackDirectoryConnection(ctx context.Context, dbtx repo.DBTX, action Action, event LogSlackDirectoryConnectionEvent) error {
	before, err := marshalAuditPayload(event.ConnectionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal Slack connection before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.ConnectionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal Slack connection after snapshot: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(action), SubjectID: event.ConnectionURN.ID.String(), SubjectType: string(subjectTypeSlackDirectoryConnection), SubjectDisplayName: conv.ToPGTextEmpty(event.ConnectionSnapshotAfter.WorkspaceName), SubjectSlug: conv.ToPGTextEmpty(""),
		BeforeSnapshot: before, AfterSnapshot: after, Metadata: nil,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SlackDirectoryConnectionV1})
}
