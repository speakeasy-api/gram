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
	ActionKeyCreate Action = "api_key:create"
	ActionKeyRevoke Action = "api_key:revoke"
)

type LogKeyCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	Scopes []string

	// Set when the key is plugin-scoped (rfc-plugin-scoped-keys.md); zero
	// values for org-wide keys. Surfaces in audit metadata as plugin_id /
	// toolset_id so admins can answer "which plugin/server was this
	// credential tied to?" from the audit history alone.
	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	ToolsetURN urn.Toolset
}

func LogKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogKeyCreateEvent) error {
	action := ActionKeyCreate

	payload := map[string]any{
		"scopes": event.Scopes,
	}
	if event.PluginID != uuid.Nil {
		payload["plugin_id"] = event.PluginID.String()
	}
	if !event.ToolsetURN.IsZero() {
		payload["toolset_id"] = event.ToolsetURN.ID.String()
	}
	metadata, err := marshalAuditPayload(payload)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogKeyRevokeEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.APIKey
	KeyName string

	Scopes []string

	// Mirrors LogKeyCreateEvent (rfc-plugin-scoped-keys.md) so the audit
	// trail is symmetric across mint and revoke.
	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	ToolsetURN urn.Toolset
}

func LogKeyRevoke(ctx context.Context, dbtx repo.DBTX, event LogKeyRevokeEvent) error {
	action := ActionKeyRevoke

	payload := map[string]any{
		"scopes": event.Scopes,
	}
	if event.PluginID != uuid.Nil {
		payload["plugin_id"] = event.PluginID.String()
	}
	if !event.ToolsetURN.IsZero() {
		payload["toolset_id"] = event.ToolsetURN.ID.String()
	}
	metadata, err := marshalAuditPayload(payload)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
