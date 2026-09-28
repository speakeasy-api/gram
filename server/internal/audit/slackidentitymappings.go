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
	ActionSlackIdentityMappingConfirm   Action = "slack-identity-mapping:confirm"
	ActionSlackIdentityMappingReassign  Action = "slack-identity-mapping:reassign"
	ActionSlackIdentityMappingReconfirm Action = "slack-identity-mapping:reconfirm"
	ActionSlackIdentityMappingUnmap     Action = "slack-identity-mapping:unmap"
)

type LogSlackIdentityMappingEvent struct {
	OrganizationID           string
	Actor                    urn.Principal
	ActorDisplayName         *string
	MembershipURN            urn.SlackDirectoryMembership
	MembershipSnapshotBefore *gen.SlackDirectoryMember
	MembershipSnapshotAfter  *gen.SlackDirectoryMember
}

func (l *Logger) LogSlackIdentityMapping(ctx context.Context, dbtx repo.DBTX, action Action, event LogSlackIdentityMappingEvent) error {
	before, err := marshalAuditPayload(event.MembershipSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal Slack mapping before: %w", err)
	}
	after, err := marshalAuditPayload(event.MembershipSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal Slack mapping after: %w", err)
	}
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ActorID: event.Actor.ID, ActorType: string(event.Actor.Type), ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName), ActorSlug: conv.ToPGTextEmpty(""),
		Action: string(action), SubjectID: event.MembershipURN.ID.String(), SubjectType: string(subjectTypeSlackDirectoryMembership), SubjectDisplayName: conv.PtrToPGTextEmpty(event.MembershipSnapshotAfter.DisplayName), SubjectSlug: conv.ToPGTextEmpty(""),
		BeforeSnapshot: before, AfterSnapshot: after, Metadata: nil,
	}
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.SlackIdentityMappingV1})
}
