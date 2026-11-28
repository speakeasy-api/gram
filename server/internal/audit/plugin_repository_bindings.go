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
	ActionPluginRepositoryBind        Action = "plugin-repository:bind"
	ActionPluginRepositoryReprovision Action = "plugin-repository:reprovision"

	subjectTypePluginRepository subjectType = "plugin_repository"
)

type LogPluginRepositoryBindingEvent struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	RepositoryID     string //nolint:glint // auditeventurnnaming: GitHub identifier kept as metadata, not an audit subject.
	InstallationID   string //nolint:glint // auditeventurnnaming: GitHub identifier kept as metadata, not an audit subject.
	RepositoryOwner  string
	RepositoryName   string
	Evidence         string
	Reprovisioned    bool
}

// LogPluginRepositoryBinding records the staff decision alongside the binding.
// Historical evidence and recovery reasons are internal and have no webhook.
func (l *Logger) LogPluginRepositoryBinding(ctx context.Context, dbtx repo.DBTX, event LogPluginRepositoryBindingEvent) error {
	action := ActionPluginRepositoryBind
	if event.Reprovisioned {
		action = ActionPluginRepositoryReprovision
	}
	metadata, err := marshalAuditPayload(struct {
		RepositoryID    string `json:"repository_id"`
		InstallationID  string `json:"installation_id"`
		RepositoryOwner string `json:"repo_owner"`
		RepositoryName  string `json:"repo_name"`
		Evidence        string `json:"evidence"`
	}{
		RepositoryID:    event.RepositoryID,
		InstallationID:  event.InstallationID,
		RepositoryOwner: event.RepositoryOwner,
		RepositoryName:  event.RepositoryName,
		Evidence:        event.Evidence,
	})
	if err != nil {
		return fmt.Errorf("marshal plugin repository binding audit: %w", err)
	}
	absent := conv.ToPGTextEmpty("")
	return l.log(ctx, dbtx, auditEntry{Params: repo.InsertAuditLogParams{
		IsInternal:         true,
		OrganizationID:     event.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: event.ProjectID, Valid: true},
		ActorID:            event.Actor.ID,
		ActorType:          string(event.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(event.ActorDisplayName),
		Action:             string(action),
		SubjectID:          event.ProjectID.String(),
		SubjectType:        string(subjectTypePluginRepository),
		Metadata:           metadata,
		ActorSlug:          absent,
		SubjectDisplayName: absent,
		SubjectSlug:        absent,
		BeforeSnapshot:     nil,
		AfterSnapshot:      nil,
		ActingSurface:      absent,
		ActingClientID:     absent,
	}, OutboxEvent: nil})
}
