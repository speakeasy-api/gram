package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionPlatformMcpRegistrationCreate        Action = "platform-mcp-registration:create"
	ActionPlatformMcpRegistrationHandoffIssue  Action = "platform-mcp-registration:handoff_issue"
	ActionPlatformMcpRegistrationHandoffRedeem Action = "platform-mcp-registration:handoff_redeem"
)

// LogPlatformMcpRegistrationCreateEvent records private component convergence
// for a reviewed catalog registration. It intentionally excludes remote URLs,
// headers, credentials, tokens, and provider response bodies.
type LogPlatformMcpRegistrationCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor urn.Principal

	PlatformMcpRegistrationURN urn.PlatformMcpRegistration
	CatalogProvider            string
	CatalogReference           string
	RemoteMcpServerID          uuid.UUID
	UserSessionIssuerID        uuid.UUID
	McpServerID                uuid.UUID
	McpEndpointID              uuid.UUID
}

func (l *Logger) LogPlatformMcpRegistrationCreate(ctx context.Context, dbtx repo.DBTX, event LogPlatformMcpRegistrationCreateEvent) error {
	action := ActionPlatformMcpRegistrationCreate
	metadata, err := marshalAuditPayload(map[string]string{
		"catalog_provider":       event.CatalogProvider,
		"catalog_reference":      event.CatalogReference,
		"remote_mcp_server_id":   event.RemoteMcpServerID.String(),
		"user_session_issuer_id": event.UserSessionIssuerID.String(),
		"mcp_server_id":          event.McpServerID.String(),
		"mcp_endpoint_id":        event.McpEndpointID.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(nil),
		ActorSlug:        conv.PtrToPGTextEmpty(nil),

		Action: string(action),

		SubjectID:          event.PlatformMcpRegistrationURN.ID.String(),
		SubjectType:        string(subjectTypePlatformMcpRegistration),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CatalogProvider),
		SubjectSlug:        conv.ToPGTextEmpty(event.CatalogReference),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.PlatformMcpRegistrationV1})
}

type LogPlatformMcpRegistrationHandoffEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor urn.Principal

	PlatformMcpRegistrationURN urn.PlatformMcpRegistration
	CatalogProvider            string
	CatalogReference           string
	HandoffID                  uuid.UUID
	Intent                     string
}

func (l *Logger) LogPlatformMcpRegistrationHandoffIssue(ctx context.Context, dbtx repo.DBTX, event LogPlatformMcpRegistrationHandoffEvent) error {
	return l.logPlatformMcpRegistrationHandoff(ctx, dbtx, ActionPlatformMcpRegistrationHandoffIssue, event)
}

func (l *Logger) LogPlatformMcpRegistrationHandoffRedeem(ctx context.Context, dbtx repo.DBTX, event LogPlatformMcpRegistrationHandoffEvent) error {
	return l.logPlatformMcpRegistrationHandoff(ctx, dbtx, ActionPlatformMcpRegistrationHandoffRedeem, event)
}

func (l *Logger) logPlatformMcpRegistrationHandoff(ctx context.Context, dbtx repo.DBTX, action Action, event LogPlatformMcpRegistrationHandoffEvent) error {
	metadata, err := marshalAuditPayload(map[string]string{
		"catalog_provider":  event.CatalogProvider,
		"catalog_reference": event.CatalogReference,
		"handoff_id":        event.HandoffID.String(),
		"intent":            event.Intent,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(nil),
		ActorSlug:        conv.PtrToPGTextEmpty(nil),

		Action: string(action),

		SubjectID:          event.PlatformMcpRegistrationURN.ID.String(),
		SubjectType:        string(subjectTypePlatformMcpRegistration),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CatalogProvider),
		SubjectSlug:        conv.ToPGTextEmpty(event.CatalogReference),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.PlatformMcpRegistrationV1})
}
