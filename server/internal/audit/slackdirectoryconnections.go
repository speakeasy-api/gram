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
	ActionSlackDirectoryConnectionSync       Action = "slack-directory-connection:sync"
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
	return l.logSlackDirectoryConnection(ctx, dbtx, ActionSlackDirectoryConnectionAuthorize, event, nil)
}
func (l *Logger) LogSlackDirectoryConnectionDisconnect(ctx context.Context, dbtx repo.DBTX, event LogSlackDirectoryConnectionEvent) error {
	return l.logSlackDirectoryConnection(ctx, dbtx, ActionSlackDirectoryConnectionDisconnect, event, nil)
}

type SlackDirectorySyncSummary struct {
	Observed         int `json:"observed"`
	ExcludedExternal int `json:"excluded_external"`
	Bots             int `json:"bots"`
}

func (l *Logger) LogSlackDirectoryConnectionSync(ctx context.Context, dbtx repo.DBTX, event LogSlackDirectoryConnectionEvent, summary SlackDirectorySyncSummary) error {
	return l.logSlackDirectoryConnection(ctx, dbtx, ActionSlackDirectoryConnectionSync, event, &summary)
}
func (l *Logger) logSlackDirectoryConnection(ctx context.Context, dbtx repo.DBTX, action Action, event LogSlackDirectoryConnectionEvent, summary *SlackDirectorySyncSummary) error {
	before, err := marshalAuditPayload(event.ConnectionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal Slack connection before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.ConnectionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal Slack connection after snapshot: %w", err)
	}
	metadata, err := marshalAuditPayload(summary)
	if err != nil {
		return fmt.Errorf("marshal Slack sync summary: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(action), SubjectID: event.ConnectionURN.ID.String(), SubjectType: string(subjectTypeSlackDirectoryConnection), SubjectDisplayName: conv.ToPGTextEmpty(event.ConnectionSnapshotAfter.WorkspaceName), SubjectSlug: conv.ToPGTextEmpty(""),
		BeforeSnapshot: before, AfterSnapshot: after, Metadata: metadata,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SlackDirectoryConnectionV1})
}
